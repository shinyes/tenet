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
