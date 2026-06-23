package proxy

import (
	"bytes"
	"github.com/cnstark/claude-switch/internal/config"
	"fmt"
	"io"
	"net/http"
)

// StreamingForwarder 流式转发器 — 逐 chunk flush SSE 响应
type StreamingForwarder struct{}

// NewStreamingForwarder 创建默认转发器
func NewStreamingForwarder() *StreamingForwarder {
	return &StreamingForwarder{}
}

// ResponseStartedError 表示失败发生在响应已开始向客户端输出之后
// （即已经 WriteHeader 或写了首字节）。此时不可再转移到下一个上游，
// 否则会把两段响应拼接，导致客户端收到截断/混乱的 JSON。
// handler 通过 errors.As 检测此类型以遵守「流式已开始后不转移」的不变量。
type ResponseStartedError struct {
	Err error
}

func (e *ResponseStartedError) Error() string {
	return fmt.Sprintf("响应已开始后的失败，不可转移: %v", e.Err)
}

func (e *ResponseStartedError) Unwrap() error { return e.Err }

// Forward 发起上游请求并流式透传响应
func (f *StreamingForwarder) Forward(cfg config.Upstream, body []byte, headers http.Header, w http.ResponseWriter) error {
	req, err := http.NewRequest("POST", cfg.URL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建上游请求失败: %w", err)
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("x-api-key", cfg.APIKey)

	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		// 连接阶段失败：响应尚未开始，可安全转移到下一个上游
		return fmt.Errorf("上游连接失败: %w", err)
	}
	defer resp.Body.Close()

	// 透传响应头
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}

	// 透传状态码 —— 此刻起响应已开始，后续任何失败都不可转移到下一个上游
	w.WriteHeader(resp.StatusCode)

	// 逐 chunk 流式转发
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				// 响应已开始，不可转移
				return &ResponseStartedError{Err: fmt.Errorf("向客户端写入失败: %w", writeErr)}
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			// 响应已开始（如读 body 阶段 context deadline exceeded），
			// 不可转移 —— 包装为 ResponseStartedError 通知 handler 停止重试
			return &ResponseStartedError{Err: fmt.Errorf("读取上游响应失败: %w", readErr)}
		}
	}
	return nil
}
