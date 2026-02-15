package node

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConnectContextHonorsDeadline(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err = n.ConnectContext(ctx, "203.0.113.1:65000")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got: %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("connect context timeout not honored, elapsed=%v", elapsed)
	}
}
