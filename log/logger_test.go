package log

import (
	"bytes"
	"strings"
	"testing"
)

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{LevelSilent, "UNKNOWN"},
		{Level(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.expected {
			t.Errorf("Level(%d).String() = %s, 期望 %s", tt.level, got, tt.expected)
		}
	}
}

func TestNopLogger(t *testing.T) {
	logger := Nop()

	// NopLogger 应该是单例
	logger2 := Nop()
	if logger != logger2 {
		t.Error("Nop() 应返回相同实例")
	}

	// 不应该 panic
	logger.Debug("test %s", "debug")
	logger.Info("test %s", "info")
	logger.Warn("test %s", "warn")
	logger.Error("test %s", "error")
}

func TestStdLoggerOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewStdLogger(WithWriter(buf), WithLevel(LevelDebug))

	logger.Info("测试消息 %d", 123)
	output := buf.String()

	if !strings.Contains(output, "INFO") {
		t.Errorf("日志应包含 INFO: %s", output)
	}
	if !strings.Contains(output, "测试消息 123") {
		t.Errorf("日志应包含消息内容: %s", output)
	}
}

func TestStdLoggerLevelFiltering(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewStdLogger(WithWriter(buf), WithLevel(LevelWarn))

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()

	if strings.Contains(output, "debug") {
		t.Error("Debug 级别不应该输出")
	}
	if strings.Contains(output, "info message") {
		t.Error("Info 级别不应该输出")
	}
	if !strings.Contains(output, "warn") {
		t.Error("Warn 级别应该输出")
	}
	if !strings.Contains(output, "error") {
		t.Error("Error 级别应该输出")
	}
}

func TestStdLoggerDebugLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewStdLogger(WithWriter(buf), WithLevel(LevelDebug))

	logger.Debug("debug message")
	output := buf.String()

	if !strings.Contains(output, "DEBUG") {
		t.Error("Debug 级别应该输出")
	}
}

func TestStdLoggerErrorLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewStdLogger(WithWriter(buf), WithLevel(LevelError))

	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error message")

	output := buf.String()

	if strings.Contains(output, "debug") || strings.Contains(output, "info") || strings.Contains(output, "warn") {
		t.Error("只有 Error 级别应该输出")
	}
	if !strings.Contains(output, "error message") {
		t.Error("Error 消息应该输出")
	}
}

func TestStdLoggerSilentLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewStdLogger(WithWriter(buf), WithLevel(LevelSilent))

	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")

	if buf.Len() > 0 {
		t.Errorf("Silent 级别不应有任何输出，实际: %s", buf.String())
	}
}

func TestDefaultLogger(t *testing.T) {
	logger := Default()
	if logger == nil {
		t.Error("Default() 返回 nil")
	}
}

func TestNewStdLoggerDefaults(t *testing.T) {
	logger := NewStdLogger()
	if logger == nil {
		t.Fatal("NewStdLogger() 返回 nil")
	}

	// 默认不应该 panic
	logger.Info("test message")
}

func TestLoggerInterface(t *testing.T) {
	// 确保 NopLogger 和 StdLogger 都实现了 Logger 接口
	var _ Logger = NopLogger{}
	var _ Logger = &StdLogger{}
}
