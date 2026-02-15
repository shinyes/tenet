package node

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReconnectTimeoutRespected(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithListenPort(0),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}
	if err := n.Start(); err != nil {
		t.Fatalf("start node failed: %v", err)
	}
	defer n.Stop()

	cfg := &ReconnectConfig{
		Enabled:           true,
		MaxRetries:        1,
		InitialDelay:      10 * time.Millisecond,
		MaxDelay:          10 * time.Millisecond,
		BackoffMultiplier: 1,
		JitterFactor:      0,
		ReconnectTimeout:  time.Nanosecond,
	}
	rm := NewReconnectManager(n, cfg)
	defer rm.Close()

	errCh := make(chan error, 1)
	rm.SetOnGaveUp(func(peerID string, attempts int, lastErr error) {
		errCh <- lastErr
	})

	start := time.Now()
	rm.ScheduleReconnect("timeout-peer", "203.0.113.1:65001")

	select {
	case lastErr := <-errCh:
		if !errors.Is(lastErr, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got: %v", lastErr)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("reconnect timeout not respected, elapsed=%v", elapsed)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for reconnect give-up callback")
	}
}
