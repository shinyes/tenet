package node

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func createBroadcastTestNode(t *testing.T, channel string, opts ...Option) *Node {
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

func connectForBroadcastTest(t *testing.T, from, to *Node) {
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

func TestBroadcastWithoutChannelMatchDoesNotSend(t *testing.T) {
	channelA := "channel-a"
	channelB := "channel-b"

	node1 := createBroadcastTestNode(t, channelA)
	defer node1.Stop()
	node2 := createBroadcastTestNode(t, channelB)
	defer node2.Stop()

	var received int32
	node2.OnReceive(func(peerID string, data []byte) {
		atomic.AddInt32(&received, 1)
		_ = peerID
		_ = data
	})

	connectForBroadcastTest(t, node1, node2)

	count, err := node1.Broadcast(channelA, []byte("broadcast strict channel match"))
	if err != nil {
		t.Fatalf("broadcast failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected broadcast count=0 without channel match, got %d", count)
	}

	time.Sleep(300 * time.Millisecond)
	if atomic.LoadInt32(&received) != 0 {
		t.Fatal("different-channel peer should not receive app payload")
	}
}
