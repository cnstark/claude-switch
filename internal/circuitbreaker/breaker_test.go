package circuitbreaker

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBreaker_NoBackoff_AlwaysAvailable(t *testing.T) {
	b := NewBreaker()
	avail, reason := b.IsAvailable("cfg1", nil)
	if !avail {
		t.Fatalf("expected always available with nil backoff, got: %s", reason)
	}

	msg := b.RecordFailure("cfg1", nil)
	if msg != "" {
		t.Fatalf("expected no message for nil backoff, got: %s", msg)
	}

	avail, _ = b.IsAvailable("cfg1", []time.Duration{})
	if !avail {
		t.Fatal("expected always available with empty backoff")
	}
}

func TestBreaker_UnknownUpstream_Available(t *testing.T) {
	b := NewBreaker()
	avail, reason := b.IsAvailable("nonexistent", []time.Duration{30 * time.Second})
	if !avail {
		t.Fatalf("expected unknown upstream to be available, got: %s", reason)
	}
}

func TestBreaker_NormalState_Available(t *testing.T) {
	backoff := []time.Duration{30 * time.Second, 2 * time.Minute}
	b := NewBreaker()

	avail, _ := b.IsAvailable("cfg1", backoff)
	if !avail {
		t.Fatal("expected available in normal state")
	}

	// 第 1 次失败不触发退避
	msg := b.RecordFailure("cfg1", backoff)
	if msg != "" {
		t.Fatalf("expected no message after 1st failure, got: %s", msg)
	}

	avail, _ = b.IsAvailable("cfg1", backoff)
	if !avail {
		t.Fatal("expected still available after 1 failure")
	}
}

func TestBreaker_TriggersBackoff_AfterTwoFailures(t *testing.T) {
	backoff := []time.Duration{30 * time.Second, 2 * time.Minute}
	b := NewBreaker()

	// 第 1 次失败
	_ = b.RecordFailure("cfg1", backoff)
	// 第 2 次失败 → 进入退避
	msg := b.RecordFailure("cfg1", backoff)
	if msg == "" || !strings.Contains(msg, "entered backoff L1") {
		t.Fatalf("expected 'entered backoff L1' message, got: %s", msg)
	}

	// 退避期内不可用
	avail, reason := b.IsAvailable("cfg1", backoff)
	if avail {
		t.Fatal("expected unavailable during backoff")
	}
	if !strings.Contains(reason, "in backoff L1") {
		t.Fatalf("expected backoff reason, got: %s", reason)
	}
}

func TestBreaker_BackoffExpires_Available(t *testing.T) {
	backoff := []time.Duration{10 * time.Millisecond}
	b := NewBreaker()

	// 触发退避
	_ = b.RecordFailure("cfg1", backoff)
	_ = b.RecordFailure("cfg1", backoff)

	avail, _ := b.IsAvailable("cfg1", backoff)
	if avail {
		t.Fatal("expected unavailable immediately after backoff triggered")
	}

	// 等待退避到期
	time.Sleep(20 * time.Millisecond)

	avail, reason := b.IsAvailable("cfg1", backoff)
	if !avail {
		t.Fatalf("expected available after backoff expired, got: %s", reason)
	}
}

func TestBreaker_ProbeSuccess_Recovers(t *testing.T) {
	backoff := []time.Duration{10 * time.Millisecond}
	b := NewBreaker()

	// 触发退避
	_ = b.RecordFailure("cfg1", backoff)
	_ = b.RecordFailure("cfg1", backoff)

	// 等待退避到期
	time.Sleep(20 * time.Millisecond)

	// 探测成功 → 恢复
	msg := b.RecordSuccess("cfg1")
	if !strings.Contains(msg, "recovered from backoff") {
		t.Fatalf("expected recovery message, got: %s", msg)
	}

	avail, _ := b.IsAvailable("cfg1", backoff)
	if !avail {
		t.Fatal("expected available after recovery")
	}

	// 再次确认失败计数已重置：一次失败不应触发退避
	msg2 := b.RecordFailure("cfg1", backoff)
	if msg2 != "" {
		t.Fatalf("expected no backoff after reset + 1 failure, got: %s", msg2)
	}
}

func TestBreaker_ProbeFailure_Escalates(t *testing.T) {
	backoff := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}
	b := NewBreaker()

	// 触发退避 L1
	_ = b.RecordFailure("cfg1", backoff)
	_ = b.RecordFailure("cfg1", backoff)

	time.Sleep(20 * time.Millisecond)

	// 退避到期后探测失败 → 升级到 L2
	msg := b.RecordFailure("cfg1", backoff)
	if !strings.Contains(msg, "escalated to backoff L2") {
		t.Fatalf("expected 'escalated to backoff L2', got: %s", msg)
	}

	// L2 退避期内不可用
	avail, reason := b.IsAvailable("cfg1", backoff)
	if avail {
		t.Fatal("expected unavailable during L2 backoff")
	}
	if !strings.Contains(reason, "in backoff L2") {
		t.Fatalf("expected L2 reason, got: %s", reason)
	}
}

