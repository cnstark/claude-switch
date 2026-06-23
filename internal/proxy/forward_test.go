package proxy

import (
	"github.com/cnstark/claude-switch/internal/auth"
	"github.com/cnstark/claude-switch/internal/config"
	"github.com/cnstark/claude-switch/internal/logging"
	"github.com/cnstark/claude-switch/internal/project"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRewriteRequest_ModelReplaced(t *testing.T) {
	body := []byte(`{"model":"aliasModel","max_tokens":100}`)
	newBody, err := rewriteRequestBody(body, "real-model-name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed map[string]any
	json.Unmarshal(newBody, &parsed)
	if parsed["model"] != "real-model-name" {
		t.Fatalf("expected model 'real-model-name', got %v", parsed["model"])
	}
	if parsed["max_tokens"] != float64(100) {
		t.Fatal("other fields should be preserved")
	}
}

// TestRewriteRequest_NoHTMLEscape 验证 rewriteRequestBody 不应把请求体里的
// HTML 特殊字符（<、>、&）转义成 < 等。这些字符在 Claude Code 的
// system-reminder / 工具描述里很常见，转义虽 JSON 语义等价，但会改变字节
// 内容并可能触发某些上游解析器的边界问题。原样保留更安全。
func TestRewriteRequest_NoHTMLEscape(t *testing.T) {
	body := []byte(`{"model":"alias","messages":[{"content":"<system-reminder>x<y&z</system-reminder>"}]}`)
	newBody, err := rewriteRequestBody(body, "real-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := string(newBody)
	// 不应出现 HTML 转义形式 < / > / &
	for _, esc := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(got, esc) {
			t.Fatalf("rewrite must not HTML-escape: found %s in output:\n%s", esc, got)
		}
	}
	// 原始的 <、>、& 应原样保留
	if !strings.Contains(got, "<system-reminder>") {
		t.Fatalf("expected raw <system-reminder> preserved, got:\n%s", got)
	}
	if !strings.Contains(got, "x<y&z") {
		t.Fatalf("expected raw x<y&z preserved, got:\n%s", got)
	}
}

func TestRewriteHeaders_KeyReplaced(t *testing.T) {
	h := http.Header{
		"X-Api-Key":         []string{"old-key"},
		"Content-Type":      []string{"application/json"},
		"Anthropic-Version": []string{"2023-06-01"},
	}
	rewriteHeaders(h, "new-upstream-key")
	if h.Get("X-Api-Key") != "new-upstream-key" {
		t.Fatalf("expected X-Api-Key to be replaced, got %s", h.Get("X-Api-Key"))
	}
	if h.Get("Content-Type") != "application/json" {
		t.Fatal("Content-Type should be preserved")
	}
}

func TestForward_BasicPassthrough(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.Header().Set("x-custom", "echo")
		w.WriteHeader(200)
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(body, &m)
		m["upstream_received"] = true
		json.NewEncoder(w).Encode(m)
	}))
	defer ts.Close()

	cfg := config.Upstream{
		Name: "cfg1", URL: ts.URL, APIKey: "sk-upstream",
		Model: "real-model", Timeout: 5 * time.Second,
	}

	authStore := auth.NewStore(map[string]string{"sk-cs-key1": "p1"})
	resolver := project.NewResolver(map[string]map[string][]string{"p1": {"aliasModel": {"cfg1"}}})
	lookup := &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg}}
	fwd := NewStreamingForwarder()
	log := logging.New(logging.Off, io.Discard)

	h := NewHandler(authStore, resolver, lookup, fwd, log)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"aliasModel","max_tokens":100}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("x-custom") != "echo" {
		t.Fatal("expected custom header to be passed through")
	}
}

func TestForward_StreamingSSE(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		for i := 0; i < 3; i++ {
			w.Write([]byte("data: chunk" + string(rune('0'+i)) + "\n\n"))
			flusher.Flush()
		}
		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer ts.Close()

	cfg := config.Upstream{Name: "cfg1", URL: ts.URL, APIKey: "sk-upstream", Model: "real-model", Timeout: 5 * time.Second}
	authStore := auth.NewStore(map[string]string{"sk-cs-key1": "p1"})
	resolver := project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1"}}})
	lookup := &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg}}
	fwd := NewStreamingForwarder()
	log := logging.New(logging.Off, io.Discard)
	h := NewHandler(authStore, resolver, lookup, fwd, log)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m","stream":true}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data: chunk0") {
		t.Fatal("expected streaming chunks in response")
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatal("expected [DONE] marker")
	}
}

