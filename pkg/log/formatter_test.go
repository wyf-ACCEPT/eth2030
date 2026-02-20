package log

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// fixed timestamp used across tests for deterministic output.
var testTime = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

func makeEntry(level LogLevel, msg string, fields map[string]interface{}) LogEntry {
	return LogEntry{
		Timestamp: testTime,
		Level:     level,
		Message:   msg,
		Fields:    fields,
	}
}

// ---------------------------------------------------------------------------
// LogLevel tests
// ---------------------------------------------------------------------------

func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level LogLevel
		want  string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{WARN, "WARN"},
		{ERROR, "ERROR"},
		{FATAL, "FATAL"},
		{LogLevel(99), "LEVEL(99)"},
	}
	for _, tt := range tests {
		got := tt.level.String()
		if got != tt.want {
			t.Errorf("LogLevel(%d).String() = %q, want %q", int(tt.level), got, tt.want)
		}
	}
}

func TestLevelFromString(t *testing.T) {
	tests := []struct {
		input string
		want  LogLevel
	}{
		{"DEBUG", DEBUG},
		{"debug", DEBUG},
		{"INFO", INFO},
		{"info", INFO},
		{"WARN", WARN},
		{"warn", WARN},
		{"WARNING", WARN},
		{"ERROR", ERROR},
		{"error", ERROR},
		{"FATAL", FATAL},
		{"fatal", FATAL},
		{"  INFO  ", INFO},
		{"unknown", INFO}, // default
		{"", INFO},        // default
	}
	for _, tt := range tests {
		got := LevelFromString(tt.input)
		if got != tt.want {
			t.Errorf("LevelFromString(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// TextFormatter tests
// ---------------------------------------------------------------------------

func TestTextFormatter_Basic(t *testing.T) {
	f := &TextFormatter{}
	entry := makeEntry(INFO, "server started", nil)
	out := f.Format(entry)

	if !strings.Contains(out, "[2024-01-01 12:00:00]") {
		t.Errorf("missing timestamp in output: %s", out)
	}
	if !strings.Contains(out, "INFO") {
		t.Errorf("missing level in output: %s", out)
	}
	if !strings.Contains(out, "server started") {
		t.Errorf("missing message in output: %s", out)
	}
}

func TestTextFormatter_WithFields(t *testing.T) {
	f := &TextFormatter{}
	fields := map[string]interface{}{
		"port": 8545,
		"host": "localhost",
	}
	entry := makeEntry(INFO, "listening", fields)
	out := f.Format(entry)

	// Fields are sorted alphabetically.
	if !strings.Contains(out, "host=localhost") {
		t.Errorf("missing host field: %s", out)
	}
	if !strings.Contains(out, "port=8545") {
		t.Errorf("missing port field: %s", out)
	}
	// host should come before port (alphabetical).
	hostIdx := strings.Index(out, "host=")
	portIdx := strings.Index(out, "port=")
	if hostIdx > portIdx {
		t.Errorf("fields not sorted: host at %d, port at %d", hostIdx, portIdx)
	}
}

func TestTextFormatter_CustomTimeFormat(t *testing.T) {
	f := &TextFormatter{TimeFormat: time.RFC822}
	entry := makeEntry(WARN, "slow", nil)
	out := f.Format(entry)

	expected := testTime.Format(time.RFC822)
	if !strings.Contains(out, expected) {
		t.Errorf("expected time format %q in output: %s", expected, out)
	}
}

func TestTextFormatter_LevelPadding(t *testing.T) {
	f := &TextFormatter{}
	// INFO is 4 chars, padded to 5 -> "INFO " with trailing space.
	entry := makeEntry(INFO, "msg", nil)
	out := f.Format(entry)
	if !strings.Contains(out, "INFO ") {
		t.Errorf("expected padded 'INFO ' in output: %s", out)
	}

	// ERROR is 5 chars, no extra padding needed.
	entry2 := makeEntry(ERROR, "msg", nil)
	out2 := f.Format(entry2)
	if !strings.Contains(out2, "ERROR") {
		t.Errorf("expected 'ERROR' in output: %s", out2)
	}
}

// ---------------------------------------------------------------------------
// JSONFormatter tests
// ---------------------------------------------------------------------------

func TestJSONFormatter_Basic(t *testing.T) {
	f := &JSONFormatter{}
	entry := makeEntry(ERROR, "disk full", nil)
	out := f.Format(entry)

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v (raw: %s)", err, out)
	}
	if parsed["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", parsed["level"])
	}
	if parsed["msg"] != "disk full" {
		t.Errorf("msg = %v, want 'disk full'", parsed["msg"])
	}
	if _, ok := parsed["time"]; !ok {
		t.Error("missing 'time' field in JSON output")
	}
}

func TestJSONFormatter_WithFields(t *testing.T) {
	f := &JSONFormatter{}
	fields := map[string]interface{}{
		"block": 12345,
		"hash":  "0xabc",
	}
	entry := makeEntry(INFO, "processed", fields)
	out := f.Format(entry)

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v (raw: %s)", err, out)
	}
	// JSON numbers are float64.
	if v, ok := parsed["block"].(float64); !ok || v != 12345 {
		t.Errorf("block = %v, want 12345", parsed["block"])
	}
	if parsed["hash"] != "0xabc" {
		t.Errorf("hash = %v, want '0xabc'", parsed["hash"])
	}
}

func TestJSONFormatter_CustomTimeFormat(t *testing.T) {
	f := &JSONFormatter{TimeFormat: "2006-01-02"}
	entry := makeEntry(DEBUG, "test", nil)
	out := f.Format(entry)

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["time"] != "2024-01-01" {
		t.Errorf("time = %v, want '2024-01-01'", parsed["time"])
	}
}

// ---------------------------------------------------------------------------
// ColorFormatter tests
// ---------------------------------------------------------------------------

func TestColorFormatter_ContainsANSI(t *testing.T) {
	f := &ColorFormatter{}
	levels := []LogLevel{DEBUG, INFO, WARN, ERROR, FATAL}

	for _, lvl := range levels {
		entry := makeEntry(lvl, "test", nil)
		out := f.Format(entry)

		// Every colored output must contain the reset sequence.
		if !strings.Contains(out, ansiReset) {
			t.Errorf("level %v: missing ANSI reset in output: %s", lvl, out)
		}
		// Must contain the level name.
		if !strings.Contains(out, lvl.String()) {
			t.Errorf("level %v: missing level name in output: %s", lvl, out)
		}
	}
}

func TestColorFormatter_DifferentColors(t *testing.T) {
	// Verify that different levels produce different color codes.
	colors := make(map[string]LogLevel)
	for _, lvl := range []LogLevel{DEBUG, INFO, WARN, ERROR} {
		c := colorForLevel(lvl)
		if prev, exists := colors[c]; exists {
			t.Errorf("levels %v and %v share the same color code %q", prev, lvl, c)
		}
		colors[c] = lvl
	}
}

func TestColorFormatter_WithFields(t *testing.T) {
	f := &ColorFormatter{}
	fields := map[string]interface{}{"key": "value"}
	entry := makeEntry(INFO, "msg", fields)
	out := f.Format(entry)

	if !strings.Contains(out, "key=value") {
		t.Errorf("missing field in colored output: %s", out)
	}
}

// ---------------------------------------------------------------------------
// LogEntry tests
// ---------------------------------------------------------------------------

func TestLogEntry_NilFields(t *testing.T) {
	// Formatters must handle nil Fields gracefully.
	entry := LogEntry{
		Timestamp: testTime,
		Level:     INFO,
		Message:   "no fields",
		Fields:    nil,
	}

	text := (&TextFormatter{}).Format(entry)
	if !strings.Contains(text, "no fields") {
		t.Errorf("TextFormatter failed with nil fields: %s", text)
	}

	js := (&JSONFormatter{}).Format(entry)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(js), &parsed); err != nil {
		t.Errorf("JSONFormatter produced invalid JSON with nil fields: %v", err)
	}

	color := (&ColorFormatter{}).Format(entry)
	if !strings.Contains(color, "no fields") {
		t.Errorf("ColorFormatter failed with nil fields: %s", color)
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestFormatterInterfaceCompliance(t *testing.T) {
	// Compile-time check that all formatters satisfy LogFormatter.
	var _ LogFormatter = (*TextFormatter)(nil)
	var _ LogFormatter = (*JSONFormatter)(nil)
	var _ LogFormatter = (*ColorFormatter)(nil)
}
