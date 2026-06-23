package usage

import "testing"

func TestScanner_StreamNormal(t *testing.T) {
	var got TokenUsage
	var called bool
	s := NewScanner(true, func(u TokenUsage) { got = u; called = true })
	s.Feed([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":12500,\"cache_creation_input_tokens\":800,\"cache_read_input_tokens\":0,\"output_tokens\":0}}}\n\n"))
	s.Feed([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{},\"usage\":{\"output_tokens\":3200}}\n\n"))
	s.Close()
	if !called {
		t.Fatal("expected onDone called for normal stream")
	}
	if got.Input != 12500 || got.Output != 3200 || got.CacheCreation != 800 || got.CacheRead != 0 {
		t.Fatalf("unexpected usage: %+v", got)
	}
}

func TestScanner_StreamAcrossChunkBoundary(t *testing.T) {
	var got TokenUsage
	var called bool
	s := NewScanner(true, func(u TokenUsage) { got = u; called = true })
	start := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":500,\"output_tokens\":0}}}\n\n"
	mid := len(start) / 2
	s.Feed([]byte(start[:mid])) // 拆成两半喂入
	s.Feed([]byte(start[mid:]))
	s.Feed([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":100}}\n\n"))
	s.Close()
	if !called {
		t.Fatal("expected onDone despite chunk boundary")
	}
	if got.Input != 500 || got.Output != 100 {
		t.Fatalf("unexpected usage across chunk: %+v", got)
	}
}

func TestScanner_StreamMissingCacheFields(t *testing.T) {
	var got TokenUsage
	s := NewScanner(true, func(u TokenUsage) { got = u })
	// Anthropic 不总是返回 cache 字段
	s.Feed([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":0}}}\n\n"))
	s.Feed([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":50}}\n\n"))
	s.Close()
	if got.Input != 100 || got.Output != 50 || got.CacheCreation != 0 || got.CacheRead != 0 {
		t.Fatalf("unexpected usage with missing cache fields: %+v", got)
	}
}

func TestScanner_StreamNoUsage_NoCallback(t *testing.T) {
	called := false
	s := NewScanner(true, func(u TokenUsage) { called = true })
	s.Feed([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"...\"}}\n\n"))
	s.Close()
	if called {
		t.Fatal("must not call onDone when no message_start seen")
	}
}

func TestScanner_NonStream_WithUsage(t *testing.T) {
	var got TokenUsage
	var called bool
	s := NewScanner(false, func(u TokenUsage) { got = u; called = true })
	body := `{"id":"msg_1","type":"message","usage":{"input_tokens":700,"output_tokens":120,"cache_creation_input_tokens":30,"cache_read_input_tokens":10}}`
	mid := len(body) / 2
	s.Feed([]byte(body[:mid]))
	s.Feed([]byte(body[mid:]))
	s.Close()
	if !called {
		t.Fatal("expected onDone for non-stream with usage")
	}
	if got.Input != 700 || got.Output != 120 || got.CacheCreation != 30 || got.CacheRead != 10 {
		t.Fatalf("unexpected non-stream usage: %+v", got)
	}
}

func TestScanner_NonStream_NoUsage_NoCallback(t *testing.T) {
	called := false
	s := NewScanner(false, func(u TokenUsage) { called = true })
	s.Feed([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`))
	s.Close()
	if called {
		t.Fatal("must not call onDone for non-stream response without usage")
	}
}

func TestScanner_MidStreamEOF_PartialCommit(t *testing.T) {
	var got TokenUsage
	var called bool
	s := NewScanner(true, func(u TokenUsage) { got = u; called = true })
	s.Feed([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":999,\"output_tokens\":0}}}\n\n"))
	s.Close() // 模拟中途 EOF：未见 message_delta
	if !called {
		t.Fatal("expected partial commit on mid-stream EOF")
	}
	if got.Input != 999 || got.Output != 0 {
		t.Fatalf("unexpected partial usage: %+v", got)
	}
}

func TestScanner_CloseIdempotent(t *testing.T) {
	count := 0
	s := NewScanner(true, func(u TokenUsage) { count++ })
	s.Feed([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
	s.Close()
	s.Close() // 二次 Close 不应重复触发
	if count != 1 {
		t.Fatalf("expected onDone once, got %d", count)
	}
}

func TestScanner_BadJSON_FailSoft(t *testing.T) {
	var got TokenUsage
	var called bool
	s := NewScanner(true, func(u TokenUsage) { got = u; called = true })
	s.Feed([]byte("data: {bad json\n")) // 坏 JSON 行应被丢弃
	s.Feed([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n"))
	s.Feed([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":8}}\n\n"))
	s.Close()
	if !called {
		t.Fatal("expected onDone despite earlier bad JSON line")
	}
	if got.Input != 5 || got.Output != 8 {
		t.Fatalf("unexpected usage after bad JSON: %+v", got)
	}
}
