package proxy

import (
	"github.com/cnstark/claude-switch/internal/auth"
	"github.com/cnstark/claude-switch/internal/config"
	"github.com/cnstark/claude-switch/internal/logging"
	"github.com/cnstark/claude-switch/internal/project"
	"github.com/cnstark/claude-switch/internal/upstream"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
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
