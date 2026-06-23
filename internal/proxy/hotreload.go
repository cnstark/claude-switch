package proxy

import (
	"github.com/cnstark/claude-switch/internal/auth"
	"github.com/cnstark/claude-switch/internal/config"
	"github.com/cnstark/claude-switch/internal/logging"
	"github.com/cnstark/claude-switch/internal/project"
	"net/http"
	"os"
)

// ReloadingHandler 每次请求从 watcher 获取最新配置快照的热重载 handler
type ReloadingHandler struct {
	authStore *auth.Store
	forwarder Forwarder
	watcher   *config.Watcher
}

// NewReloadingHandler 创建支持热重载的 handler
func NewReloadingHandler(
	authStore *auth.Store,
	forwarder Forwarder,
	watcher *config.Watcher,
) *ReloadingHandler {
	return &ReloadingHandler{
		authStore: authStore,
		forwarder: forwarder,
		watcher:   watcher,
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
	var currentLogLevel string
	for name, p := range snap.Projects {
		projData[name] = p.ModelMap
		// Track log level for the first project (used for request logging)
		if currentLogLevel == "" {
			currentLogLevel = string(p.LogLevel)
		}
	}
	resolver := project.NewResolver(projData)
	lookup := &snapshotLookup{snap: snap}

	// Determine log level from the first project's config (default: off)
	lvl := logging.Off
	for _, p := range snap.Projects {
		switch p.LogLevel {
		case "debug", "Debug":
			lvl = logging.Debug
		case "meta", "Meta":
			lvl = logging.Meta
		}
		break // use first project's log level
	}
	log := logging.New(lvl, os.Stderr)

	// Build handler with current snapshot dependencies
	handler := &Handler{
		auth:      h.authStore,
		resolver:  resolver,
		lookup:    lookup,
		forwarder: h.forwarder,
		log:       log,
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
