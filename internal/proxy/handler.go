package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cnstark/claude-switch/internal/config"
	"github.com/cnstark/claude-switch/internal/logging"
	"github.com/cnstark/claude-switch/internal/usage"
	"io"
	"net/http"
	"strings"
)

// AuthStore 鉴权接口
type AuthStore interface {
	Authenticate(apiKey string) (projectName string, ok bool)
}

// ModelResolver 模型路由接口
type ModelResolver interface {
	Resolve(projectName, requestModel string) ([]string, bool)
}

// ConfigLookup 配置查询接口
type ConfigLookup interface {
	Upstream(name string) (config.Upstream, bool)
}

// Forwarder 上游转发接口
type Forwarder interface {
	Forward(cfg config.Upstream, body []byte, headers http.Header, w http.ResponseWriter, c *usage.Collector, log *logging.Logger) error
}

// Handler 代理 HTTP handler
type Handler struct {
	auth             AuthStore
	resolver         ModelResolver
	lookup           ConfigLookup
	forwarder        Forwarder
	log              *logging.Logger
	tracker          usage.Recorder // nil = usage 关闭
	usageEnabled     bool           // 来自 per-request snapshot.Server.UsageStats
	projectLogLevels map[string]config.LogLevel
}

// NewHandler 创建代理 handler
func NewHandler(auth AuthStore, resolver ModelResolver, lookup ConfigLookup, forwarder Forwarder, log *logging.Logger) *Handler {
	return &Handler{
		auth:      auth,
		resolver:  resolver,
		lookup:    lookup,
		forwarder: forwarder,
		log:       log,
	}
}

// ServeHTTP 处理请求：鉴权 → 解析模型 → 路由 → 转发
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// recover panic
	defer func() {
		if rec := recover(); rec != nil {
			h.log.Info("handler panic recovered", map[string]any{"panic": fmt.Sprintf("%v", rec)})
			writeError(w, http.StatusInternalServerError, "internal_error", "内部服务错误")
		}
	}()

	// 1. 鉴权：优先 x-api-key，其次 Authorization: Bearer <key>
	apiKey := r.Header.Get("x-api-key")
	if apiKey == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			apiKey = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	projectName, ok := h.auth.Authenticate(apiKey)
	if !ok {
		h.log.Info("auth failed", map[string]any{"key_prefix": maskKeyLog(apiKey)})
		writeError(w, http.StatusUnauthorized, "authentication_error", "无效的 API key")
		return
	}

	// 根据请求所属 project 设置日志级别（每个 project 可独立配置 log_level）
	h.log.SetLevel(logLevelForProject(projectName, h.projectLogLevels))

	// 2. 记录请求头（debug 级别，辅助排查上游兼容性问题）
	h.log.Debug("request headers", map[string]any{
		"content_type":   r.Header.Get("content-type"),
		"accept":         r.Header.Get("accept"),
		"user_agent":     r.Header.Get("user-agent"),
		"anthropic_ver":  r.Header.Get("anthropic-version"),
		"content_length": r.Header.Get("content-length"),
	})

	// 3. 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "无法读取请求体")
		return
	}
	h.log.Debug("request body received", map[string]any{
		"body_len":      len(body),
		"content_type":  r.Header.Get("content-type"),
		"raw_body_head": truncStr(string(body), 512),
	})

	// 4. 解析 model
	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		h.log.Debug("request body json parse failed", map[string]any{
			"error":     err.Error(),
			"body_head": truncStr(string(body), 512),
			"body_len":  len(body),
			"body_tail": truncTail(string(body), 128),
		})
		writeError(w, http.StatusBadRequest, "invalid_request_error", "请求体 JSON 解析失败")
		return
	}
	requestModel, ok := reqBody["model"].(string)
	if !ok || requestModel == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "请求体缺少 model 字段")
		return
	}

	// 5. 路由查表
	cfgNames, ok := h.resolver.Resolve(projectName, requestModel)
	if !ok {
		h.log.Info("model not found", map[string]any{
			"project": projectName,
			"model":   requestModel,
		})
		writeError(w, http.StatusNotFound, "not_found_error",
			fmt.Sprintf("项目 %q 未配置模型 %q 的映射", projectName, requestModel))
		return
	}

	// usage 收集器：仅当开关开启且 tracker 存在时创建（per-request）。
	// 关闭时传 nil，Forward 走零开销直传路径。故障转移复用同一 collector：
	// 连接失败的 cfg 不会 Attach（不计数），成功的 cfg 流结束时 Close 触发一次 Record。
	var collector *usage.Collector
	if h.usageEnabled && h.tracker != nil {
		collector = usage.NewCollector(h.tracker, projectName, requestModel)
	}

	// 6. 依次尝试转发
	for _, cfgName := range cfgNames {
		cfg, ok := h.lookup.Upstream(cfgName)
		if !ok {
			continue
		}
		// 故障转移时更新 usage collector 的 model 为上游真实模型名
		if collector != nil {
			collector.SetModel(cfg.Model)
		}
		rewrittenBody, err := rewriteRequestBody(body, cfg.Model)
		if err != nil {
			h.log.Info("rewrite failed", map[string]any{"error": err.Error()})
			writeError(w, http.StatusInternalServerError, "internal_error", "请求重写失败")
			return
		}
		h.log.Debug("forwarding to upstream", map[string]any{
			"upstream":            cfgName,
			"upstream_url":        cfg.URL,
			"rewritten_body_len":  len(rewrittenBody),
			"rewritten_body_head": truncStr(string(rewrittenBody), 512),
			"rewritten_body_tail": truncTail(string(rewrittenBody), 128),
		})
		reqHeaders := r.Header.Clone()
		rewriteHeaders(reqHeaders, cfg.APIKey)

		fwdErr := h.forwarder.Forward(cfg, rewrittenBody, reqHeaders, w, collector, h.log)
		if fwdErr == nil {
			h.log.Info("request forwarded", map[string]any{
				"project":  projectName,
				"model":    requestModel,
				"upstream": cfgName,
			})
			return
		}
		// 不变量：响应已开始后（已 WriteHeader / 写了首字节）不得转移到下一个上游，
		// 否则两段响应拼接会让客户端收到截断/混乱的 JSON（unexpected end of JSON input）。
		// 此时也无法再向客户端写错误响应，只能记日志后终止。
		var startedErr *ResponseStartedError
		if errors.As(fwdErr, &startedErr) {
			h.log.Info("upstream failed after response started, aborting failover", map[string]any{
				"upstream": cfgName,
				"error":    startedErr.Err.Error(),
			})
			return
		}
		h.log.Info("upstream failed, trying next", map[string]any{
			"upstream": cfgName,
			"error":    fwdErr.Error(),
		})
	}

	// 全部失败
	h.log.Info("all upstreams failed", map[string]any{
		"project": projectName,
		"model":   requestModel,
	})
	writeError(w, http.StatusBadGateway, "upstream_error", "所有上游均不可用")
}

