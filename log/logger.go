// Package log provides a simple logging interface for the tenet library.
// By default, the library uses a silent logger (NopLogger) to avoid polluting
// the host application's output. Users can inject their own logger implementation.
package log

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents the severity level of a log message.
type Level int

const (
	// LevelDebug is for detailed debugging information.
	LevelDebug Level = iota
	// LevelInfo is for general operational information.
	LevelInfo
	// LevelWarn is for warning conditions.
	LevelWarn
	// LevelError is for error conditions.
	LevelError
	// LevelSilent disables all logging.
	LevelSilent
)

// String returns the string representation of the log level.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger defines the logging interface used by tenet.
// Implementations should be safe for concurrent use.
type Logger interface {
	// Debug logs a debug-level message.
	Debug(format string, args ...interface{})
	// Info logs an info-level message.
	Info(format string, args ...interface{})
	// Warn logs a warning-level message.
	Warn(format string, args ...interface{})
	// Error logs an error-level message.
	Error(format string, args ...interface{})
}

// NopLogger is a no-operation logger that discards all log messages.
// This is the default logger used by tenet to ensure silent operation
// when embedded in other applications.
type NopLogger struct{}

// Debug implements Logger.Debug (no-op).
func (NopLogger) Debug(format string, args ...interface{}) {}

// Info implements Logger.Info (no-op).
func (NopLogger) Info(format string, args ...interface{}) {}

// Warn implements Logger.Warn (no-op).
func (NopLogger) Warn(format string, args ...interface{}) {}

// Error implements Logger.Error (no-op).
func (NopLogger) Error(format string, args ...interface{}) {}

// Nop returns a singleton NopLogger instance.
func Nop() Logger {
	return NopLogger{}
}

// StdLogger is a simple logger that writes to an io.Writer with level filtering.
type StdLogger struct {
	mu     sync.Mutex
	writer io.Writer
	level  Level
	prefix string
}

// StdLoggerOption is a configuration option for StdLogger.
type StdLoggerOption func(*StdLogger)

// WithWriter sets the output writer for the logger.
func WithWriter(w io.Writer) StdLoggerOption {
	return func(l *StdLogger) {
		l.writer = w
	}
}

// WithLevel sets the minimum log level.
func WithLevel(level Level) StdLoggerOption {
	return func(l *StdLogger) {
		l.level = level
	}
}

// WithPrefix sets a prefix for all log messages.
func WithPrefix(prefix string) StdLoggerOption {
	return func(l *StdLogger) {
		l.prefix = prefix
	}
}

// NewStdLogger creates a new StdLogger with the given options.
// By default, it writes to os.Stderr at Info level.
func NewStdLogger(opts ...StdLoggerOption) *StdLogger {
	l := &StdLogger{
		writer: os.Stderr,
		level:  LevelInfo,
		prefix: "[tenet]",
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// log writes a formatted log message if the level is >= the configured level.
func (l *StdLogger) log(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)

	if l.prefix != "" {
		fmt.Fprintf(l.writer, "%s %s %s %s\n", timestamp, l.prefix, level.String(), msg)
	} else {
		fmt.Fprintf(l.writer, "%s %s %s\n", timestamp, level.String(), msg)
	}
}

// Debug implements Logger.Debug.
func (l *StdLogger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

// Info implements Logger.Info.
func (l *StdLogger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

// Warn implements Logger.Warn.
func (l *StdLogger) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

// Error implements Logger.Error.
func (l *StdLogger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// Default returns a default StdLogger that writes to stderr at Info level.
func Default() Logger {
	return NewStdLogger()
}
