package usage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestTracker_RecordAccumulates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	tr := NewTracker(path)
	defer tr.Close()

	tr.Record("p1", "modelA", "2026-06-23", TokenUsage{Input: 100, Output: 50, CacheCreation: 10, CacheRead: 5})
	tr.Record("p1", "modelA", "2026-06-23", TokenUsage{Input: 200, Output: 30, CacheCreation: 0, CacheRead: 0})
	tr.Record("p1", "modelB", "2026-06-23", TokenUsage{Input: 7, Output: 0, CacheCreation: 0, CacheRead: 0})
	tr.Record("p2", "modelA", "2026-06-22", TokenUsage{Input: 1, Output: 1, CacheCreation: 0, CacheRead: 0})

	if err := tr.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	f, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := f.Buckets["p1"]["modelA"]["2026-06-23"]
	if got.Input != 300 || got.Output != 80 || got.CacheCreation != 10 || got.CacheRead != 5 {
		t.Fatalf("unexpected accumulated bucket: %+v", got)
	}
	if f.Buckets["p1"]["modelB"]["2026-06-23"].Input != 7 {
		t.Fatal("modelB bucket mismatch")
	}
	if f.Buckets["p2"]["modelA"]["2026-06-22"].Input != 1 {
		t.Fatal("p2 bucket mismatch")
	}
}

func TestTracker_ConcurrentRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	tr := NewTracker(path)
	defer tr.Close()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			tr.Record("p", "m", "2026-06-23", TokenUsage{Input: 1, Output: 1, CacheCreation: 1, CacheRead: 1})
		}()
	}
	wg.Wait()
	tr.Flush()

	f, _ := LoadFile(path)
	got := f.Buckets["p"]["m"]["2026-06-23"]
	if got.Input != int64(n) || got.Output != int64(n) || got.CacheCreation != int64(n) || got.CacheRead != int64(n) {
		t.Fatalf("concurrent accumulate mismatch: %+v", got)
	}
}

func TestTracker_ConcurrentRecordAndFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	tr := NewTracker(path)
	defer tr.Close()

	const (
		nRecorders = 20
		nFlushers  = 4
		nPerWorker = 50
	)

	var wg sync.WaitGroup

	// Recorder goroutines: 并发 Record
	for i := 0; i < nRecorders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < nPerWorker; j++ {
				tr.Record("p", "m", "2026-06-23", TokenUsage{Input: 1, Output: 2, CacheCreation: 3, CacheRead: 4})
			}
		}()
	}

	// Flusher goroutines: 与 Record 同时调用 Flush
	for i := 0; i < nFlushers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < nPerWorker/5; j++ {
				// Flush 错误可忽略 — 有些可能因竞争条件报错，dirty 会被恢复重试
				tr.Flush()
			}
		}()
	}

	wg.Wait()
	if err := tr.Flush(); err != nil {
		t.Fatalf("final flush: %v", err)
	}

	f, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := f.Buckets["p"]["m"]["2026-06-23"]
	expected := int64(nRecorders * nPerWorker)
	if got.Input != expected || got.Output != expected*2 || got.CacheCreation != expected*3 || got.CacheRead != expected*4 {
		t.Fatalf("concurrent Record+Flush mismatch: got %+v, expected Input=%d Output=%d CacheCreation=%d CacheRead=%d",
			got, expected, expected*2, expected*3, expected*4)
	}
}

func TestTracker_LoadPreservesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	tr1 := NewTracker(path)
	tr1.Record("p1", "m1", "2026-06-23", TokenUsage{Input: 42, Output: 0, CacheCreation: 0, CacheRead: 0})
	tr1.Close()

	tr2 := NewTracker(path)
	defer tr2.Close()
	tr2.Record("p1", "m1", "2026-06-23", TokenUsage{Input: 1, Output: 0, CacheCreation: 0, CacheRead: 0})
	tr2.Flush()

	f, _ := LoadFile(path)
	got := f.Buckets["p1"]["m1"]["2026-06-23"]
	if got.Input != 43 {
		t.Fatalf("expected preserved+added=43, got %d", got.Input)
	}
}

func TestTracker_LoadCorruptedFile_StartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	os.WriteFile(path, []byte("{not valid json"), 0600)

	tr := NewTracker(path) // 不应 panic
	defer tr.Close()
	tr.Record("p", "m", "2026-06-23", TokenUsage{Input: 1, Output: 0, CacheCreation: 0, CacheRead: 0})
	tr.Flush()

	f, _ := LoadFile(path)
	if f.Buckets["p"]["m"]["2026-06-23"].Input != 1 {
		t.Fatal("expected fresh start after corrupted file")
	}
}

func TestTracker_LoadIncompatibleVersion_BackupsAndStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	os.WriteFile(path, []byte(`{"version":999,"buckets":{}}`), 0600)

	tr := NewTracker(path)
	defer tr.Close()
	tr.Record("p", "m", "2026-06-23", TokenUsage{Input: 1, Output: 0, CacheCreation: 0, CacheRead: 0})
	tr.Flush()

	f, _ := LoadFile(path)
	if f.Version != 1 {
		t.Fatalf("expected rewritten file version 1, got %d", f.Version)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "usage.json.bak.*"))
	if len(matches) == 0 {
		t.Fatal("expected backup file for incompatible version")
	}
}

func TestTracker_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	tr := NewTracker(path)
	tr.Record("proj", "mod", "2026-06-23", TokenUsage{Input: 12500, Output: 3200, CacheCreation: 800, CacheRead: 600})
	tr.Close()

	f, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if f.Version != 1 {
		t.Fatalf("expected version 1, got %d", f.Version)
	}
	u := f.Buckets["proj"]["mod"]["2026-06-23"]
	if u.Input != 12500 || u.Output != 3200 || u.CacheCreation != 800 || u.CacheRead != 600 {
		t.Fatalf("round-trip mismatch: %+v", u)
	}
}

// --- Collector ---

type fakeRecorder struct {
	onRecord func(project, model, date string, u TokenUsage)
}

func (f *fakeRecorder) Record(project, model, date string, u TokenUsage) {
	if f.onRecord != nil {
		f.onRecord(project, model, date, u)
	}
}

func TestCollector_StreamCommit(t *testing.T) {
	var got TokenUsage
	var calls int
	rec := fakeRecorder{onRecord: func(p, m, d string, u TokenUsage) { got = u; calls++ }}
	c := NewCollector(&rec, "p1", "modelA")
	c.Attach("text/event-stream")
	c.Feed([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n"))
	c.Feed([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":4}}\n\n"))
	c.Close()
	if calls != 1 {
		t.Fatalf("expected 1 Record call, got %d", calls)
	}
	if got.Input != 10 || got.Output != 4 {
		t.Fatalf("unexpected committed usage: %+v", got)
	}
}

func TestCollector_NoAttach_NoCommit(t *testing.T) {
	calls := 0
	rec := fakeRecorder{onRecord: func(p, m, d string, u TokenUsage) { calls++ }}
	c := NewCollector(&rec, "p1", "modelA")
	c.Close() // 未 Attach（连接失败场景）
	if calls != 0 {
		t.Fatal("must not commit when never attached")
	}
}