// logLevelForProject 根据 project 名查找对应的日志级别，未找到时默认 Meta。
func logLevelForProject(projectName string, levels map[string]config.LogLevel) logging.Level {
	if levels == nil {
		return logging.Meta
	}
	ll, ok := levels[projectName]
	if !ok {
		return logging.Meta
	}
	switch ll {
	case config.LogDebug:
		return logging.Debug
	case config.LogMeta:
		return logging.Meta
	case config.LogOff:
		return logging.Off
	default:
		return logging.Meta
	}
}

// rewriteRequestBody 替换 JSON 请求体中的 model 字段。
// 使用 json.Encoder + SetEscapeHTML(false)，避免把请求体里的 <、>、& 等
// HTML 特殊字符转义成 < 等——这些字符在 Claude Code 的 system-reminder
// 和工具描述里很常见，转义虽 JSON 语义等价，但会改变字节内容并可能触发某些
// 上游解析器的边界问题，原样保留更安全。
func rewriteRequestBody(body []byte, targetModel string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("rewrite: json 解析失败: %w", err)
	}
	m["model"] = targetModel
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, fmt.Errorf("rewrite: json 序列化失败: %w", err)
	}
	// json.Encoder.Encode 会追加一个换行符，去掉以保持与原 body 一致
	out := buf.Bytes()
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}

// rewriteHeaders 删除原认证头，写入上游 key
func rewriteHeaders(h http.Header, upstreamKey string) {
	h.Del("x-api-key")
	h.Del("Authorization")
	h.Set("x-api-key", upstreamKey)
}

func writeError(w http.ResponseWriter, statusCode int, errType, message string) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errType,
			"message": message,
		},
	})
}

func maskKeyLog(key string) string {
	if len(key) <= 12 {
		if len(key) > 4 {
			return key[:4] + "..."
		}
		return "..."
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// truncStr 截取字符串前 n 个字符用于日志，避免落盘过大
func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// truncTail 截取字符串末尾 n 个字符，便于观察 JSON 是否被截断
func truncTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "...(truncated)..." + s[len(s)-n:]
}
