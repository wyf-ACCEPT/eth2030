package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// newTestLogger returns a Logger that writes JSON into buf.
func newTestLogger(buf *bytes.Buffer, level slog.Level) *Logger {
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level})
	return NewWithHandler(h)
}

// ---------------------------------------------------------------------------
// Logger.Module
// ---------------------------------------------------------------------------

func TestLogger_Module(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, slog.LevelDebug)
	child := l.Module("evm")

	child.Info("hello")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, buf.String())
	}

	if entry["module"] != "evm" {
		t.Fatalf("module = %v, want %q", entry["module"], "evm")
	}
	if entry["msg"] != "hello" {
		t.Fatalf("msg = %v, want %q", entry["msg"], "hello")
	}
}

func TestLogger_ModuleChain(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, slog.LevelDebug)
	child := l.Module("txpool").With("peer", "abc")

	child.Info("added")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, buf.String())
	}

	if entry["module"] != "txpool" {
		t.Fatalf("module = %v, want %q", entry["module"], "txpool")
	}
	if entry["peer"] != "abc" {
		t.Fatalf("peer = %v, want %q", entry["peer"], "abc")
	}
}

// ---------------------------------------------------------------------------
// Logger levels
// ---------------------------------------------------------------------------

func TestLogger_Levels(t *testing.T) {
	tests := []struct {
		level  slog.Level
		logFn  func(l *Logger)
		expect bool // whether message should appear
	}{
		{slog.LevelInfo, func(l *Logger) { l.Debug("nope") }, false},
		{slog.LevelInfo, func(l *Logger) { l.Info("yes") }, true},
		{slog.LevelInfo, func(l *Logger) { l.Warn("yes") }, true},
		{slog.LevelInfo, func(l *Logger) { l.Error("yes") }, true},
		{slog.LevelWarn, func(l *Logger) { l.Info("nope") }, false},
		{slog.LevelWarn, func(l *Logger) { l.Warn("yes") }, true},
		{slog.LevelDebug, func(l *Logger) { l.Debug("yes") }, true},
	}

	for i, tt := range tests {
		var buf bytes.Buffer
		l := newTestLogger(&buf, tt.level)
		tt.logFn(l)

		got := buf.Len() > 0
		if got != tt.expect {
			t.Errorf("test %d: output=%v, want %v (level=%v, buf=%s)",
				i, got, tt.expect, tt.level, buf.String())
		}
	}
}

// ---------------------------------------------------------------------------
// Structured key-value args
// ---------------------------------------------------------------------------

func TestLogger_KeyValueArgs(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, slog.LevelInfo)

	l.Info("block processed", "number", 100, "hash", "0xabc")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// slog renders numbers as float64 in JSON.
	if v, ok := entry["number"].(float64); !ok || v != 100 {
		t.Fatalf("number = %v, want 100", entry["number"])
	}
	if entry["hash"] != "0xabc" {
		t.Fatalf("hash = %v, want %q", entry["hash"], "0xabc")
	}
}

// ---------------------------------------------------------------------------
// Default logger
// ---------------------------------------------------------------------------

func TestDefaultLogger(t *testing.T) {
	// The package init() sets a default logger; verify it is not nil and
	// does not panic.
	if Default() == nil {
		t.Fatal("Default() returned nil")
	}

	// Replace the default with a test logger and verify the package-level
	// functions use it.
	var buf bytes.Buffer
	l := newTestLogger(&buf, slog.LevelInfo)
	SetDefault(l)
	defer SetDefault(New(slog.LevelInfo)) // restore

	Info("test info", "k", "v")

	if !strings.Contains(buf.String(), "test info") {
		t.Fatalf("output missing 'test info': %s", buf.String())
	}

	// SetDefault(nil) should be a no-op.
	SetDefault(nil)
	if Default() != l {
		t.Fatal("SetDefault(nil) replaced the logger")
	}
}

// ---------------------------------------------------------------------------
// Package-level functions
// ---------------------------------------------------------------------------

func TestPackageLevelFunctions(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, slog.LevelDebug)
	SetDefault(l)
	defer SetDefault(New(slog.LevelInfo))

	Debug("d")
	Info("i")
	Warn("w")
	Error("e")

	out := buf.String()
	for _, msg := range []string{"d", "i", "w", "e"} {
		if !strings.Contains(out, msg) {
			t.Errorf("missing message %q in output", msg)
		}
	}
}
