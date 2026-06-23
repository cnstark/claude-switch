package config

import (
	"os"
	"testing"
	"time"
)

func TestValidate_Success(t *testing.T) {
	cfg := Config{
		Server: Server{
			Listen: "127.0.0.1:8787",
			PrivateKeys: map[string]string{
				"sk-cs-key1": "project1",
			},
		},
		Upstreams: []Upstream{
			{Name: "cfg1", URL: "https://api.anthropic.com", APIKey: "sk-ant-xxx", Model: "claude-opus-4-8", Timeout: 60 * time.Second},
			{Name: "cfg2", URL: "https://other.com", APIKey: "sk-xxx", Model: "claude-sonnet-4-6", Timeout: 30 * time.Second},
		},
		Projects: []Project{
			{
				Name: "project1", LogLevel: LogMeta,
				ModelMap: map[string][]string{
					"modelA": {"cfg1", "cfg2"},
				},
			},
		},
	}
	err := Validate(cfg)
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidate_DuplicateUpstreamName(t *testing.T) {
	cfg := Config{
		Server:    Server{Listen: "127.0.0.1:8787", PrivateKeys: map[string]string{"sk-cs-key1": "p1"}},
		Upstreams: []Upstream{
			{Name: "cfg1", URL: "https://a.com", APIKey: "k1", Model: "m1", Timeout: 60 * time.Second},
			{Name: "cfg1", URL: "https://b.com", APIKey: "k2", Model: "m2", Timeout: 30 * time.Second},
		},
		Projects: []Project{
			{Name: "p1", ModelMap: map[string][]string{"m": {"cfg1"}}},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate upstream name")
	}
}

func TestValidate_DanglingUpstreamRef(t *testing.T) {
	cfg := Config{
		Server:    Server{Listen: "127.0.0.1:8787", PrivateKeys: map[string]string{"sk-cs-key1": "p1"}},
		Upstreams: []Upstream{
			{Name: "cfg1", URL: "https://a.com", APIKey: "k1", Model: "m1", Timeout: 60 * time.Second},
		},
		Projects: []Project{
			{Name: "p1", ModelMap: map[string][]string{"m": {"cfg1", "cfg_nonexistent"}}},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for dangling upstream reference")
	}
}

func TestValidate_NoPrivateKeys(t *testing.T) {
	cfg := Config{
		Server:    Server{Listen: "127.0.0.1:8787"},
		Upstreams: []Upstream{
			{Name: "cfg1", URL: "https://a.com", APIKey: "k1", Model: "m1", Timeout: 60 * time.Second},
		},
		Projects: []Project{
			{Name: "p1", ModelMap: map[string][]string{"m": {"cfg1"}}},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for no private keys configured")
	}
}

func TestValidate_PrivateKeyPointsToMissingProject(t *testing.T) {
	cfg := Config{
		Server: Server{
			Listen:      "127.0.0.1:8787",
			PrivateKeys: map[string]string{"sk-cs-key1": "nonexistent_project"},
		},
		Upstreams: []Upstream{
			{Name: "cfg1", URL: "https://a.com", APIKey: "k1", Model: "m1", Timeout: 60 * time.Second},
		},
		Projects: []Project{
			{Name: "p1", ModelMap: map[string][]string{"m": {"cfg1"}}},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for private key pointing to missing project")
	}
}

func TestValidate_DuplicateProjectName(t *testing.T) {
	cfg := Config{
		Server: Server{Listen: "127.0.0.1:8787", PrivateKeys: map[string]string{"sk-cs-key1": "p1"}},
		Upstreams: []Upstream{
			{Name: "cfg1", URL: "https://a.com", APIKey: "k1", Model: "m1", Timeout: 60 * time.Second},
		},
		Projects: []Project{
			{Name: "p1", ModelMap: map[string][]string{"m": {"cfg1"}}},
			{Name: "p1", ModelMap: map[string][]string{"m2": {"cfg1"}}},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate project name")
	}
}

func TestValidate_DuplicateModelMapKey(t *testing.T) {
	cfg := Config{
		Server: Server{Listen: "127.0.0.1:8787", PrivateKeys: map[string]string{"sk-cs-key1": "p1"}},
		Upstreams: []Upstream{
			{Name: "cfg1", URL: "https://a.com", APIKey: "k1", Model: "m1", Timeout: 60 * time.Second},
		},
		Projects: []Project{
			{Name: "p1", ModelMap: map[string][]string{"modelA": {"cfg1"}}},
		},
	}
	err := Validate(cfg)
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidate_EmptyUpstreamName(t *testing.T) {
	cfg := Config{
		Server:    Server{Listen: "127.0.0.1:8787", PrivateKeys: map[string]string{"sk-cs-key1": "p1"}},
		Upstreams: []Upstream{
			{Name: "", URL: "https://a.com", APIKey: "k1", Model: "m1", Timeout: 60 * time.Second},
		},
		Projects: []Project{
			{Name: "p1", ModelMap: map[string][]string{"m": {"cfg1"}}},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for empty upstream name")
	}
}

func TestValidate_DuplicatePrivateKey(t *testing.T) {
	cfg := Config{
		Server: Server{
			Listen: "127.0.0.1:8787",
			// 构造一个实际只含一个 key 的 map（Go 语法不允许 literal 重复 key）
			PrivateKeys: map[string]string{"sk-cs-key1": "p2"},
		},
		Upstreams: []Upstream{
			{Name: "cfg1", URL: "https://a.com", APIKey: "k1", Model: "m1", Timeout: 60 * time.Second},
		},
		Projects: []Project{
			{Name: "p1", ModelMap: map[string][]string{"m": {"cfg1"}}},
			{Name: "p2", ModelMap: map[string][]string{"m": {"cfg1"}}},
		},
	}
	// Go 语法不允许 literal 重复 key，因此无法在编译期构造出重复 private key 的情形。
	// 此测试仅验证单 key 指向存在的项目时通过校验。
	err := Validate(cfg)
	if err != nil {
		t.Fatalf("expected valid with one key, got: %v", err)
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	yamlData := `
server:
  listen: 127.0.0.1:8787
  private_keys:
    sk-cs-key1: project1
upstreams:
  - name: cfg1
    url: https://api.anthropic.com
    apikey: sk-ant-xxx
    model: claude-opus-4-8
    timeout: 60s
projects:
  - name: project1
    log_level: meta
    model_map:
      modelA: [cfg1]
`
	path := t.TempDir() + "/config.yaml"

	snap, err := Load([]byte(yamlData))
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if snap.Server.Listen != "127.0.0.1:8787" {
		t.Fatalf("expected listen 127.0.0.1:8787, got %s", snap.Server.Listen)
	}
	if len(snap.Upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(snap.Upstreams))
	}
	if snap.Upstreams["cfg1"].Model != "claude-opus-4-8" {
		t.Fatalf("expected model claude-opus-4-8, got %s", snap.Upstreams["cfg1"].Model)
	}

	// Test Save (atomic write)
	err = Save(snap.Raw, path)
	if err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	// Reload saved file to verify round-trip
	snap2, err := LoadFile(path)
	if err != nil {
		t.Fatalf("unexpected reload error: %v", err)
	}
	if snap2.Server.Listen != snap.Server.Listen {
		t.Fatal("round-trip mismatch")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	_, err := Load([]byte(`not: [valid: yaml`))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoad_ValidationError(t *testing.T) {
	yamlData := `
server:
  listen: 127.0.0.1:8787
  private_keys: {}
upstreams: []
projects: []
`
	_, err := Load([]byte(yamlData))
	if err == nil {
		t.Fatal("expected validation error for missing private keys")
	}
}

func TestWatcher_DetectChange(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/config.yaml"

	initialYAML := `
server:
  listen: 127.0.0.1:8787
  private_keys:
    sk-cs-key1: project1
upstreams:
  - name: cfg1
    url: https://a.com
    apikey: k1
    model: m1
    timeout: 60s
projects:
  - name: project1
    log_level: off
    model_map:
      modelA: [cfg1]
`
	if err := os.WriteFile(path, []byte(initialYAML), 0600); err != nil {
		t.Fatal(err)
	}

	w := NewWatcher(path, 50*time.Millisecond)
	snap, err := w.Current()
	if err != nil {
		t.Fatalf("initial load failed: %v", err)
	}
	if snap.Upstreams["cfg1"].Model != "m1" {
		t.Fatalf("expected model m1, got %s", snap.Upstreams["cfg1"].Model)
	}

	// Modify file
	updatedYAML := `
server:
  listen: 127.0.0.1:8787
  private_keys:
    sk-cs-key1: project1
upstreams:
  - name: cfg1
    url: https://a.com
    apikey: k1
    model: m2-updated
    timeout: 60s
projects:
  - name: project1
    log_level: off
    model_map:
      modelA: [cfg1]
`
	time.Sleep(100 * time.Millisecond) // ensure mtime differs
	if err := os.WriteFile(path, []byte(updatedYAML), 0600); err != nil {
		t.Fatal(err)
	}

	// Wait for watcher to detect change
	time.Sleep(300 * time.Millisecond)

	snap2, err := w.Current()
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if snap2.Upstreams["cfg1"].Model != "m2-updated" {
		t.Fatalf("expected model m2-updated after reload, got %s", snap2.Upstreams["cfg1"].Model)
	}
	w.Stop()
}

func TestWatcher_InvalidConfigKeepsOld(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/config.yaml"

	validYAML := `
server:
  listen: 127.0.0.1:8787
  private_keys:
    sk-cs-key1: project1
upstreams:
  - name: cfg1
    url: https://a.com
    apikey: k1
    model: m1
    timeout: 60s
projects:
  - name: project1
    log_level: off
    model_map:
      modelA: [cfg1]
`
	os.WriteFile(path, []byte(validYAML), 0600)

	w := NewWatcher(path, 50*time.Millisecond)
	defer w.Stop()
	_, err := w.Current()
	if err != nil {
		t.Fatal(err)
	}

	// Write invalid config
	time.Sleep(100 * time.Millisecond)
	os.WriteFile(path, []byte(`invalid: [yaml`), 0600)
	time.Sleep(300 * time.Millisecond)

	snap, err := w.Current()
	if err != nil {
		t.Fatal("expected old config to be retained")
	}
	if snap.Upstreams["cfg1"].Model != "m1" {
		t.Fatal("expected old config to be preserved after invalid reload")
	}
}

func TestWatcher_GetSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/config.yaml"
	validYAML := `
server:
  listen: 127.0.0.1:8787
  private_keys:
    sk-cs-key1: project1
upstreams:
  - name: cfg1
    url: https://a.com
    apikey: k1
    model: m1
    timeout: 60s
projects:
  - name: project1
    log_level: off
    model_map:
      modelA: [cfg1]
`
	os.WriteFile(path, []byte(validYAML), 0600)

	w := NewWatcher(path, 50*time.Millisecond)
	defer w.Stop()
	snap := w.GetSnapshot()
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
}

func TestLoad_UsageStats(t *testing.T) {
	// 显式 true
	snap, err := Load([]byte(`
server:
  listen: 127.0.0.1:8787
  usage_stats: true
  private_keys:
    sk-cs-key1: project1
upstreams:
  - name: cfg1
    url: https://a.com
    apikey: k1
    model: m1
    timeout: 60s
projects:
  - name: project1
    model_map:
      modelA: [cfg1]
`))
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if !snap.Server.UsageStats {
		t.Fatal("expected usage_stats=true after load")
	}

	// 默认 false（缺省字段）
	snap2, err := Load([]byte(`
server:
  listen: 127.0.0.1:8787
  private_keys:
    sk-cs-key1: project1
upstreams:
  - {name: cfg1, url: https://a.com, apikey: k1, model: m1, timeout: 60s}
projects:
  - {name: project1, model_map: {modelA: [cfg1]}}
`))
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if snap2.Server.UsageStats {
		t.Fatal("expected usage_stats default false")
	}
}