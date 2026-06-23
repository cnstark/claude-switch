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
		return fmt.Errorf("上游连接失败: %w", err)
	}
	defer resp.Body.Close()

	// 透传响应头
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}

	// 透传状态码
	w.WriteHeader(resp.StatusCode)

	// 逐 chunk 流式转发
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("向客户端写入失败: %w", writeErr)
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("读取上游响应失败: %w", readErr)
		}
	}
	return nil
}