// TestFailover_ResponseBodyStarted_NoFailover 验证故障转移不变量：
// 第一个上游在 WriteHeader 之后、读 body 阶段失败（模拟响应已开始后超时/读错），
// proxy 绝不能转移到第二个上游——否则会把两段响应拼给客户端，导致
// 客户端解析到截断的 JSON（unexpected end of JSON input）。
func TestFailover_ResponseBodyStarted_NoFailover(t *testing.T) {
	// 第二个上游：仅当被错误地调用时才会收到请求
	secondCalled := false
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalled = true
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"second":"should-not-be-merged"}`))
	}))
	defer ts2.Close()

	// 第一个上游：先 WriteHeader(200) 并写一段响应体，然后模拟读 body 阶段失败
	// 用一个会在写首字节后 hang 住直到客户端超时的 server
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		// 已提交响应头 + 写了首字节 → 响应已开始
		w.Write([]byte("data: partial-from-first\n\n"))
		flusher.Flush()
		// 然后挂起，让 client 的 Timeout 在读 body 阶段触发
		<-r.Context().Done()
	}))
	defer ts1.Close()

	cfg1 := config.Upstream{Name: "cfg1", URL: ts1.URL, APIKey: "k1", Model: "m1", Timeout: 200 * time.Millisecond}
	cfg2 := config.Upstream{Name: "cfg2", URL: ts2.URL, APIKey: "k2", Model: "m2", Timeout: 5 * time.Second}

	authStore := auth.NewStore(map[string]string{"sk-cs-key1": "p1"})
	resolver := project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1", "cfg2"}}})
	lookup := &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg1, "cfg2": cfg2}}
	fwd := NewStreamingForwarder()
	log := logging.New(logging.Off, io.Discard)
	h := NewHandler(authStore, resolver, lookup, fwd, log)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	// 不变量：响应已开始后不得转移到第二个上游
	if secondCalled {
		t.Fatal("FAIL: proxy transferred to second upstream AFTER response body had started — " +
			"this merges two responses and corrupts the client stream (root cause of 'unexpected end of JSON input')")
	}
	// 客户端应只看到第一个上游的部分响应，不被第二段污染
	if strings.Contains(body, "should-not-be-merged") {
		t.Fatalf("FAIL: client received merged response from two upstreams:\n%s", body)
	}
}

func TestFailover_FirstFails_FallbackSucceeds(t *testing.T) {
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok from backup"}`))
	}))
	defer ts2.Close()

	cfg1 := config.Upstream{Name: "cfg1", URL: "http://127.0.0.1:19999", APIKey: "k1", Model: "m1", Timeout: 100 * time.Millisecond}
	cfg2 := config.Upstream{Name: "cfg2", URL: ts2.URL, APIKey: "k2", Model: "m2", Timeout: 5 * time.Second}

	authStore := auth.NewStore(map[string]string{"sk-cs-key1": "p1"})
	resolver := project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1", "cfg2"}}})
	lookup := &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg1, "cfg2": cfg2}}
	fwd := NewStreamingForwarder()
	log := logging.New(logging.Off, io.Discard)
	h := NewHandler(authStore, resolver, lookup, fwd, log)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected fallback to cfg2 succeed with 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ok from backup") {
		t.Fatal("expected response from backup upstream")
	}
}

func TestFailover_AllFail_502(t *testing.T) {
	cfg1 := config.Upstream{Name: "cfg1", URL: "http://127.0.0.1:19998", APIKey: "k1", Model: "m1", Timeout: 50 * time.Millisecond}
	cfg2 := config.Upstream{Name: "cfg2", URL: "http://127.0.0.1:19999", APIKey: "k2", Model: "m2", Timeout: 50 * time.Millisecond}

	authStore := auth.NewStore(map[string]string{"sk-cs-key1": "p1"})
	resolver := project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1", "cfg2"}}})
	lookup := &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg1, "cfg2": cfg2}}
	fwd := NewStreamingForwarder()
	log := logging.New(logging.Off, io.Discard)
	h := NewHandler(authStore, resolver, lookup, fwd, log)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 502 {
		t.Fatalf("expected 502 when all upstreams fail, got %d", rec.Code)
	}
	var errResp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp["type"] != "error" {
		t.Fatal("expected Anthropic error format for 502")
	}
}
