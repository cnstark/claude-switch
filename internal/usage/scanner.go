// Package usage 提供 token 用量统计：旁路扫描 SSE 抽取 usage、内存累加与持久化。
package usage

import (
	"bytes"
	"encoding/json"
)

// TokenUsage 单个请求桶的四类 token 计数
type TokenUsage struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	CacheCreation int64 `json:"cache_creation"`
	CacheRead     int64 `json:"cache_read"`
}

// Scanner 旁路扫描上游响应流，抽取 Anthropic usage 事件。
// 不修改流、不阻塞流、解析失败 fail-soft。
// streaming=true 走 SSE 逐行解析；false 走非流式整 body 缓冲，Close 时取顶层 usage。
type Scanner struct {
	streaming bool
	leftover  []byte // 流式：跨 chunk 的不完整行尾
	buf       []byte // 非流式：累积完整 body
	usage     TokenUsage
	sawStart  bool // 流式：见过 message_start；非流式：顶层 usage 存在
	done      bool // onDone 是否已触发（幂等）
	onDone    func(TokenUsage)
}

// NewScanner 创建扫描器。streaming 决定解析模式；onDone 在 Close 时若见过 usage 则回调一次。
func NewScanner(streaming bool, onDone func(TokenUsage)) *Scanner {
	return &Scanner{streaming: streaming, onDone: onDone}
}

// Feed 喂入一个 chunk。流式模式按行解析，非流式模式追加到缓冲。
func (s *Scanner) Feed(chunk []byte) {
	if s.streaming {
		s.feedStream(chunk)
	} else {
		s.buf = append(s.buf, chunk...)
	}
}

// Close 流结束。若见过 usage 则触发一次 onDone。幂等。
func (s *Scanner) Close() {
	if s.done {
		return
	}
	if !s.streaming {
		s.parseNonStream()
	}
	if s.sawStart {
		s.done = true
		if s.onDone != nil {
			s.onDone(s.usage)
		}
	}
}

func (s *Scanner) feedStream(chunk []byte) {
	data := append(s.leftover, chunk...)
	s.leftover = nil
	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			s.leftover = data // 剩余不完整行，留待下次
			return
		}
		line := data[:idx]
		data = data[idx+1:]
		s.processLine(line)
	}
}

func (s *Scanner) processLine(line []byte) {
	trimmed := bytes.TrimRight(line, "\r")
	if !bytes.HasPrefix(trimmed, []byte("data:")) {
		return
	}
	payload := bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
	if len(payload) == 0 {
		return
	}
	// 轻量过滤：只含 usage 的 data 行才解析
	if !bytes.Contains(payload, []byte("usage")) {
		return
	}
	var ev sseEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return // fail-soft
	}
	switch ev.Type {
	case "message_start":
		s.sawStart = true
		s.usage.Input += ev.Message.Usage.InputTokens
		s.usage.CacheCreation += ev.Message.Usage.CacheCreationInputTokens
		s.usage.CacheRead += ev.Message.Usage.CacheReadInputTokens
		// message_start 的 output_tokens 为 0，不计
	case "message_delta":
		// message_delta.usage.output_tokens 是累计最终值，取最后值
		if ev.Usage.OutputTokens > 0 {
			s.usage.Output = ev.Usage.OutputTokens
		}
	}
}

// parseNonStream 非流式：解析整个 body 的顶层 usage
func (s *Scanner) parseNonStream() {
	if len(s.buf) == 0 {
		return
	}
	var resp struct {
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(s.buf, &resp); err != nil {
		return // fail-soft
	}
	if resp.Usage == nil {
		return // 无顶层 usage，不计数
	}
	s.sawStart = true
	s.usage = TokenUsage{
		Input:         resp.Usage.InputTokens,
		Output:        resp.Usage.OutputTokens,
		CacheCreation: resp.Usage.CacheCreationInputTokens,
		CacheRead:     resp.Usage.CacheReadInputTokens,
	}
}

// sseEvent 仅抽取 SSE 事件中与 usage 相关的字段
type sseEvent struct {
	Type    string `json:"type"`
	Message struct {
		Usage struct {
			InputTokens              int64 `json:"input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Usage struct {
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}
