package logging

import (
	"context"
	"log/slog"
)

// DualHandler 同时写入两个 slog.Handler（文件 + 控制台）。
type DualHandler struct {
	a, b slog.Handler
}

// NewDualHandler 创建双写 handler。
func NewDualHandler(a, b slog.Handler) *DualHandler {
	return &DualHandler{a: a, b: b}
}

func (h *DualHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.a.Enabled(ctx, level) || h.b.Enabled(ctx, level)
}

func (h *DualHandler) Handle(ctx context.Context, r slog.Record) error {
	// 两个 handler 各写一份；任一失败返回 error
	errA := h.a.Handle(ctx, r)
	errB := h.b.Handle(ctx, r)
	if errA != nil {
		return errA
	}
	return errB
}

func (h *DualHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &DualHandler{
		a: h.a.WithAttrs(attrs),
		b: h.b.WithAttrs(attrs),
	}
}

func (h *DualHandler) WithGroup(name string) slog.Handler {
	return &DualHandler{
		a: h.a.WithGroup(name),
		b: h.b.WithGroup(name),
	}
}
