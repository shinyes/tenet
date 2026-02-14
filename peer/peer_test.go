package peer

import (
	"net"
	"sync"
	"testing"
	"time"
)

func TestConnStateString(t *testing.T) {
	tests := []struct {
		state    ConnState
		expected string
	}{
		{StateDisconnected, "Disconnected"},
		{StateConnecting, "Connecting"},
		{StateHandshaking, "Handshaking"},
		{StateConnected, "Connected"},
		{StateDisconnecting, "Disconnecting"},
		{ConnState(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("ConnState(%d).String() = %s, 期望 %s", tt.state, got, tt.expected)
		}
	}
}

func TestNewPeerStore(t *testing.T) {
	store := NewPeerStore()
	if store == nil {
		t.Fatal("NewPeerStore 返回 nil")
	}
	if store.Count() != 0 {
		t.Errorf("新 PeerStore 应该为空，实际 Count=%d", store.Count())
	}
}

func TestPeerStoreAddAndGet(t *testing.T) {
	store := NewPeerStore()

	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:8000")
	peer := &Peer{
		ID:    "peer1",
		Addr:  addr,
		State: StateConnected,
	}

	store.Add(peer)

	if store.Count() != 1 {
		t.Errorf("添加后 Count 期望 1，实际 %d", store.Count())
	}

	got, ok := store.Get("peer1")
	if !ok {
		t.Fatal("Get 返回 ok=false")
	}
	if got.ID != "peer1" {
		t.Errorf("获取的 Peer ID 不匹配: %s", got.ID)
	}
}

func TestPeerStoreRemove(t *testing.T) {
	store := NewPeerStore()

	peer := &Peer{ID: "peer1"}
	store.Add(peer)
	store.Remove("peer1")

	if store.Count() != 0 {
		t.Errorf("删除后 Count 应为 0，实际 %d", store.Count())
	}

	_, ok := store.Get("peer1")
	if ok {
		t.Error("删除后 Get 应返回 ok=false")
	}
}

func TestPeerStoreIDs(t *testing.T) {
	store := NewPeerStore()

	store.Add(&Peer{ID: "peer1"})
	store.Add(&Peer{ID: "peer2"})
	store.Add(&Peer{ID: "peer3"})

	ids := store.IDs()
	if len(ids) != 3 {
		t.Errorf("IDs 长度期望 3，实际 %d", len(ids))
	}

	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	if !idSet["peer1"] || !idSet["peer2"] || !idSet["peer3"] {
		t.Error("IDs 返回的列表不完整")
	}
}

func TestPeerStoreConcurrent(t *testing.T) {
	store := NewPeerStore()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			peerID := string(rune('A' + id%26))
			store.Add(&Peer{ID: peerID})
			store.Get(peerID)
			store.IDs()
		}(i)
	}

	wg.Wait()
	// 不应 panic
}

func TestPeerUpdateLastSeen(t *testing.T) {
	peer := &Peer{ID: "test", LastSeen: time.Now().Add(-time.Hour)}
	before := peer.GetLastSeen()

	time.Sleep(5 * time.Millisecond)
	peer.UpdateLastSeen()

	after := peer.GetLastSeen()
	if !after.After(before) {
		t.Error("UpdateLastSeen 未更新时间")
	}
}

func TestPeerGetLastSeen(t *testing.T) {
	now := time.Now()
	peer := &Peer{ID: "test", LastSeen: now}

	got := peer.GetLastSeen()
	if !got.Equal(now) {
		t.Errorf("GetLastSeen 返回值不正确: %v", got)
	}
}

func TestPeerSetGetState(t *testing.T) {
	peer := &Peer{ID: "test", State: StateDisconnected}

	peer.SetState(StateConnecting)
	if peer.GetState() != StateConnecting {
		t.Error("SetState(StateConnecting) 未生效")
	}

	peer.SetState(StateConnected)
	if peer.GetState() != StateConnected {
		t.Error("SetState(StateConnected) 未生效")
	}
}

