package proxy

import (
	"encoding/json"
	"github.com/cnstark/claude-switch/internal/auth"
	"github.com/cnstark/claude-switch/internal/config"
	"github.com/cnstark/claude-switch/internal/logging"
	"github.com/cnstark/claude-switch/internal/project"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIntegration_FullFlow(t *testing.T) {
	// Fake upstream: verifies rewritten model and correct API key
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-real-upstream" {
			w.WriteHeader(401)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(body, &m)
		if m["model"] != "claude-opus-4-8" {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"unexpected model"}`))
			return
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "msg_123",
			"model": "claude-opus-4-8",
			"content": []map[string]any{
				{"type": "text", "text": "Hello from upstream"},
			},
		})
	}))
	defer ts1.Close()

	cfgUpstreams := map[string]config.Upstream{
		"cfg1": {
			Name: "cfg1", URL: ts1.URL, APIKey: "sk-real-upstream",
			Model: "claude-opus-4-8", Timeout: 5 * time.Second,
		},
	}
	keys := map[string]string{"sk-cs-myproject": "myproject"}
	projMap := map[string]map[string][]string{
		"myproject": {"my-model-alias": {"cfg1"}},
	}

	authStore := auth.NewStore(keys)
	resolver := project.NewResolver(projMap)
	lookup := &configLookup{upstreams: cfgUpstreams}
	fwd := NewStreamingForwarder()
	log := logging.NewNopLogger()

	handler := NewHandler(authStore, resolver, lookup, fwd, log)

	reqBody := `{"model":"my-model-alias","max_tokens":100,"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("x-api-key", "sk-cs-myproject")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["id"] != "msg_123" {
		t.Fatalf("expected response from upstream, got %v", resp)
	}
	if rec.Header().Get("content-type") != "application/json" {
		t.Fatalf("expected content-type application/json, got %s", rec.Header().Get("content-type"))
	}
}

func TestIntegration_StreamingFullFlow(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		for _, chunk := range []string{"Hello", " ", "World"} {
			w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
		}
		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer ts.Close()

	cfgUpstreams := map[string]config.Upstream{
		"cfg1": {Name: "cfg1", URL: ts.URL, APIKey: "k1", Model: "real-m", Timeout: 5 * time.Second},
	}
	keys := map[string]string{"sk-cs-p1": "p1"}
	projMap := map[string]map[string][]string{"p1": {"m": {"cfg1"}}}

	handler := NewHandler(
		auth.NewStore(keys),
		project.NewResolver(projMap),
		&configLookup{upstreams: cfgUpstreams},
		NewStreamingForwarder(),
		logging.NewNopLogger(),
	)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m","stream":true}`))
	req.Header.Set("x-api-key", "sk-cs-p1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Hello") || !strings.Contains(body, "[DONE]") {
		t.Fatalf("expected streaming content, got: %s", body)
	}
}
