package config

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Watcher 监控配置文件 mtime，变更时热重载
type Watcher struct {
	path     string
	interval time.Duration
	mu       sync.RWMutex
	snap     *ConfigSnapshot // 当前有效配置
	mtime    time.Time
	stopCh   chan struct{}
}

// NewWatcher 创建热重载监控器，启动后台轮询
func NewWatcher(path string, interval time.Duration) *Watcher {
	w := &Watcher{
		path:     path,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
	// 初始加载
	snap, err := LoadFile(path)
	if err == nil {
		fi, _ := os.Stat(path)
		w.mtime = fi.ModTime()
		w.snap = &snap
	}
	go w.loop()
	return w
}

func (w *Watcher) loop() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.checkAndReload()
		case <-w.stopCh:
			return
		}
	}
}

func (w *Watcher) checkAndReload() {
	fi, err := os.Stat(w.path)
	if err != nil {
		return
	}
	w.mu.RLock()
	currentMtime := w.mtime
	w.mu.RUnlock()

	if !fi.ModTime().After(currentMtime) {
		return
	}

	snap, err := LoadFile(w.path)
	if err != nil {
		// 重载失败，保留旧配置，输出警告
		fmt.Fprintf(os.Stderr, "[cs-proxy] 配置重载失败（保留旧配置）: %v\n", err)
		return
	}

	w.mu.Lock()
	w.snap = &snap
	w.mtime = fi.ModTime()
	w.mu.Unlock()
}

// Current 返回当前快照（带错误检查）
func (w *Watcher) Current() (ConfigSnapshot, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.snap == nil {
		return ConfigSnapshot{}, os.ErrNotExist
	}
	return *w.snap, nil
}

// GetSnapshot 返回当前快照指针（供代理热路径零拷贝使用）
func (w *Watcher) GetSnapshot() *ConfigSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snap
}

// Path 返回监控的配置文件路径
func (w *Watcher) Path() string {
	return w.path
}

// Stop 停止后台轮询
func (w *Watcher) Stop() {
	close(w.stopCh)
}
