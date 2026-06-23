package proxy

import (
	"github.com/cnstark/claude-switch/internal/config"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Server 代理服务器
type Server struct {
	httpServer *http.Server
	watcher    *config.Watcher
}

// NewServer 创建代理服务器
func NewServer(watcher *config.Watcher, handler http.Handler) *Server {
	return &Server{
		httpServer: &http.Server{
			Handler: handler,
		},
		watcher: watcher,
	}
}

// Start 启动服务器并等待优雅退出
func (s *Server) Start(listenAddr string) error {
	s.httpServer.Addr = listenAddr
	fmt.Fprintf(os.Stderr, "[cs-proxy] 监听 %s\n", listenAddr)

	snap, err := s.watcher.Current()
	if err == nil {
		fmt.Fprintf(os.Stderr, "[cs-proxy] 已加载 %d 个 upstream, %d 个 project\n",
			len(snap.Upstreams), len(snap.Projects))
		fmt.Fprintf(os.Stderr, "[cs-proxy] 配置文件: %s\n", s.watcher.Path())
	}

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-errCh:
		return fmt.Errorf("服务器启动失败: %w", err)
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "[cs-proxy] 收到信号 %s，优雅退出中...\n", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("关闭服务器失败: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[cs-proxy] 已退出\n")
	return nil
}
