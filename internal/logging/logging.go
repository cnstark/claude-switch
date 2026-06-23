package logging

import (
	"encoding/json"
	"io"
	"time"
)

// Level 日志级别
type Level int

const (
	Off   Level = 0
	Meta  Level = 1
	Debug Level = 2
)

// Logger 结构化 JSON 行日志
type Logger struct {
	level  Level
	writer io.Writer
}

// New 创建 Logger
func New(level Level, w io.Writer) *Logger {
	return &Logger{level: level, writer: w}
}

// Info 记录 Meta 级别日志
func (l *Logger) Info(msg string, fields map[string]any) {
	l.log(Meta, msg, fields)
}

// Debug 记录 Debug 级别日志
func (l *Logger) Debug(msg string, fields map[string]any) {
	l.log(Debug, msg, fields)
}

// Log 记录 Meta 级别信息（与 Info 等价）。受 off 级别限制。
func (l *Logger) Log(msg string, fields map[string]any) {
	l.log(Meta, msg, fields)
}

func (l *Logger) log(minLevel Level, msg string, fields map[string]any) {
	if l.level < minLevel {
		return
	}
	entry := make(map[string]any, len(fields)+3)
	entry["time"] = time.Now().UTC().Format(time.RFC3339)
	entry["level"] = int(minLevel)
	entry["msg"] = msg
	for k, v := range fields {
		entry[k] = v
	}
	b, _ := json.Marshal(entry)
	l.writer.Write(append(b, '\n'))
}

// Level 返回当前日志级别
func (l *Logger) Level() Level {
	return l.level
}
