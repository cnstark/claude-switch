package proxy

import (
	"github.com/cnstark/claude-switch/internal/config"
	"github.com/cnstark/claude-switch/internal/logging"
	"encoding/json"
	"fmt"
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
	Forward(cfg config.Upstream, body []byte, headers http.Header, w http.ResponseWriter) error
}

// Handler 代理 HTTP handler
type Handler struct {
	auth      AuthStore
	resolver  ModelResolver
	lookup    ConfigLookup
	forwarder Forwarder
	log       *logging.Logger
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

	// 2. 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "无法读取请求体")
		return
	}

	// 3. 解析 model
	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "请求体 JSON 解析失败")
		return
	}
	requestModel, ok := reqBody["model"].(string)
	if !ok || requestModel == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "请求体缺少 model 字段")
		return
	}

	// 4. 路由查表
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

	// 5. 依次尝试转发
	for _, cfgName := range cfgNames {
		cfg, ok := h.lookup.Upstream(cfgName)
		if !ok {
			continue
		}
		rewrittenBody, err := rewriteRequestBody(body, cfg.Model)
		if err != nil {
			h.log.Info("rewrite failed", map[string]any{"error": err.Error()})
			writeError(w, http.StatusInternalServerError, "internal_error", "请求重写失败")
			return
		}
		reqHeaders := r.Header.Clone()
		rewriteHeaders(reqHeaders, cfg.APIKey)

		fwdErr := h.forwarder.Forward(cfg, rewrittenBody, reqHeaders, w)
		if fwdErr == nil {
			h.log.Info("request forwarded", map[string]any{
				"project":  projectName,
				"model":    requestModel,
				"upstream": cfgName,
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

// rewriteRequestBody 替换 JSON 请求体中的 model 字段
func rewriteRequestBody(body []byte, targetModel string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("rewrite: json 解析失败: %w", err)
	}
	m["model"] = targetModel
	newBody, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("rewrite: json 序列化失败: %w", err)
	}
	return newBody, nil
}

// rewriteHeaders 删除原 x-api-key，写入上游 key
func rewriteHeaders(h http.Header, upstreamKey string) {
	h.Del("x-api-key")
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
