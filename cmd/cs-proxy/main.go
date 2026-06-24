package main

import (
	"fmt"
	"github.com/cnstark/claude-switch/internal/auth"
	"github.com/cnstark/claude-switch/internal/config"
	"github.com/cnstark/claude-switch/internal/proxy"
	"github.com/cnstark/claude-switch/internal/usage"
	"os"
	"path/filepath"
	"time"
)

// version 在构建时通过 -ldflags 注入，默认值为 "dev"
var version = "dev"

func main() {
	// --version 直接输出版本号并退出
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("cs-proxy version", version)
		os.Exit(0)
	}

	configPath := os.Getenv("CS_CONFIG")
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "无法获取用户主目录:", err)
			os.Exit(1)
		}
		configPath = filepath.Join(home, ".claude_switch", "config.yaml")
	}

	// 确保配置文件存在，不存在则自动创建默认配置
	if key, err := config.EnsureConfig(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "创建配置文件失败: %v\n", err)
		os.Exit(1)
	} else if key != "" {
		fmt.Fprintf(os.Stderr, "已创建默认配置文件: %s\n", configPath)
		fmt.Fprintf(os.Stderr, "默认私有 key: %s\n", key)
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
		fmt.Fprintln(os.Stderr, "请先使用 cs 命令完善配置：")
		fmt.Fprintln(os.Stderr, "  1. cs upstream add <name> --url ... --apikey ... --model ...")
		fmt.Fprintln(os.Stderr, "  2. cs mapping add default <请求模型名> <上游名>")
		fmt.Fprintln(os.Stderr, "  3. cs proxy restart")
		os.Exit(1)
	}

	// CS_LISTEN 环境变量覆盖配置文件中的 listen 地址。
	// Docker 部署时需要设为 0.0.0.0:8787，因为容器内 127.0.0.1 不接收
	// docker-proxy 通过 veth 网桥发来的连接。
	if envListen := os.Getenv("CS_LISTEN"); envListen != "" {
		snap.Server.Listen = envListen
	}

	// usage tracker：进程级单例，加载历史 usage.json 并启动后台刷盘。
	// usage_stats 关闭时仍创建（保留历史数据、随时可热重载开启），仅不产生新记录。
	usagePath := filepath.Join(filepath.Dir(configPath), "usage.json")
	tracker := usage.NewTracker(usagePath)
	defer tracker.Close() // 优雅退出时 final flush

	authStore := auth.NewStore(snap.Server.PrivateKeys)
	fwd := proxy.NewStreamingForwarder()

	handler := proxy.NewReloadingHandler(authStore, fwd, watcher, tracker)

	srv := proxy.NewServer(watcher, handler)
	if err := srv.Start(snap.Server.Listen); err != nil {
		fmt.Fprintf(os.Stderr, "服务器错误: %v\n", err)
		os.Exit(1)
	}
}
