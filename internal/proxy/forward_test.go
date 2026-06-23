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
