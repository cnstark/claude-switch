package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSince(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
		check   func(string) bool
	}{
		{"", false, func(s string) bool { return len(s) == 10 }},                // 默认 7d → 日期
		{"7d", false, func(s string) bool { return len(s) == 10 }},              // → 日期
		{"2026-06-01", false, func(s string) bool { return s == "2026-06-01" }}, // 原样
		{"bad", true, nil},
		{"-1d", true, nil},
	}
	for _, c := range cases {
		got, err := parseSince(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseSince(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSince(%q) unexpected error: %v", c.in, err)
			continue
		}
		if !c.check(got) {
			t.Errorf("parseSince(%q) = %q, check failed", c.in, got)
		}
	}
}

func TestQuery_FiltersByProjectModelSince(t *testing.T) {
	f := &File{Version: 1, Buckets: map[string]map[string]map[string]*TokenUsage{
		"p1": {
			"modelA": {
				"2026-06-23": {Input: 100, Output: 10, CacheCreation: 0, CacheRead: 0},
				"2026-06-20": {Input: 50, Output: 5, CacheCreation: 0, CacheRead: 0},
			},
			"modelB": {
				"2026-06-23": {Input: 7, Output: 1, CacheCreation: 0, CacheRead: 0},
			},
		},
		"p2": {
			"modelA": {
				"2026-06-23": {Input: 999, Output: 0, CacheCreation: 0, CacheRead: 0},
			},
		},
	}}

	if rows := Query(f, "", "", "1970-01-01"); len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(rows))
	}
	if rows := Query(f, "p1", "", "1970-01-01"); len(rows) != 3 {
		t.Fatalf("expected 3 rows for p1, got %d", len(rows))
	}
	if rows := Query(f, "p1", "modelA", "1970-01-01"); len(rows) != 2 {
		t.Fatalf("expected 2 rows for p1/modelA, got %d", len(rows))
	}
	rows := Query(f, "p1", "modelA", "2026-06-23")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after since, got %d", len(rows))
	}
	if rows[0].Input != 100 {
		t.Fatalf("expected input 100, got %d", rows[0].Input)
	}
	if rows[0].Total != 110 {
		t.Fatalf("expected total 110, got %d", rows[0].Total)
	}
}

func TestQuery_Sorted(t *testing.T) {
	f := &File{Version: 1, Buckets: map[string]map[string]map[string]*TokenUsage{
		"p2": {"m": {"2026-06-01": {Input: 1, Output: 0, CacheCreation: 0, CacheRead: 0}}},
		"p1": {"m": {
			"2026-06-03": {Input: 1, Output: 0, CacheCreation: 0, CacheRead: 0},
			"2026-06-02": {Input: 1, Output: 0, CacheCreation: 0, CacheRead: 0},
		}},
	}}
	rows := Query(f, "", "", "1970-01-01")
	if rows[0].Project != "p1" || rows[0].Date != "2026-06-02" {
		t.Fatalf("expected sorted p1/2026-06-02 first, got %+v", rows[0])
	}
	if rows[len(rows)-1].Project != "p2" {
		t.Fatalf("expected p2 last, got %+v", rows[len(rows)-1])
	}
}

func TestRunStats_MissingFile(t *testing.T) {
	out, err := RunStats(filepath.Join(t.TempDir(), "nope.json"), "", "7d", "")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if !strings.Contains(out, "暂无") {
		t.Fatalf("expected empty-data message, got: %s", out)
	}
}

func TestRunStats_RendersTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	tr := NewTracker(path)
	tr.Record("p1", "modelA", "2026-06-23", TokenUsage{Input: 100, Output: 10, CacheCreation: 5, CacheRead: 2})
	tr.Close()

	out, err := RunStats(path, "", "1970-01-01", "")
	if err != nil {
		t.Fatalf("runstats: %v", err)
	}
	if !strings.Contains(out, "p1") || !strings.Contains(out, "modelA") || !strings.Contains(out, "100") {
		t.Fatalf("expected table with data, got: %s", out)
	}
}

func TestRunStats_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	os.WriteFile(path, []byte("not json"), 0600)
	_, err := RunStats(path, "", "7d", "")
	if err == nil {
		t.Fatal("expected error for corrupt file")
	}
}
