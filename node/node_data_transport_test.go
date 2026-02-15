package node

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/shinyes/tenet/crypto"
	"github.com/shinyes/tenet/peer"
)

type staticAddr string

func (a staticAddr) Network() string { return "test" }
func (a staticAddr) String() string  { return string(a) }

func makeTestSession(t *testing.T, local, remote *crypto.Identity, password string) *crypto.Session {
	t.Helper()

	initHS, msg1, err := crypto.NewInitiatorHandshake(
		local.NoisePrivateKey[:],
		local.NoisePublicKey[:],
		[]byte(password),
	)
	if err != nil {
		t.Fatalf("new initiator handshake failed: %v", err)
	}

	respHS, err := crypto.NewResponderHandshake(
		remote.NoisePrivateKey[:],
		remote.NoisePublicKey[:],
		[]byte(password),
	)
	if err != nil {
		t.Fatalf("new responder handshake failed: %v", err)
	}

	msg2, _, err := respHS.ProcessMessage(msg1)
	if err != nil {
		t.Fatalf("responder process msg1 failed: %v", err)
	}

	msg3, initSession, err := initHS.ProcessMessage(msg2)
	if err != nil {
		t.Fatalf("initiator process msg2 failed: %v", err)
	}
	if initSession == nil {
		t.Fatal("initiator session is nil after msg2")
	}

	if _, _, err := respHS.ProcessMessage(msg3); err != nil {
		t.Fatalf("responder process msg3 failed: %v", err)
	}

	return initSession
}

func waitUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestSendAppFrameRequiresReliableTransport(t *testing.T) {
	const pwd = "test-secret"

	n, err := NewNode(
		WithNetworkPassword(pwd),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}

	remoteID, err := crypto.NewIdentity()
	if err != nil {
		t.Fatalf("new identity failed: %v", err)
	}

	p := &peer.Peer{
		ID:        remoteID.ID.String(),
		Addr:      &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999},
		Transport: "udp",
		Session:   makeTestSession(t, n.Identity, remoteID, pwd),
		LastSeen:  time.Now(),
	}
	n.Peers.Add(p)

	err = n.sendAppFrame(p.ID, AppFrameTypeChannelUpdate, []byte("hi"))
	if err == nil {
		t.Fatal("expected sendAppFrame to fail without reliable transport")
	}
	if !strings.Contains(err.Error(), "reliable data transport unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecryptFailureThresholdTriggersFastRehandshake(t *testing.T) {
	const pwd = "test-secret"
	const channel = "recovery-channel"
	const msg = "recovered-payload"

	received := make(chan struct{}, 1)

	node1, err := NewNode(
		WithNetworkPassword(pwd),
		WithListenPort(0),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
		WithEnableReconnect(false),
		WithMaxConsecutiveDecryptFailures(3),
		WithChannelID(channel),
	)
	if err != nil {
		t.Fatalf("new node1 failed: %v", err)
	}
	defer node1.Stop()

	node2, err := NewNode(
		WithNetworkPassword(pwd),
		WithListenPort(0),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
		WithEnableReconnect(false),
		WithMaxConsecutiveDecryptFailures(3),
		WithChannelID(channel),
	)
	if err != nil {
		t.Fatalf("new node2 failed: %v", err)
	}
	defer node2.Stop()

	if err := node1.Start(); err != nil {
		t.Fatalf("start node1 failed: %v", err)
	}
	if err := node2.Start(); err != nil {
		t.Fatalf("start node2 failed: %v", err)
	}

	node2.OnReceive(func(peerID string, data []byte) {
		if peerID != node1.ID() || string(data) != msg {
			return
		}
		select {
		case received <- struct{}{}:
		default:
		}
	})

	if err := node1.Connect(fmt.Sprintf("127.0.0.1:%d", node2.LocalAddr.Port)); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	if !waitUntil(5*time.Second, func() bool {
		_, ok := node2.Peers.Get(node1.ID())
		return ok
	}) {
		t.Fatal("node2 did not observe node1 as peer")
	}

	p2, ok := node2.Peers.Get(node1.ID())
	if !ok {
		t.Fatal("node2 peer missing after connect")
	}
	oldSession := p2.GetSession()
	if oldSession == nil {
		t.Fatal("node2 old session is nil")
	}

	var mappedAddr string
	node2.mu.RLock()
	for addr, pid := range node2.addrToPeer {
		if pid == node1.ID() {
			mappedAddr = addr
			break
		}
	}
	node2.mu.RUnlock()
	if mappedAddr == "" {
		t.Fatal("failed to find addrToPeer mapping for node1")
	}

	badPayload := []byte("not-a-valid-noise-ciphertext")
	for i := 0; i < 3; i++ {
		node2.processData(staticAddr(mappedAddr), badPayload)
	}

	if !waitUntil(5*time.Second, func() bool {
		pNow, ok := node2.Peers.Get(node1.ID())
		if !ok {
			return false
		}
		return pNow.GetSession() != oldSession
	}) {
		t.Fatal("expected fast re-handshake to refresh session after decrypt failures")
	}

	if !waitUntil(5*time.Second, func() bool {
		_, ok := node1.Peers.Get(node2.ID())
		return ok
	}) {
		t.Fatal("node1 did not observe node2 as peer")
	}

	if err := node1.Send(channel, node2.ID(), []byte(msg)); err != nil {
		t.Fatalf("send after fast re-handshake failed: %v", err)
	}

	if !waitUntil(5*time.Second, func() bool {
		select {
		case <-received:
			return true
		default:
			return false
		}
	}) {
		t.Fatal("expected business payload delivery after fast re-handshake")
	}
}
