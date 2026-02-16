package node

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
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

func TestConnectContextNodeNotStarted(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}

	err = n.ConnectContext(context.Background(), "203.0.113.1:65000")
	if err == nil {
		t.Fatal("expected not started error, got nil")
	}
	if !strings.Contains(err.Error(), "node not started") {
		t.Fatalf("expected node not started, got: %v", err)
	}
}

func TestConnectContextReturnsWhenNodeClosing(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithListenPort(0),
		WithEnableRelay(false),
		WithEnableHolePunch(false),
		WithEnableReconnect(false),
		WithDialTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}
	if err := n.Start(); err != nil {
		t.Fatalf("start node failed: %v", err)
	}
	defer n.Stop()

	close(n.closing)

	err = n.ConnectContext(context.Background(), "203.0.113.1:65000")
	if err == nil {
		t.Fatal("expected closing error, got nil")
	}
	if !strings.Contains(err.Error(), "node is closing") {
		t.Fatalf("expected node is closing, got: %v", err)
	}
}

func TestAwaitConnectResultTimeout(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithEnableRelay(false),
		WithEnableReconnect(false),
		WithDialTimeout(30*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}

	resultChan := make(chan connectResult, 1)
	rUDPAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}

	start := time.Now()
	err = n.awaitConnectResult(context.Background(), "127.0.0.1:12345", rUDPAddr, nil, resultChan)
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "connect timeout") {
		t.Fatalf("expected connect timeout, got: %v", err)
	}
	if elapsed < 20*time.Millisecond {
		t.Fatalf("timeout returned too early: %v", elapsed)
	}
}

func TestAwaitConnectResultReturnsTCPErrorOnTimeout(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithEnableRelay(false),
		WithEnableReconnect(false),
		WithDialTimeout(30*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}

	tcpErr := errors.New("tcp punch failed")
	resultChan := make(chan connectResult, 1)
	resultChan <- connectResult{Transport: "tcp", Err: tcpErr}
	rUDPAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}

	err = n.awaitConnectResult(context.Background(), "127.0.0.1:12345", rUDPAddr, nil, resultChan)
	if !errors.Is(err, tcpErr) {
		t.Fatalf("expected tcp error, got: %v", err)
	}
}

func TestAwaitConnectResultReturnsNilOnUDPMarker(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithEnableRelay(false),
		WithEnableReconnect(false),
		WithDialTimeout(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}

	resultChan := make(chan connectResult, 1)
	resultChan <- connectResult{Transport: "udp"}
	rUDPAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}

	err = n.awaitConnectResult(context.Background(), "127.0.0.1:12345", rUDPAddr, nil, resultChan)
	if err != nil {
		t.Fatalf("expected nil on udp marker, got: %v", err)
	}
}

type testConn struct {
	closed atomic.Bool
}

func (c *testConn) Read(_ []byte) (int, error)         { return 0, io.EOF }
func (c *testConn) Write(p []byte) (int, error)        { return len(p), nil }
func (c *testConn) Close() error                       { c.closed.Store(true); return nil }
func (c *testConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *testConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (c *testConn) SetDeadline(_ time.Time) error      { return nil }
func (c *testConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *testConn) SetWriteDeadline(_ time.Time) error { return nil }

func TestAwaitLateTCPUpgradeClosesNonTCPConn(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithEnableRelay(false),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultChan := make(chan connectResult, 1)
	conn := &testConn{}
	resultChan <- connectResult{Transport: "udp", Conn: conn}

	n.awaitLateTCPUpgrade(ctx, resultChan, []byte("dummy"))
	time.Sleep(50 * time.Millisecond)

	if !conn.closed.Load() {
		t.Fatal("expected non-tcp conn to be closed")
	}
}

func TestAwaitLateTCPUpgradeExitsOnContextCancel(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithEnableRelay(false),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	resultChan := make(chan connectResult, 1)

	n.awaitLateTCPUpgrade(ctx, resultChan, []byte("dummy"))
	cancel()
	time.Sleep(20 * time.Millisecond)

	select {
	case resultChan <- connectResult{Transport: "udp"}:
		// Goroutine should have exited and not consume from channel.
	default:
		t.Fatal("result channel unexpectedly consumed after context cancel")
	}
}

func TestBuildInitiatorHandshakePacketsFormat(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithEnableRelay(false),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}

	hsUDP, hsTCP, packetUDP, packetTCP, err := n.buildInitiatorHandshakePackets()
	if err != nil {
		t.Fatalf("build handshake packets failed: %v", err)
	}
	if hsUDP == nil || hsTCP == nil {
		t.Fatal("expected non-nil handshake states")
	}
	if len(packetUDP) < 6 || len(packetTCP) < 6 {
		t.Fatalf("unexpected packet size: udp=%d tcp=%d", len(packetUDP), len(packetTCP))
	}
	if !bytes.Equal(packetUDP[:4], []byte("TENT")) || packetUDP[4] != PacketTypeHandshake {
		t.Fatalf("invalid udp handshake packet prefix/type: %x", packetUDP[:5])
	}
	if !bytes.Equal(packetTCP[:4], []byte("TENT")) || packetTCP[4] != PacketTypeHandshake {
		t.Fatalf("invalid tcp handshake packet prefix/type: %x", packetTCP[:5])
	}
}

func TestRegisterPendingHandshakesStoresBothTransports(t *testing.T) {
	n, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithEnableRelay(false),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("new node failed: %v", err)
	}

	hsUDP, hsTCP, _, _, err := n.buildInitiatorHandshakePackets()
	if err != nil {
		t.Fatalf("build handshake packets failed: %v", err)
	}

	rUDP := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
	rTCP := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
	n.registerPendingHandshakes(rUDP, rTCP, hsUDP, hsTCP)

	udpKey := "udp://" + rUDP.String()
	tcpKey := "tcp://" + rTCP.String()

	n.mu.RLock()
	_, okUDP := n.pendingHandshakes[udpKey]
	_, okTCP := n.pendingHandshakes[tcpKey]
	n.mu.RUnlock()

	if !okUDP || !okTCP {
		t.Fatalf("pending handshakes not stored, udp=%v tcp=%v", okUDP, okTCP)
	}
}
