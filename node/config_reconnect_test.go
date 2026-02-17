package node

import (
	"strings"
	"testing"
	"time"
)

func TestReconnectConfigValidation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NetworkPassword = "test"
	cfg.ReconnectConfig = &ReconnectConfig{
		Enabled:           true,
		MaxRetries:        -1,
		InitialDelay:      0,
		MaxDelay:          0,
		BackoffMultiplier: 0.5,
		JitterFactor:      1.5,
		ReconnectTimeout:  0,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected reconnect config validation error")
	}

	want := []string{
		"ReconnectConfig.MaxRetries",
		"ReconnectConfig.InitialDelay",
		"ReconnectConfig.MaxDelay",
		"ReconnectConfig.BackoffMultiplier",
		"ReconnectConfig.JitterFactor",
		"ReconnectConfig.ReconnectTimeout",
	}
	for _, token := range want {
		if !strings.Contains(err.Error(), token) {
			t.Fatalf("expected error to contain %q, got: %v", token, err)
		}
	}
}

func TestReconnectConfigValidationMaxDelayLessThanInitial(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NetworkPassword = "test"
	cfg.ReconnectConfig = &ReconnectConfig{
		Enabled:           true,
		MaxRetries:        3,
		InitialDelay:      5 * time.Second,
		MaxDelay:          time.Second,
		BackoffMultiplier: 2,
		JitterFactor:      0.2,
		ReconnectTimeout:  5 * time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected max delay validation error")
	}
	if !strings.Contains(err.Error(), "ReconnectConfig.MaxDelay must be >=") {
		t.Fatalf("unexpected error: %v", err)
	}
}
