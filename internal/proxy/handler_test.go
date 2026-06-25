package proxy

import (
	"encoding/json"
	"fmt"
	"github.com/cnstark/claude-switch/internal/auth"
	"github.com/cnstark/claude-switch/internal/circuitbreaker"
	"github.com/cnstark/claude-switch/internal/config"
	"github.com/cnstark/claude-switch/internal/logging"
	"github.com/cnstark/claude-switch/internal/project"
	"github.com/cnstark/claude-switch/internal/upstream"
	"github.com/cnstark/claude-switch/internal/usage"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// configLookup implements ConfigLookup interface for tests
type configLookup struct {
	upstreams map[string]config.Upstream
}

func (c *configLookup) Upstream(name string) (config.Upstream, bool) {
	u, ok := c.upstreams[name]
	return u, ok
}

func setupTestHandler(keys map[string]string, projMap map[string]map[string][]string, upstreams map[string]config.Upstream) *Handler {
	authStore := auth.NewStore(keys)
	resolver := project.NewResolver(projMap)
	lookup := &configLookup{upstreams: upstreams}
	fwd := upstream.NewClient()
	log := logging.New(logging.Off, io.Discard)
	return NewHandler(authStore, resolver, lookup, fwd, log)
}

func TestHandler_AuthFailure_401(t *testing.T) {
	h := setupTestHandler(
		map[string]string{"sk-cs-key1": "p1"},
		nil,
		nil,
	)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "bad-key")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	var errResp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp["type"] != "error" {
		t.Fatal("expected Anthropic error format")
	}
}

func TestHandler_UnknownModel_404(t *testing.T) {
	h := setupTestHandler(
		map[string]string{"sk-cs-key1": "p1"},
		map[string]map[string][]string{
			"p1": {"knownModel": {"cfg1"}},
		},
		map[string]config.Upstream{
			"cfg1": {Name: "cfg1", URL: "http://example.com", APIKey: "k", Model: "real-m", Timeout: 0},
		},
	)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"unknownModel"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_MissingAPIKey_401(t *testing.T) {
	h := setupTestHandler(
		map[string]string{"sk-cs-key1": "p1"},
		nil,
		nil,
	)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("expected 401 for missing api key, got %d", rec.Code)
	}
}

func TestHandler_BearerTokenAuth_Success(t *testing.T) {
	h := setupTestHandler(
		map[string]string{"sk-cs-key1": "p1"},
		map[string]map[string][]string{
			"p1": {"m": {"cfg1"}},
		},
		map[string]config.Upstream{
			"cfg1": {Name: "cfg1", URL: "http://127.0.0.1:1", APIKey: "k", Model: "real-m", Timeout: 0},
		},
	)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Authorization", "Bearer sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// 不再是 401 即表示 Bearer token 鉴权通过
	if rec.Code == 401 {
		t.Fatal("expected Bearer token auth to succeed, got 401")
	}
}

func TestHandler_BearerTokenAuth_Failure(t *testing.T) {
	h := setupTestHandler(
		map[string]string{"sk-cs-key1": "p1"},
		nil,
		nil,
	)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Authorization", "Bearer bad-key")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Fatalf("expected 401 for bad Bearer token, got %d", rec.Code)
	}
}

func TestHandler_XAPIKeyTakesPrecedence(t *testing.T) {
	h := setupTestHandler(
		map[string]string{"sk-cs-key1": "p1"},
		map[string]map[string][]string{
			"p1": {"m": {"cfg1"}},
		},
		map[string]config.Upstream{
			"cfg1": {Name: "cfg1", URL: "http://127.0.0.1:1", APIKey: "k", Model: "real-m", Timeout: 0},
		},
	)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("Authorization", "Bearer bad-key") // x-api-key 应优先
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// x-api-key 优先，正确的 key 应该通过鉴权
	if rec.Code == 401 {
		t.Fatal("expected x-api-key to take precedence over Authorization header, got 401")
	}
}