func TestBreaker_L4CyclesBackToL1(t *testing.T) {
	backoff := []time.Duration{
		1 * time.Millisecond,
		1 * time.Millisecond,
		1 * time.Millisecond,
		1 * time.Millisecond,
	}
	b := NewBreaker()

	// 触发 L1
	_ = b.RecordFailure("cfg1", backoff)
	_ = b.RecordFailure("cfg1", backoff)
	time.Sleep(10 * time.Millisecond)

	// L1 → L2
	_ = b.RecordFailure("cfg1", backoff)
	time.Sleep(10 * time.Millisecond)

	// L2 → L3
	_ = b.RecordFailure("cfg1", backoff)
	time.Sleep(10 * time.Millisecond)

	// L3 → L4
	_ = b.RecordFailure("cfg1", backoff)
	time.Sleep(10 * time.Millisecond)

	// L4 → 循环回 L1
	msg := b.RecordFailure("cfg1", backoff)
	if !strings.Contains(msg, "cycled back to backoff L1") {
		t.Fatalf("expected 'cycled back to backoff L1', got: %s", msg)
	}

	// 确认在 L1 退避期内
	avail, reason := b.IsAvailable("cfg1", backoff)
	if avail {
		t.Fatal("expected unavailable after cycle back to L1")
	}
	if !strings.Contains(reason, "in backoff L1") {
		t.Fatalf("expected L1 reason after cycle, got: %s", reason)
	}
}

func TestBreaker_SuccessInNormalState_NoMessage(t *testing.T) {
	b := NewBreaker()
	msg := b.RecordSuccess("cfg1")
	if msg != "" {
		t.Fatalf("expected no message for normal state success, got: %s", msg)
	}
}

func TestBreaker_Concurrent(t *testing.T) {
	backoff := []time.Duration{30 * time.Second}
	b := NewBreaker()
	var wg sync.WaitGroup

	// 10 个 goroutine 同时操作同一 upstream
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.IsAvailable("cfg1", backoff)
			b.RecordFailure("cfg1", backoff)
			b.RecordSuccess("cfg1")
		}()
	}
	wg.Wait()
	// 不 panic 即通过
}

func TestBreaker_MultipleUpstreams_Independent(t *testing.T) {
	backoff1 := []time.Duration{10 * time.Millisecond}
	backoff2 := []time.Duration{20 * time.Millisecond}
	b := NewBreaker()

	// cfg1 进入退避
	_ = b.RecordFailure("cfg1", backoff1)
	_ = b.RecordFailure("cfg1", backoff1)

	// cfg2 不受影响
	avail, _ := b.IsAvailable("cfg2", backoff2)
	if !avail {
		t.Fatal("cfg2 should be independent from cfg1 backoff")
	}

	// cfg2 正常失败 1 次，不触发退避
	_ = b.RecordFailure("cfg2", backoff2)
	avail, _ = b.IsAvailable("cfg2", backoff2)
	if !avail {
		t.Fatal("cfg2 should still be available after 1 failure")
	}
}

func TestBreaker_BackoffChangeViaConfig_HotReload(t *testing.T) {
	// 模拟热重载：backoff 配置变更，下次调用使用新值
	backoffV1 := []time.Duration{30 * time.Millisecond}
	backoffV2 := []time.Duration{5 * time.Millisecond}
	b := NewBreaker()

	// 用 v1 触发退避
	_ = b.RecordFailure("cfg1", backoffV1)
	_ = b.RecordFailure("cfg1", backoffV1)

	// 退避期内用 v1 查询
	avail, _ := b.IsAvailable("cfg1", backoffV1)
	if avail {
		t.Fatal("expected unavailable with v1 backoff")
	}

	// 用 v2 查询——backoff 不同但退避状态基于 v1 的到期时间
	// 如果 v2 的 T1 更短，此时可能已经到期
	time.Sleep(10 * time.Millisecond)
	avail, _ = b.IsAvailable("cfg1", backoffV2)
	if avail {
		t.Fatal("expected still unavailable — nextAvailableAt based on original backoff")
	}

	// v1 到期后应可用
	time.Sleep(30 * time.Millisecond)
	avail, _ = b.IsAvailable("cfg1", backoffV2)
	if !avail {
		t.Fatal("expected available after original backoff expired")
	}
}