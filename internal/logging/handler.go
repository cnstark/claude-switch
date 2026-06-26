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

// Enabled 采用 OR 语义：任一子 handler 启用即通过。
// 注意：Handle 会无条件调用两个子 handler，子 handler 内部不再复查 Enabled，
// 因此仅当两个子 handler 共用同一级别时语义一致；若级别不对称，"已禁用"的一方仍会被写入。
// 当前用法两个子 handler 共享 server 级别，无此问题。
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
