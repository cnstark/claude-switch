// Package logging 日志基础设施（基于 log/slog）
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DailyRotateWriter 按天轮转的文件写入器，实现 io.Writer。
// 每次 Write 检查日期是否跨天，跨天则关闭旧文件、创建新文件。
// 轮转时自动清理超过 maxDays 天的历史文件（maxDays=0 永不清理）。
type DailyRotateWriter struct {
	dir      string
	baseName string
	maxDays  int

	mu      sync.Mutex
	file    *os.File
	curDate string // "2006-01-02"
}

// NewDailyRotateWriter 创建按天轮转写入器。自动创建 dir 目录（0700）。
func NewDailyRotateWriter(dir, baseName string, maxDays int) (*DailyRotateWriter, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("创建日志目录 %s 失败: %w", dir, err)
	}
	w := &DailyRotateWriter{
		dir:      dir,
		baseName: baseName,
		maxDays:  maxDays,
	}
	if err := w.openFile(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *DailyRotateWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if today != w.curDate {
		if err := w.rotate(today); err != nil {
			return 0, err
		}
	}
	return w.file.Write(p)
}

// Close 关闭当前文件
func (w *DailyRotateWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

func (w *DailyRotateWriter) openFile() error {
	today := time.Now().Format("2006-01-02")
	path := filepath.Join(w.dir, w.baseName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("打开日志文件 %s 失败: %w", path, err)
	}
	w.file = f
	w.curDate = today
	return nil
}

func (w *DailyRotateWriter) rotate(today string) error {
	// 关闭当前文件
	if w.file != nil {
		w.file.Close()
	}

	// 将当天文件重命名为带日期的历史文件
	curPath := filepath.Join(w.dir, w.baseName)
	datedName := strings.TrimSuffix(w.baseName, ".log") + "-" + w.curDate + ".log"
	datedPath := filepath.Join(w.dir, datedName)
	if _, err := os.Stat(curPath); err == nil {
		os.Rename(curPath, datedPath)
	}

	// 清理过期文件
	if w.maxDays > 0 {
		w.cleanup()
	}

	// 打开新文件
	return w.openFile()
}

func (w *DailyRotateWriter) cleanup() {
	// maxDays<=0 表示永久保留历史文件，不清理（rotate 也会跳过调用，此处为防御性短路）。
	if w.maxDays <= 0 {
		return
	}
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -w.maxDays)
	prefix := strings.TrimSuffix(w.baseName, ".log") + "-"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		dateStr := strings.TrimPrefix(name, prefix)
		dateStr = strings.TrimSuffix(dateStr, ".log")
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if fileDate.Before(cutoff) {
			os.Remove(filepath.Join(w.dir, name))
		}
	}
}
