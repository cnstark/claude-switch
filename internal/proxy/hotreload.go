package proxy

import (
	"github.com/cnstark/claude-switch/internal/auth"
	"github.com/cnstark/claude-switch/internal/config"
	"github.com/cnstark/claude-switch/internal/logging"
	"github.com/cnstark/claude-switch/internal/project"
	"github.com/cnstark/claude-switch/internal/usage"
	"net/http"
	"os"
)

// ReloadingHandler 每次请求从 watcher 获取最新配置快照的热重载 handler。
// tracker 不在 snapshot 里（计数器是累积状态，不随配置热重载），挂在 handler 上每请求透传。
type ReloadingHandler struct {
	authStore *auth.Store
	forwarder Forwarder
	watcher   *config.Watcher
	tracker   usage.Recorder
}

// NewReloadingHandler 创建支持热重载的 handler。tracker 为进程级 usage 记录器。
func NewReloadingHandler(
	authStore *auth.Store,
	forwarder Forwarder,
	watcher *config.Watcher,
	tracker usage.Recorder,
) *ReloadingHandler {
	return &ReloadingHandler{
		authStore: authStore,
		forwarder: forwarder,
		watcher:   watcher,
		tracker:   tracker,
	}
}

// ServeHTTP 每次请求从 watcher 重建 resolver/lookup
func (h *ReloadingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	snap := h.watcher.GetSnapshot()
	if snap == nil {
		writeError(w, http.StatusServiceUnavailable, "config_error", "配置未加载")
		return
	}

	// Rebuild auth keys on each request (allows key rotation via hot-reload)
	h.authStore.Update(snap.Server.PrivateKeys)

	// Build resolver from current snapshot
	projData := make(map[string]map[string][]string, len(snap.Projects))
	projectLogLevels := make(map[string]config.LogLevel, len(snap.Projects))
	for name, p := range snap.Projects {
		projData[name] = p.ModelMap
		projectLogLevels[name] = p.LogLevel
	}
	resolver := project.NewResolver(projData)
	lookup := &snapshotLookup{snap: snap}

	// 初始 logger 使用 Meta 级别，确保鉴权失败等关键事件被记录。
	// 鉴权成功后 Handler 会根据请求所属 project 的 log_level 动态调整。
	log := logging.New(logging.Meta, os.Stderr)

	// Build handler with current snapshot dependencies
	handler := &Handler{
		auth:             h.authStore,
		resolver:         resolver,
		lookup:           lookup,
		forwarder:        h.forwarder,
		log:              log,
		tracker:          h.tracker,
		usageEnabled:     snap.Server.UsageStats,
		projectLogLevels: projectLogLevels,
	}

	handler.ServeHTTP(w, r)
}

// snapshotLookup implements ConfigLookup from snapshot
type snapshotLookup struct {
	snap *config.ConfigSnapshot
}

func (l *snapshotLookup) Upstream(name string) (config.Upstream, bool) {
	u, ok := l.snap.Upstreams[name]
	return u, ok
}
