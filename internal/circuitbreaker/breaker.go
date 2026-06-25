// Package circuitbreaker 提供上游熔断器，支持多档退避恢复。
// 连续失败 2 次后进入退避；退避到期后放行一次探测，成功则恢复、失败则升级。
package circuitbreaker

import (
	"fmt"
	"sync"
	"time"
)

const failThreshold = 2 // 连续失败阈值

// upstreamState 单个 upstream 的熔断状态
type upstreamState struct {
	level            int       // 0=正常, 1-4=退避档位
	consecutiveFails int       // 当前连续失败次数
	nextAvailableAt  time.Time // 退避到期时间，level=0 时为零值
}

// Breaker 线程安全的熔断器
type Breaker struct {
	mu     sync.Mutex
	states map[string]*upstreamState
}

// NewBreaker 创建新的熔断器实例
func NewBreaker() *Breaker {
	return &Breaker{
		states: make(map[string]*upstreamState),
	}
}

// IsAvailable 返回该 upstream 当前是否可尝试。
// backoff 为空或 nil 时始终返回 true（不启用熔断）。
// 不可用时返回原因字符串（用于日志）。
func (b *Breaker) IsAvailable(name string, backoff []time.Duration) (bool, string) {
	if len(backoff) == 0 {
		return true, ""
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.states[name]
	if !ok || state.level == 0 {
		return true, ""
	}

	now := time.Now()
	if now.Before(state.nextAvailableAt) {
		remaining := state.nextAvailableAt.Sub(now).Round(time.Second)
		return false, fmt.Sprintf("upstream %q in backoff L%d, available in %s", name, state.level, remaining)
	}

	// 退避到期，放行一次探测
	return true, ""
}

// RecordFailure 记录一次可重试失败。
// 调用前应确保已通过 IsAvailable 检查。
// 返回状态变更描述（进入退避/升级档位/循环），用于日志；无变更返回空字符串。
func (b *Breaker) RecordFailure(name string, backoff []time.Duration) string {
	if len(backoff) == 0 {
		return ""
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	state := b.ensureState(name)
	state.consecutiveFails++

	if state.level == 0 && state.consecutiveFails < failThreshold {
		// 正常状态下未达阈值，不进入退避
		return ""
	}

	// 触发退避或升级
	prevLevel := state.level

	if state.level == 0 {
		// 首次进入退避：L1
		state.level = 1
	} else if state.level < len(backoff) {
		// 升级到下一档
		state.level++
	} else {
		// L4 失败 → 循环回 L1
		state.level = 1
	}

	state.consecutiveFails = 1 // 退避到期后首次探测失败，计数重置为 1
	tier := state.level - 1    // backoff 索引
	state.nextAvailableAt = time.Now().Add(backoff[tier])

	if prevLevel == 0 {
		return fmt.Sprintf("upstream %q entered backoff L%d (%s)", name, state.level, backoff[tier])
	} else if state.level > prevLevel {
		return fmt.Sprintf("upstream %q escalated to backoff L%d (%s)", name, state.level, backoff[tier])
	} else {
		// state.level == 1 && prevLevel == len(backoff)：循环回 L1
		return fmt.Sprintf("upstream %q cycled back to backoff L1 (%s)", name, backoff[tier])
	}
}

// RecordSuccess 记录成功，重置所有熔断状态。
// 返回 "recovered" 如果是从退避中恢复，否则返回空字符串。
func (b *Breaker) RecordSuccess(name string) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.states[name]
	if !ok {
		return ""
	}

	wasInBackoff := state.level > 0
	state.level = 0
	state.consecutiveFails = 0
	state.nextAvailableAt = time.Time{}

	if wasInBackoff {
		return fmt.Sprintf("upstream %q recovered from backoff", name)
	}
	return ""
}

// ensureState 获取或创建 upstream 状态（调用方需持有锁）
func (b *Breaker) ensureState(name string) *upstreamState {
	state, ok := b.states[name]
	if !ok {
		state = &upstreamState{}
		b.states[name] = state
	}
	return state
}