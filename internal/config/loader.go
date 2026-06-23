// Package config 配置加载与原子写入
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load 从 YAML 字节加载配置，返回校验通过的快照
func Load(data []byte) (ConfigSnapshot, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ConfigSnapshot{}, fmt.Errorf("yaml 解析失败: %w", err)
	}
	if err := Validate(cfg); err != nil {
		return ConfigSnapshot{}, fmt.Errorf("配置校验失败: %w", err)
	}
	// 填充默认值
	for i := range cfg.Projects {
		if cfg.Projects[i].LogLevel == "" {
			cfg.Projects[i].LogLevel = LogOff
		}
	}
	return NewSnapshot(cfg), nil
}

// LoadFile 从文件路径加载配置
func LoadFile(path string) (ConfigSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ConfigSnapshot{}, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}
	return Load(data)
}

// Save 原子写入配置到文件（写临时文件 → rename）
func Save(cfg Config, path string) error {
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("原子替换配置文件失败: %w", err)
	}
	return nil
}
