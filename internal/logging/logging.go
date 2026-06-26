package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// NewStdErrLogger 创建仅输出到 stderr 的 TextHandler logger，用于启动阶段。
func NewStdErrLogger(level slog.Leveler) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// NewLogger 创建双写 logger（文件 JSON + stderr Text）。
// logFile 为空时使用 DefaultLogDir()/cs-proxy.log。
func NewLogger(level slog.Leveler, logFile string, maxDays int) (*slog.Logger, error) {
	if logFile == "" {
		logFile = filepath.Join(DefaultLogDir(), "cs-proxy.log")
	}
	dir := filepath.Dir(logFile)
	baseName := filepath.Base(logFile)

	fileWriter, err := NewDailyRotateWriter(dir, baseName, maxDays)
	if err != nil {
		return nil, err
	}

	jsonHandler := slog.NewJSONHandler(fileWriter, &slog.HandlerOptions{Level: level})
	textHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	dualHandler := NewDualHandler(jsonHandler, textHandler)

	return slog.New(dualHandler), nil
}

// ParseLevel 将配置字符串转换为 slog.Level。无效值返回 defaultLevel。
func ParseLevel(s string, defaultLevel slog.Level) slog.Level {
	switch s {
	case "off":
		return slog.LevelError + 1
	case "info", "meta":
		return slog.LevelInfo
	case "debug":
		return slog.LevelDebug
	default:
		return defaultLevel
	}
}

// DefaultLogDir 返回默认日志目录 ~/.claude_switch/logs/。
func DefaultLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "logs")
	}
	return filepath.Join(home, ".claude_switch", "logs")
}

// NewNopLogger 创建丢弃所有输出的 logger，用于测试。
func NewNopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// MaskKey 脱敏 API key：保留前 8 字符 + "..." + 后 4 字符（短 key 适配）。
func MaskKey(key string) string {
	n := len(key)
	if n == 0 {
		return "..."
	}
	if n <= 4 {
		return key[:1] + "..."
	}
	if n <= 12 {
		return key[:4] + "..."
	}
	return key[:8] + "..." + key[n-4:]
}
