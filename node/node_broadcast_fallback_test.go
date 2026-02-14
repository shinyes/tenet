package node

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func createBroadcastFallbackTestNode(t *testing.T, channel string, opts ...Option) *Node {
	t.Helper()

	base := []Option{
		WithNetworkPassword("test-secret"),
		WithListenPort(0),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
	}
	if channel != "" {
		base = append(base, WithChannelID(channel))
	}
	base = append(base, opts...)

	n, err := NewNode(base...)
	if err != nil {
		t.Fatalf("create node failed: %v", err)
	}
	if err := n.Start(); err != nil {
		t.Fatalf("start node failed: %v", err)
	}
	return n
}

func connectForBroadcastFallbackTest(t *testing.T, from, to *Node) {
	t.Helper()

	connected := make(chan struct{}, 1)
	from.OnPeerConnected(func(peerID string) {
		select {
		case connected <- struct{}{}:
		default:
		}
		_ = peerID
	})

	addr := fmt.Sprintf("127.0.0.1:%d", to.LocalAddr.Port)
	if err := from.Connect(addr); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("connect timeout")
	}
}

func TestBroadcastFallbackSwitch(t *testing.T) {
	if !DefaultConfig().EnableBroadcastFallback {
		t.Fatal("default config should enable broadcast fallback")
	}

	t.Run("enabled-by-default", func(t *testing.T) {
		channelA := "channel-a"
		channelB := "channel-b"

		node1 := createBroadcastFallbackTestNode(t, channelA)
		defer node1.Stop()
		node2 := createBroadcastFallbackTestNode(t, channelB)
		defer node2.Stop()

		var received int32
		node2.OnReceive(func(peerID string, data []byte) {
			atomic.AddInt32(&received, 1)
			_ = peerID
			_ = data
		})

		connectForBroadcastFallbackTest(t, node1, node2)

		count, err := node1.Broadcast(channelA, []byte("broadcast fallback default"))
		if err != nil {
			t.Fatalf("broadcast failed: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected fallback broadcast count=1, got %d", count)
		}

		time.Sleep(300 * time.Millisecond)
		if atomic.LoadInt32(&received) != 0 {
			t.Fatal("different-channel peer should not receive app payload")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		channelA := "channel-a"
		channelB := "channel-b"

		node1 := createBroadcastFallbackTestNode(t, channelA, WithEnableBroadcastFallback(false))
		defer node1.Stop()
		node2 := createBroadcastFallbackTestNode(t, channelB)
		defer node2.Stop()

		var received int32
		node2.OnReceive(func(peerID string, data []byte) {
			atomic.AddInt32(&received, 1)
			_ = peerID
			_ = data
		})

		connectForBroadcastFallbackTest(t, node1, node2)

		count, err := node1.Broadcast(channelA, []byte("broadcast fallback disabled"))
		if err != nil {
			t.Fatalf("broadcast failed: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected no fallback broadcast count=0, got %d", count)
		}

		time.Sleep(300 * time.Millisecond)
		if atomic.LoadInt32(&received) != 0 {
			t.Fatal("different-channel peer should not receive app payload")
		}
	})
}
