package config

import "time"

// Server 代理服务器配置
type Server struct {
	Listen      string            `yaml:"listen"`
	PrivateKeys map[string]string `yaml:"private_keys"` // key → project name
}

// Upstream 上游 API 配置（cfg）
type Upstream struct {
	Name    string        `yaml:"name"`
	URL     string        `yaml:"url"`
	APIKey  string        `yaml:"apikey"`
	Model   string        `yaml:"model"`
	Timeout time.Duration `yaml:"timeout"`
}

// LogLevel 日志级别
type LogLevel string

const (
	LogOff   LogLevel = "off"
	LogMeta  LogLevel = "meta"
	LogDebug LogLevel = "debug"
)

// Project 项目配置
type Project struct {
	Name     string              `yaml:"name"`
	LogLevel LogLevel            `yaml:"log_level"`
	ModelMap map[string][]string `yaml:"model_map"` // 请求模型名 → 有序 cfg 名列表
}

// Config 完整配置（对应 config.yaml）
type Config struct {
	Server    Server     `yaml:"server"`
	Upstreams []Upstream `yaml:"upstreams"`
	Projects  []Project  `yaml:"projects"`
}

// ConfigSnapshot 运行时快照，包含索引后的快速查找表
type ConfigSnapshot struct {
	Server    Server
	Upstreams map[string]Upstream // name → Upstream
	Projects  map[string]Project  // name → Project
	Raw       Config              // 原始配置用于序列化
}

// NewSnapshot 从 Config 构造索引后的快照
func NewSnapshot(cfg Config) ConfigSnapshot {
	us := make(map[string]Upstream, len(cfg.Upstreams))
	for _, u := range cfg.Upstreams {
		us[u.Name] = u
	}
	ps := make(map[string]Project, len(cfg.Projects))
	for _, p := range cfg.Projects {
		ps[p.Name] = p
	}
	// 默认 listen
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = "127.0.0.1:8787"
	}
	return ConfigSnapshot{
		Server:    cfg.Server,
		Upstreams: us,
		Projects:  ps,
		Raw:       cfg,
	}
}
