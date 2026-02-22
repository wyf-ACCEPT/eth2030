// Package log provides structured logging for the eth2030 Ethereum execution
// client. It wraps Go's log/slog with Ethereum-specific conveniences such as
// per-module child loggers.
package log

import (
	"log/slog"
	"os"
)

// Logger wraps slog.Logger with Ethereum-specific context.
type Logger struct {
	inner *slog.Logger
}

// defaultLogger is the process-wide logger used by the package-level
// convenience functions.
var defaultLogger *Logger

func init() {
	defaultLogger = New(slog.LevelInfo)
}

// New creates a Logger that writes JSON to stderr at the given level.
func New(level slog.Level) *Logger {
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	return &Logger{inner: slog.New(h)}
}

// NewWithHandler creates a Logger backed by the supplied slog.Handler. This
// is useful for testing or for writing to a custom destination.
func NewWithHandler(h slog.Handler) *Logger {
	return &Logger{inner: slog.New(h)}
}

// SetDefault replaces the package-level default logger.
func SetDefault(l *Logger) {
	if l != nil {
		defaultLogger = l
	}
}

// Default returns the current package-level default logger.
func Default() *Logger {
	return defaultLogger
}

// Module returns a child logger with an additional "module" attribute. This
// is the primary way subsystems (evm, txpool, p2p, ...) obtain their own
// contextual logger.
func (l *Logger) Module(name string) *Logger {
	return &Logger{inner: l.inner.With("module", name)}
}

// With returns a child logger with additional key-value context.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{inner: l.inner.With(args...)}
}

// Debug logs at LevelDebug.
func (l *Logger) Debug(msg string, args ...any) { l.inner.Debug(msg, args...) }

// Info logs at LevelInfo.
func (l *Logger) Info(msg string, args ...any) { l.inner.Info(msg, args...) }

// Warn logs at LevelWarn.
func (l *Logger) Warn(msg string, args ...any) { l.inner.Warn(msg, args...) }

// Error logs at LevelError.
func (l *Logger) Error(msg string, args ...any) { l.inner.Error(msg, args...) }

// ---------------------------------------------------------------------------
// Package-level convenience functions -- delegate to defaultLogger.
// ---------------------------------------------------------------------------

// Debug logs at LevelDebug using the default logger.
func Debug(msg string, args ...any) { defaultLogger.Debug(msg, args...) }

// Info logs at LevelInfo using the default logger.
func Info(msg string, args ...any) { defaultLogger.Info(msg, args...) }

// Warn logs at LevelWarn using the default logger.
func Warn(msg string, args ...any) { defaultLogger.Warn(msg, args...) }

// Error logs at LevelError using the default logger.
func Error(msg string, args ...any) { defaultLogger.Error(msg, args...) }