func TestPeerStats(t *testing.T) {
	peer := &Peer{ID: "test"}

	peer.AddBytesSent(100)
	peer.AddBytesSent(50)
	peer.AddBytesReceived(200)

	stats := peer.GetStats()
	if stats.BytesSent != 150 {
		t.Errorf("BytesSent 期望 150，实际 %d", stats.BytesSent)
	}
	if stats.BytesReceived != 200 {
		t.Errorf("BytesReceived 期望 200，实际 %d", stats.BytesReceived)
	}
}

func TestPeerSetLinkMode(t *testing.T) {
	peer := &Peer{ID: "test"}
	relayAddr, _ := net.ResolveUDPAddr("udp", "192.168.1.100:9000")

	peer.SetLinkMode("relay", relayAddr)

	peer.mu.RLock()
	defer peer.mu.RUnlock()
	if peer.LinkMode != "relay" {
		t.Errorf("LinkMode 期望 relay，实际 %s", peer.LinkMode)
	}
	if peer.RelayTarget != relayAddr {
		t.Error("RelayTarget 未正确设置")
	}
}

func TestPeerGetTransportInfo(t *testing.T) {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:8000")
	peer := &Peer{
		ID:        "test",
		Addr:      addr,
		Transport: "udp",
		Conn:      nil,
	}

	transport, gotAddr, conn := peer.GetTransportInfo()
	if transport != "udp" {
		t.Errorf("Transport 期望 udp，实际 %s", transport)
	}
	if gotAddr != addr {
		t.Error("Addr 不匹配")
	}
	if conn != nil {
		t.Error("Conn 应为 nil")
	}
}

func TestPeerClose(t *testing.T) {
	peer := &Peer{
		ID:        "test",
		State:     StateConnected,
		Transport: "udp",
	}

	peer.Close()

	if peer.GetState() != StateDisconnected {
		t.Errorf("Close 后状态应为 Disconnected，实际 %v", peer.GetState())
	}
}

func TestPeerReserveRehandshakeAttemptWindow(t *testing.T) {
	p := &Peer{ID: "test"}
	now := time.Now()

	ok, wait := p.ReserveRehandshakeAttempt(now, time.Second, 2)
	if !ok || wait != 0 {
		t.Fatalf("first attempt should pass, ok=%v wait=%v", ok, wait)
	}

	ok, wait = p.ReserveRehandshakeAttempt(now.Add(10*time.Millisecond), time.Second, 2)
	if !ok || wait != 0 {
		t.Fatalf("second attempt should pass, ok=%v wait=%v", ok, wait)
	}

	ok, wait = p.ReserveRehandshakeAttempt(now.Add(20*time.Millisecond), time.Second, 2)
	if ok {
		t.Fatal("third attempt in the same window should be throttled")
	}
	if wait <= 0 {
		t.Fatalf("throttled wait should be positive, got %v", wait)
	}
}

func TestPeerRecordRehandshakeFailureBackoffAndReset(t *testing.T) {
	p := &Peer{ID: "test"}
	base := 100 * time.Millisecond
	max := 800 * time.Millisecond
	now := time.Now()

	failures, delay := p.RecordRehandshakeFailure(now, base, max)
	if failures != 1 {
		t.Fatalf("expected first failure count 1, got %d", failures)
	}
	if delay != base {
		t.Fatalf("expected first backoff %v, got %v", base, delay)
	}

	ok, wait := p.ReserveRehandshakeAttempt(now.Add(20*time.Millisecond), time.Second, 10)
	if ok {
		t.Fatal("attempt during backoff should be throttled")
	}
	if wait <= 0 {
		t.Fatalf("expected positive wait during backoff, got %v", wait)
	}

	failures, delay = p.RecordRehandshakeFailure(now.Add(base), base, max)
	if failures != 2 {
		t.Fatalf("expected second failure count 2, got %d", failures)
	}
	if delay != 2*base {
		t.Fatalf("expected second backoff %v, got %v", 2*base, delay)
	}

	p.RecordRehandshakeSuccess(now.Add(2*base), 0)
	failures, delay = p.RecordRehandshakeFailure(now.Add(3*base), base, max)
	if failures != 1 {
		t.Fatalf("expected failure count reset to 1 after success, got %d", failures)
	}
	if delay != base {
		t.Fatalf("expected backoff reset to base %v, got %v", base, delay)
	}
}