func TestHandler_Failover_CountsOnce(t *testing.T) {
	// cfg1 连接失败 → cfg2 成功带 usage → 只计一次
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":42,\"output_tokens\":0}}}\n\n"))
		flusher.Flush()
		w.Write([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n"))
		flusher.Flush()
	}))
	defer ts2.Close()

	cfg1 := config.Upstream{Name: "cfg1", URL: "http://127.0.0.1:19996", APIKey: "k1", Model: "m1", Timeout: 50 * time.Millisecond}
	cfg2 := config.Upstream{Name: "cfg2", URL: ts2.URL, APIKey: "k2", Model: "m2", Timeout: 5 * time.Second}

	rec := &usageFakeRecorder{}
	h := &Handler{
		auth:         auth.NewStore(map[string]string{"sk-cs-key1": "p1"}),
		resolver:     project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1", "cfg2"}}}),
		lookup:       &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg1, "cfg2": cfg2}},
		forwarder:    NewStreamingForwarder(),
		log:          logging.New(logging.Off, io.Discard),
		tracker:      rec,
		usageEnabled: true,
	}

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200 via cfg2, got %d", w.Code)
	}
	if rec.calls != 1 {
		t.Fatalf("expected exactly 1 usage commit, got %d", rec.calls)
	}
	if rec.u.Input != 42 || rec.u.Output != 7 {
		t.Fatalf("unexpected usage: %+v", rec.u)
	}
	// 故障转移后 model 应为上游真实模型名（cfg2.Model="m2"），而非 model_map 的 key（"m"）
	if rec.model != "m2" {
		t.Fatalf("expected model recorded as upstream real model 'm2', got %q", rec.model)
	}
}

func TestHandler_UsageDisabled_NoCollector(t *testing.T) {
	// usage_stats 关闭 → 不注入 collector，零计数
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":42,\"output_tokens\":0}}}\n\n"))
		flusher.Flush()
		w.Write([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n"))
		flusher.Flush()
	}))
	defer ts.Close()

	rec := &usageFakeRecorder{}
	h := &Handler{
		auth:         auth.NewStore(map[string]string{"sk-cs-key1": "p1"}),
		resolver:     project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1"}}}),
		lookup:       &configLookup{upstreams: map[string]config.Upstream{"cfg1": {Name: "cfg1", URL: ts.URL, APIKey: "k", Model: "m", Timeout: 5 * time.Second}}},
		forwarder:    NewStreamingForwarder(),
		log:          logging.New(logging.Off, io.Discard),
		tracker:      rec,
		usageEnabled: false,
	}
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if rec.calls != 0 {
		t.Fatalf("expected no usage when disabled, got %d", rec.calls)
	}
}

