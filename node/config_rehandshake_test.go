package node

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultFastRehandshakeConfigValid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NetworkPassword = "test"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid, got: %v", err)
	}
}

func TestFastRehandshakeConfigValidation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NetworkPassword = "test"
	cfg.FastRehandshakeBaseBackoff = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid fast re-handshake config")
	}
	if !strings.Contains(err.Error(), "FastRehandshakeBaseBackoff") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestWithFastRehandshakeOptions(t *testing.T) {
	cfg := DefaultConfig()

	WithFastRehandshakeBackoff(700*time.Millisecond, 4*time.Second)(cfg)
	WithFastRehandshakeWindow(20*time.Second, 9)(cfg)
	WithFastRehandshakeFailThreshold(5)(cfg)
	WithFastRehandshakePendingTTL(45 * time.Second)(cfg)

	if cfg.FastRehandshakeBaseBackoff != 700*time.Millisecond {
		t.Fatalf("unexpected FastRehandshakeBaseBackoff: %v", cfg.FastRehandshakeBaseBackoff)
	}
	if cfg.FastRehandshakeMaxBackoff != 4*time.Second {
		t.Fatalf("unexpected FastRehandshakeMaxBackoff: %v", cfg.FastRehandshakeMaxBackoff)
	}
	if cfg.FastRehandshakeWindow != 20*time.Second {
		t.Fatalf("unexpected FastRehandshakeWindow: %v", cfg.FastRehandshakeWindow)
	}
	if cfg.FastRehandshakeMaxAttemptsWindow != 9 {
		t.Fatalf("unexpected FastRehandshakeMaxAttemptsWindow: %d", cfg.FastRehandshakeMaxAttemptsWindow)
	}
	if cfg.FastRehandshakeFailThreshold != 5 {
		t.Fatalf("unexpected FastRehandshakeFailThreshold: %d", cfg.FastRehandshakeFailThreshold)
	}
	if cfg.FastRehandshakePendingTTL != 45*time.Second {
		t.Fatalf("unexpected FastRehandshakePendingTTL: %v", cfg.FastRehandshakePendingTTL)
	}
}
