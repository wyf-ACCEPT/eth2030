package log

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// LogLevel represents the severity of a log entry.
type LogLevel int

const (
	// DEBUG is the most verbose level, used for development diagnostics.
	DEBUG LogLevel = iota
	// INFO is for general operational messages.
	INFO
	// WARN indicates a potentially harmful situation.
	WARN
	// ERROR indicates a failure that does not stop the application.
	ERROR
	// FATAL indicates a critical failure that typically terminates the process.
	FATAL
)

// String returns the uppercase name of the level.
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return fmt.Sprintf("LEVEL(%d)", int(l))
	}
}

// LevelFromString parses a log level from its string representation.
// The match is case-insensitive. Unrecognised strings return INFO.
func LevelFromString(s string) LogLevel {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN", "WARNING":
		return WARN
	case "ERROR":
		return ERROR
	case "FATAL":
		return FATAL
	default:
		return INFO
	}
}

// LogEntry holds all data for a single log event.
type LogEntry struct {
	Timestamp time.Time
	Level     LogLevel
	Message   string
	Fields    map[string]interface{}
}

// LogFormatter formats a LogEntry into a printable string.
type LogFormatter interface {
	Format(entry LogEntry) string
}

// ---------------------------------------------------------------------------
// TextFormatter
// ---------------------------------------------------------------------------

// TextFormatter renders log entries as plain text in the format:
//
//	[2024-01-01 12:00:00] INFO  message key=value
type TextFormatter struct {
	// TimeFormat controls the timestamp layout. Defaults to
	// "2006-01-02 15:04:05" when empty.
	TimeFormat string
}

// Format produces a plain-text line for the given entry.
func (f *TextFormatter) Format(entry LogEntry) string {
	tf := f.TimeFormat
	if tf == "" {
		tf = "2006-01-02 15:04:05"
	}

	var b strings.Builder
	b.WriteString("[")
	b.WriteString(entry.Timestamp.Format(tf))
	b.WriteString("] ")
	// Pad level name to 5 chars for alignment (DEBUG/INFO /WARN /ERROR/FATAL).
	b.WriteString(fmt.Sprintf("%-5s", entry.Level.String()))
	b.WriteString(" ")
	b.WriteString(entry.Message)

	// Append fields sorted by key for deterministic output.
	if len(entry.Fields) > 0 {
		keys := sortedKeys(entry.Fields)
		for _, k := range keys {
			b.WriteString(" ")
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(fmt.Sprintf("%v", entry.Fields[k]))
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// JSONFormatter
// ---------------------------------------------------------------------------

// JSONFormatter renders log entries as a single JSON object per line.
type JSONFormatter struct {
	// TimeFormat controls the timestamp layout. Defaults to time.RFC3339 when
	// empty.
	TimeFormat string
}

// Format produces a JSON string for the given entry.
func (f *JSONFormatter) Format(entry LogEntry) string {
	tf := f.TimeFormat
	if tf == "" {
		tf = time.RFC3339
	}

	obj := make(map[string]interface{}, 3+len(entry.Fields))
	obj["time"] = entry.Timestamp.Format(tf)
	obj["level"] = entry.Level.String()
	obj["msg"] = entry.Message

	for k, v := range entry.Fields {
		obj[k] = v
	}

	data, err := json.Marshal(obj)
	if err != nil {
		// Fallback: return a best-effort string so logging never panics.
		return fmt.Sprintf(`{"time":%q,"level":%q,"msg":%q,"error":"marshal failed"}`,
			entry.Timestamp.Format(tf), entry.Level.String(), entry.Message)
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// ColorFormatter
// ---------------------------------------------------------------------------

// ANSI color escape codes used by ColorFormatter.
const (
	ansiReset  = "\033[0m"
	ansiGray   = "\033[37m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiBold   = "\033[1m"
)

// ColorFormatter renders log entries as ANSI-colored text. Each log level
// gets a distinct color:
//
//	DEBUG -> gray
//	INFO  -> green
//	WARN  -> yellow
//	ERROR -> red
//	FATAL -> bold red
type ColorFormatter struct {
	// TimeFormat controls the timestamp layout. Defaults to
	// "2006-01-02 15:04:05" when empty.
	TimeFormat string
}

// colorForLevel returns the ANSI escape sequence for the given level.
func colorForLevel(level LogLevel) string {
	switch level {
	case DEBUG:
		return ansiGray
	case INFO:
		return ansiGreen
	case WARN:
		return ansiYellow
	case ERROR:
		return ansiRed
	case FATAL:
		return ansiBold + ansiRed
	default:
		return ansiReset
	}
}

// Format produces a colored text line for the given entry.
func (f *ColorFormatter) Format(entry LogEntry) string {
	tf := f.TimeFormat
	if tf == "" {
		tf = "2006-01-02 15:04:05"
	}

	color := colorForLevel(entry.Level)

	var b strings.Builder
	b.WriteString("[")
	b.WriteString(entry.Timestamp.Format(tf))
	b.WriteString("] ")
	b.WriteString(color)
	b.WriteString(fmt.Sprintf("%-5s", entry.Level.String()))
	b.WriteString(ansiReset)
	b.WriteString(" ")
	b.WriteString(entry.Message)

	if len(entry.Fields) > 0 {
		keys := sortedKeys(entry.Fields)
		for _, k := range keys {
			b.WriteString(" ")
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(fmt.Sprintf("%v", entry.Fields[k]))
		}
	}
	return b.String()
}

// sortedKeys returns the map keys in sorted order.
func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