func TestHandler_ErrorResponsePassthrough_NoCount(t *testing.T) {
	// 上游返回 400 错误（不可重试）→ 透传，不计数
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`))
	}))
	defer ts.Close()

	rec := &usageFakeRecorder{}
	h := &Handler{
		auth:         auth.NewStore(map[string]string{"sk-cs-key1": "p1"}),
		resolver:     project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1"}}}),
		lookup:       &configLookup{upstreams: map[string]config.Upstream{"cfg1": {Name: "cfg1", URL: ts.URL, APIKey: "k", Model: "m", Timeout: 5 * time.Second}}},
		forwarder:    NewStreamingForwarder(),
		log:          logging.New(logging.Off, io.Discard),
		tracker:      rec,
		usageEnabled: true,
	}
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 passthrough, got %d", w.Code)
	}
	if rec.calls != 0 {
		t.Fatalf("expected no usage for error response, got %d", rec.calls)
	}
}

func TestIntegration_UsagePersisted_ReadBackByStats(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	usagePath := filepath.Join(dir, "usage.json")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":125,\"output_tokens\":0}}}\n\n"))
		flusher.Flush()
		w.Write([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":25}}\n\n"))
		flusher.Flush()
	}))
	defer ts.Close()

	cfgYAML := fmt.Sprintf(`
server:
  listen: 127.0.0.1:8787
  usage_stats: true
  private_keys:
    sk-cs-key1: p1
upstreams:
  - name: cfg1
    url: %s
    apikey: k
    model: real
    timeout: 5s
projects:
  - name: p1
    log_level: off
    model_map:
      m: [cfg1]
`, ts.URL)
	os.WriteFile(configPath, []byte(cfgYAML), 0600)

	watcher := config.NewWatcher(configPath, 50*time.Millisecond)
	defer watcher.Stop()
	tracker := usage.NewTracker(usagePath)
	defer tracker.Close()

	snap, err := watcher.Current()
	if err != nil {
		t.Fatalf("watcher current: %v", err)
	}
	authStore := auth.NewStore(snap.Server.PrivateKeys)
	fwd := NewStreamingForwarder()
	handler := NewReloadingHandler(authStore, fwd, watcher, tracker, nil)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	tracker.Flush()

	out, err := usage.RunStats(usagePath, "p1", "1970-01-01", "")
	if err != nil {
		t.Fatalf("runstats: %v", err)
	}
	if !strings.Contains(out, "125") || !strings.Contains(out, "25") {
		t.Fatalf("expected persisted usage in stats output, got: %s", out)
	}
	// 统计 key 应为上游真实模型名 "real"，而非 model_map 的 key "m"
	if !strings.Contains(out, "real") {
		t.Fatalf("expected stats to use upstream real model 'real', got: %s", out)
	}
}

func TestHandler_MissingModelField_400(t *testing.T) {
	h := setupTestHandler(
		map[string]string{"sk-cs-key1": "p1"},
		map[string]map[string][]string{"p1": {"m": {"cfg1"}}},
		map[string]config.Upstream{
			"cfg1": {Name: "cfg1", URL: "http://example.com", APIKey: "k", Model: "real-m", Timeout: 0},
		},
	)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"max_tokens":100}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("expected 400 for missing model field, got %d", rec.Code)
	}
}

// ── Circuit breaker integration tests ──

// TestBreaker_BackoffSkipsUpstream 验证退避期内跳过上游，故障转移到备选
func TestBreaker_BackoffSkipsUpstream(t *testing.T) {
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok from backup"}`))
	}))
	defer ts2.Close()

	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(503)
		w.Write([]byte(`{"type":"error","error":{"type":"overloaded"}}`))
	}))
	defer ts1.Close()

	cfg1 := config.Upstream{
		Name: "cfg1", URL: ts1.URL, APIKey: "k1", Model: "m1",
		Timeout: 5 * time.Second, RetryBackoff: []time.Duration{10 * time.Minute},
	}
	cfg2 := config.Upstream{
		Name: "cfg2", URL: ts2.URL, APIKey: "k2", Model: "m2",
		Timeout: 5 * time.Second,
	}

	breaker := circuitbreaker.NewBreaker()

	h := &Handler{
		auth:      auth.NewStore(map[string]string{"sk-cs-key1": "p1"}),
		resolver:  project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1", "cfg2"}}}),
		lookup:    &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg1, "cfg2": cfg2}},
		forwarder: NewStreamingForwarder(),
		log:       logging.New(logging.Off, io.Discard),
		breaker:   breaker,
	}

	// 前两次请求：cfg1 返回 503，故障转移到 cfg2 成功；cfg1 累积 2 次失败进入退避
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
		req.Header.Set("x-api-key", "sk-cs-key1")
		req.Header.Set("content-type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("request %d: expected 200 via cfg2, got %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}

	// 第三次请求：cfg1 在退避期内，直接被跳过，只用 cfg2
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 via cfg2 (cfg1 in backoff), got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestBreaker_SingleUpstream_ForcesProbe 验证单 upstream 全部被跳过时兜底探测
func TestBreaker_SingleUpstream_ForcesProbe(t *testing.T) {
	ts503 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(503)
		w.Write([]byte(`{"type":"error","error":{"type":"overloaded"}}`))
	}))
	defer ts503.Close()

	ts200 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts200.Close()

	breaker := circuitbreaker.NewBreaker()

	cfg503 := config.Upstream{
		Name: "cfg1", URL: ts503.URL, APIKey: "k1", Model: "m1",
		Timeout: 5 * time.Second, RetryBackoff: []time.Duration{10 * time.Minute},
	}

	h503 := &Handler{
		auth:      auth.NewStore(map[string]string{"sk-cs-key1": "p1"}),
		resolver:  project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1"}}}),
		lookup:    &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg503}},
		forwarder: NewStreamingForwarder(),
		log:       logging.New(logging.Off, io.Discard),
		breaker:   breaker,
	}

	// 两次 503 触发退避（每个请求 cfg1 返回可重试错误，最终 502）
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
		req.Header.Set("x-api-key", "sk-cs-key1")
		req.Header.Set("content-type", "application/json")
		rec := httptest.NewRecorder()
		h503.ServeHTTP(rec, req)
		if rec.Code != 502 {
			t.Fatalf("request %d: expected 502 (only one upstream in backoff), got %d", i+1, rec.Code)
		}
	}

	// 第三次请求：cfg1 在退避期内，但单 upstream 触发兜底强制探测
	// 换回正常的 upstream（返回 200）验证强制探测能成功
	cfg200 := config.Upstream{
		Name: "cfg1", URL: ts200.URL, APIKey: "k1", Model: "m1",
		Timeout: 5 * time.Second, RetryBackoff: []time.Duration{10 * time.Minute},
	}
	h := &Handler{
		auth:      auth.NewStore(map[string]string{"sk-cs-key1": "p1"}),
		resolver:  project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1"}}}),
		lookup:    &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg200}},
		forwarder: NewStreamingForwarder(),
		log:       logging.New(logging.Off, io.Discard),
		breaker:   breaker,
	}
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected forced probe to succeed with 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestBreaker_NoBackoffUpstream_NotAffected 验证无 backoff 的 upstream 不受影响
func TestBreaker_NoBackoffUpstream_NotAffected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	cfg1 := config.Upstream{
		Name: "cfg1", URL: ts.URL, APIKey: "k1", Model: "m1",
		Timeout: 5 * time.Second, // 无 RetryBackoff
	}

	breaker := circuitbreaker.NewBreaker()

	h := &Handler{
		auth:      auth.NewStore(map[string]string{"sk-cs-key1": "p1"}),
		resolver:  project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1"}}}),
		lookup:    &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg1}},
		forwarder: NewStreamingForwarder(),
		log:       logging.New(logging.Off, io.Discard),
		breaker:   breaker,
	}

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 for upstream without backoff, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestBreaker_4xxNotCounted 验证不可重试的 4xx 不计入熔断
func TestBreaker_4xxNotCounted(t *testing.T) {
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts2.Close()

	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error"}}`))
	}))
	defer ts1.Close()

	cfg1 := config.Upstream{
		Name: "cfg1", URL: ts1.URL, APIKey: "k1", Model: "m1",
		Timeout: 5 * time.Second, RetryBackoff: []time.Duration{10 * time.Minute},
	}
	cfg2 := config.Upstream{
		Name: "cfg2", URL: ts2.URL, APIKey: "k2", Model: "m2",
		Timeout: 5 * time.Second, RetryBackoff: []time.Duration{10 * time.Minute},
	}

	breaker := circuitbreaker.NewBreaker()

	h := &Handler{
		auth:      auth.NewStore(map[string]string{"sk-cs-key1": "p1"}),
		resolver:  project.NewResolver(map[string]map[string][]string{"p1": {"m": {"cfg1", "cfg2"}}}),
		lookup:    &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg1, "cfg2": cfg2}},
		forwarder: NewStreamingForwarder(),
		log:       logging.New(logging.Off, io.Discard),
		breaker:   breaker,
	}

	// 多次 400（不可重试），不应触发 cfg1 的熔断
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
		req.Header.Set("x-api-key", "sk-cs-key1")
		req.Header.Set("content-type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		// 400 应直接透传，不故障转移到 cfg2
		if rec.Code != 400 {
			t.Fatalf("request %d: expected 400 passthrough, got %d", i+1, rec.Code)
		}
	}

	// 确认 cfg1 未被熔断：换一个返回 200 的 upstream 验证
	ts200 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts200.Close()

	cfg1OK := config.Upstream{
		Name: "cfg1", URL: ts200.URL, APIKey: "k1", Model: "m1",
		Timeout: 5 * time.Second, RetryBackoff: []time.Duration{10 * time.Minute},
	}
	h.lookup = &configLookup{upstreams: map[string]config.Upstream{"cfg1": cfg1OK, "cfg2": cfg2}}

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("x-api-key", "sk-cs-key1")
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("cfg1 should not be in backoff (4xx not counted), expected 200, got %d", rec.Code)
	}
}
