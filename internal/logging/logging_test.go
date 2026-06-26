package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewNopLogger_DiscardsAll(t *testing.T) {
	logger := NewNopLogger()
	logger.Info("should not appear anywhere")
	// 不 panic 即通过
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"off", slog.LevelError + 1},
		{"info", slog.LevelInfo},
		{"meta", slog.LevelInfo},
		{"debug", slog.LevelDebug},
		{"invalid", slog.LevelInfo}, // default
		{"", slog.LevelInfo},
	}
	for _, tc := range tests {
		got := ParseLevel(tc.input, slog.LevelInfo)
		if got != tc.expected {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestDailyRotateWriter_WriteAndRotate(t *testing.T) {
	dir := t.TempDir()
	w, err := NewDailyRotateWriter(dir, "test.log", 7)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	defer w.Close()

	// 写入
	_, err = w.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// 验证文件存在
	path := filepath.Join(dir, "test.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}

	// 模拟跨天：手动调用 rotate
	w.mu.Lock()
	err = w.rotate("2000-01-01")
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// 验证历史文件存在
	today := time.Now().Format("2006-01-02")
	datedPath := filepath.Join(dir, "test-"+today+".log")
	if _, err := os.Stat(datedPath); os.IsNotExist(err) {
		t.Fatalf("expected dated file %s to exist", datedPath)
	}
}

func TestDailyRotateWriter_Cleanup(t *testing.T) {
	dir := t.TempDir()
	// 创建过期文件
	oldDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	oldPath := filepath.Join(dir, "test-"+oldDate+".log")
	os.WriteFile(oldPath, []byte("old"), 0600)

	// 创建近期文件
	recentDate := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	recentPath := filepath.Join(dir, "test-"+recentDate+".log")
	os.WriteFile(recentPath, []byte("recent"), 0600)

	w, err := NewDailyRotateWriter(dir, "test.log", 7)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	defer w.Close()

	// 触发清理
	w.mu.Lock()
	w.cleanup()
	w.mu.Unlock()

	// 过期文件应被删除
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("expected old file to be deleted")
	}
	// 近期文件应保留
	if _, err := os.Stat(recentPath); os.IsNotExist(err) {
		t.Fatal("expected recent file to be kept")
	}
}

func TestDailyRotateWriter_MaxDaysZero_NoCleanup(t *testing.T) {
	dir := t.TempDir()
	oldDate := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	oldPath := filepath.Join(dir, "test-"+oldDate+".log")
	os.WriteFile(oldPath, []byte("old"), 0600)

	w, err := NewDailyRotateWriter(dir, "test.log", 0)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	defer w.Close()

	w.mu.Lock()
	w.cleanup()
	w.mu.Unlock()

	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		t.Fatal("expected old file to be kept when maxDays=0")
	}
}

func TestDualHandler_BothWritten(t *testing.T) {
	var bufA, bufB bytes.Buffer
	a := slog.NewJSONHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewJSONHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelDebug})
	dual := NewDualHandler(a, b)
	logger := slog.New(dual)

	logger.Info("test")

	if !strings.Contains(bufA.String(), "test") {
		t.Fatal("expected handler A to receive log")
	}
	if !strings.Contains(bufB.String(), "test") {
		t.Fatal("expected handler B to receive log")
	}
}

func TestNewStdErrLogger(t *testing.T) {
	logger := NewStdErrLogger(slog.LevelInfo)
	logger.Info("stderr test")
	// 不 panic 即通过
}
