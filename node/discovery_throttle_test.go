package node

import (
	"fmt"
	"testing"
	"time"
)

func newDiscoveryTestNode(t *testing.T) *Node {
	t.Helper()

	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}
	return n
}

func TestDiscoveryConnectDedupeWindow(t *testing.T) {
	n := newDiscoveryTestNode(t)

	peerID := "peer-1"
	addr := "127.0.0.1:10001"
	key := peerID + "|" + addr

	if !n.tryBeginDiscoveryConnect(peerID, addr) {
		t.Fatal("first discovery connect should pass")
	}
	if n.tryBeginDiscoveryConnect(peerID, addr) {
		t.Fatal("duplicate discovery connect should be deduped")
	}
	n.endDiscoveryConnect()

	n.mu.Lock()
	n.discoveryConnectSeen[key] = time.Now().Add(-time.Second)
	n.mu.Unlock()

	if !n.tryBeginDiscoveryConnect(peerID, addr) {
		t.Fatal("discovery connect should pass after dedupe window expires")
	}
	n.endDiscoveryConnect()
}

func TestDiscoveryConnectConcurrencyLimit(t *testing.T) {
	n := newDiscoveryTestNode(t)

	limit := cap(n.discoveryConnectSem)
	for i := 0; i < limit; i++ {
		peerID := fmt.Sprintf("peer-%d", i)
		addr := fmt.Sprintf("127.0.0.1:%d", 20000+i)
		if !n.tryBeginDiscoveryConnect(peerID, addr) {
			t.Fatalf("expected slot %d to be acquired", i)
		}
	}

	if n.tryBeginDiscoveryConnect("overflow", "127.0.0.1:29999") {
		t.Fatal("expected overflow connect to be throttled")
	}

	for i := 0; i < limit; i++ {
		n.endDiscoveryConnect()
	}

	if !n.tryBeginDiscoveryConnect("after-release", "127.0.0.1:30000") {
		t.Fatal("expected connect to pass after releasing slots")
	}
	n.endDiscoveryConnect()
}

func TestDiscoveryConnectSeenHardLimit(t *testing.T) {
	n := newDiscoveryTestNode(t)

	total := discoveryConnectSeenHardLimit + 128
	for i := 0; i < total; i++ {
		peerID := fmt.Sprintf("peer-hard-%d", i)
		addr := fmt.Sprintf("127.0.0.1:%d", 40000+i)
		if !n.tryBeginDiscoveryConnect(peerID, addr) {
			t.Fatalf("expected connect %d to be admitted", i)
		}
		n.endDiscoveryConnect()
	}

	n.mu.RLock()
	seenLen := len(n.discoveryConnectSeen)
	n.mu.RUnlock()
	if seenLen > discoveryConnectSeenHardLimit {
		t.Fatalf("discovery seen map exceeded hard limit: got %d > %d", seenLen, discoveryConnectSeenHardLimit)
	}
}
