package main

import (
	"github.com/cnstark/claude-switch/internal/auth"
	"github.com/cnstark/claude-switch/internal/config"
	"github.com/cnstark/claude-switch/internal/proxy"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	configPath := os.Getenv("CS_CONFIG")
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "无法获取用户主目录:", err)
			os.Exit(1)
		}
		configPath = filepath.Join(home, ".claude_switch", "config.yaml")
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "无法创建配置目录: %v\n", err)
		os.Exit(1)
	}

	watcher := config.NewWatcher(configPath, 2*time.Second)
	defer watcher.Stop()

	snap, err := watcher.Current()
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载配置失败:", err)
		fmt.Fprintln(os.Stderr, "请先使用 cs 命令创建配置：")
		fmt.Fprintln(os.Stderr, "  1. cs key gen                  # 生成私有 key")
		fmt.Fprintln(os.Stderr, "  2. cs project add <name> --key <sk-cs-...>  # 添加项目")
		fmt.Fprintln(os.Stderr, "  3. cs upstream add <name> --url ... --apikey ... --model ...")
		fmt.Fprintln(os.Stderr, "  4. cs mapping add <project> <model> <cfg>")
		os.Exit(1)
	}

	authStore := auth.NewStore(snap.Server.PrivateKeys)
	fwd := proxy.NewStreamingForwarder()

	handler := proxy.NewReloadingHandler(authStore, fwd, watcher)

	srv := proxy.NewServer(watcher, handler)
	if err := srv.Start(snap.Server.Listen); err != nil {
		fmt.Fprintf(os.Stderr, "服务器错误: %v\n", err)
		os.Exit(1)
	}
}

