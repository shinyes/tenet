package node

import "testing"

type kcpTestLogger struct{}

func (k *kcpTestLogger) Debug(format string, args ...interface{}) {}
func (k *kcpTestLogger) Info(format string, args ...interface{})  {}
func (k *kcpTestLogger) Warn(format string, args ...interface{})  {}
func (k *kcpTestLogger) Error(format string, args ...interface{}) {}

func TestNewKCPManagerLoggerDefaultAndInjected(t *testing.T) {
	manager := NewKCPManager(DefaultKCPConfig())
	if manager.logger == nil {
		t.Fatal("expected default logger to be set")
	}

	custom := &kcpTestLogger{}
	manager2 := NewKCPManager(DefaultKCPConfig(), custom)
	if manager2.logger != custom {
		t.Fatal("expected injected logger to be used")
	}
}
