package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLogger_Off_NoOutput(t *testing.T) {
	var buf bytes.Buffer
	l := New(Off, &buf)
	l.Log("test message", map[string]any{"key": "val"})
	if buf.Len() != 0 {
		t.Fatal("expected no output for Off level")
	}
}

func TestLogger_Meta_Output(t *testing.T) {
	var buf bytes.Buffer
	l := New(Meta, &buf)
	l.Log("request completed", map[string]any{
		"project": "p1",
		"model":   "modelA",
		"upstream": "cfg1",
		"status":  200,
	})
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON log line: %v", err)
	}
	if entry["msg"] != "request completed" {
		t.Fatalf("expected msg field, got %v", entry["msg"])
	}
	if entry["project"] != "p1" {
		t.Fatalf("expected project p1, got %v", entry["project"])
	}
}

func TestLogger_Debug_IncludesAllFields(t *testing.T) {
	var buf bytes.Buffer
	l := New(Debug, &buf)
	l.Log("debug info", map[string]any{
		"request_body": `{"model":"m"}`,
	})
	if !strings.Contains(buf.String(), "request_body") {
		t.Fatal("expected request_body in debug output")
	}
}

func TestLogger_WithLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(Meta, &buf)
	// Debug level messages should NOT appear at Meta level
	l.Debug("debug msg", map[string]any{"x": 1})
	if buf.Len() != 0 {
		t.Fatal("expected no debug output at Meta level")
	}
	l.Info("info msg", map[string]any{"x": 1})
	if buf.Len() == 0 {
		t.Fatal("expected info output at Meta level")
	}
}
