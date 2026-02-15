package node

import (
	"net"
	"testing"
	"time"

	"github.com/shinyes/tenet/peer"
)

func buildSingleDiscoveryPayload(peerID, addr string) []byte {
	payload := make([]byte, 2+1+len(peerID)+1+len(addr))
	payload[1] = 1 // count = 1

	offset := 2
	payload[offset] = byte(len(peerID))
	offset++
	copy(payload[offset:], peerID)
	offset += len(peerID)
	payload[offset] = byte(len(addr))
	offset++
	copy(payload[offset:], addr)

	return payload
}

func TestProcessDiscoveryResponseShortPeerIDNoPanic(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}

	sourceAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19001}
	helperPeerID := "helper-peer"
	n.Peers.Add(&peer.Peer{
		ID:        helperPeerID,
		Addr:      sourceAddr,
		Transport: "udp",
		LastSeen:  time.Now(),
	})
	n.mu.Lock()
	n.addrToPeer[sourceAddr.String()] = helperPeerID
	n.mu.Unlock()

	peerID := "abc"
	n.Peers.Add(&peer.Peer{ID: peerID}) // avoid launching outbound connect in this panic-safety test
	addr := "127.0.0.1:1"
	payload := buildSingleDiscoveryPayload(peerID, addr)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("processDiscoveryResponse panicked with short peerID: %v", r)
		}
	}()

	n.processDiscoveryResponse(sourceAddr, payload)
}

func TestProcessDiscoveryResponseRejectsUnauthenticatedSource(t *testing.T) {
	n := newDiscoveryTestNode(t)

	payload := buildSingleDiscoveryPayload("peer-unauth", "127.0.0.1:19002")
	sourceAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19003}

	n.processDiscoveryResponse(sourceAddr, payload)

	n.mu.RLock()
	seenLen := len(n.discoveryConnectSeen)
	n.mu.RUnlock()
	if seenLen != 0 {
		t.Fatalf("unauthenticated discovery response should be ignored, got seen size %d", seenLen)
	}
}

func TestProcessDiscoveryResponseAllowsAuthenticatedSource(t *testing.T) {
	n := newDiscoveryTestNode(t)

	sourceAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19004}
	helperPeerID := "helper-peer"
	n.Peers.Add(&peer.Peer{
		ID:        helperPeerID,
		Addr:      sourceAddr,
		Transport: "udp",
		LastSeen:  time.Now(),
	})
	n.mu.Lock()
	n.addrToPeer[sourceAddr.String()] = helperPeerID
	n.mu.Unlock()

	payload := buildSingleDiscoveryPayload("peer-auth", "127.0.0.1:19005")
	n.processDiscoveryResponse(sourceAddr, payload)

	n.mu.RLock()
	seenLen := len(n.discoveryConnectSeen)
	n.mu.RUnlock()
	if seenLen != 1 {
		t.Fatalf("authenticated discovery response should be processed, got seen size %d", seenLen)
	}
}

func TestProbeNATViaHelperPeer(t *testing.T) {
	const password = "nat-probe-secret"

	n1, err := NewNode(
		WithNetworkPassword(password),
		WithListenPort(0),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("new node1 failed: %v", err)
	}
	n2, err := NewNode(
		WithNetworkPassword(password),
		WithListenPort(0),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("new node2 failed: %v", err)
	}

	if err := n1.Start(); err != nil {
		t.Fatalf("start node1 failed: %v", err)
	}
	if err := n2.Start(); err != nil {
		_ = n1.Stop()
		t.Fatalf("start node2 failed: %v", err)
	}
	defer n1.Stop()
	defer n2.Stop()

	n1.Peers.Add(&peer.Peer{
		ID:        "helper-peer",
		Addr:      &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: n2.LocalAddr.Port},
		Transport: "udp",
		LastSeen:  time.Now(),
	})

	result, err := n1.ProbeNAT()
	if err != nil {
		t.Fatalf("ProbeNAT failed: %v", err)
	}
	if result == nil {
		t.Fatal("ProbeNAT returned nil result")
	}
	if result.PublicAddr == nil {
		t.Fatal("ProbeNAT returned nil public address")
	}
}
