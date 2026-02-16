package node

import (
	"context"
	"fmt"
	"net"

	"github.com/shinyes/tenet/nat"
	"github.com/shinyes/tenet/transport"
)

// startUDP initializes the UDP socket and mux stack.
func (n *Node) startUDP(listenAddr string) error {
	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("resolve listen addr failed: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen udp failed: %w", err)
	}

	_ = conn.SetReadBuffer(4 * 1024 * 1024)
	_ = conn.SetWriteBuffer(4 * 1024 * 1024)

	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = conn.Close()
		return fmt.Errorf("unexpected local addr type: %T", conn.LocalAddr())
	}

	n.conn = conn
	n.LocalAddr = udpAddr
	n.mux = transport.NewUDPMux(n.conn)
	n.mux.Start()
	n.tentConn = n.mux.GetTentConn()
	n.localPeerID = n.Identity.ID.String()
	return nil
}

// startTCP binds TCP on the same port as UDP for punch compatibility.
func (n *Node) startTCP() error {
	tcpPort := n.LocalAddr.Port
	tcpListenAddr := fmt.Sprintf("[::]:%d", tcpPort)
	lc := transport.ListenConfig()
	listener, err := lc.Listen(context.Background(), "tcp", tcpListenAddr)
	if err != nil {
		return fmt.Errorf("listen tcp failed: %w", err)
	}

	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		_ = listener.Close()
		return fmt.Errorf("unexpected listener type: %T", listener)
	}

	n.tcpListener = tcpListener
	n.tcpLocalPort = tcpPort
	return nil
}

// startRelay initializes relay manager and static relay nodes.
func (n *Node) startRelay() {
	n.relayManager = nat.NewRelayManager(n.mux.GetTentConn())
	for _, addrStr := range n.Config.RelayNodes {
		relayAddr, err := net.ResolveUDPAddr("udp", addrStr)
		if err != nil {
			n.Config.Logger.Error("invalid relay addr: %s: %v", addrStr, err)
			continue
		}
		n.relayManager.AddRelay(addrStr, relayAddr)
		n.relayAddrSet[relayAddr.String()] = true
	}
}

// startNATAndKCP initializes NAT probe and optional KCP transport.
func (n *Node) startNATAndKCP() {
	n.natProber = nat.NewNATProber(n.mux.GetPunchConn())
	n.kcpTransport = NewKCPTransport(n, n.Config.KCPConfig, n.mux)
	if err := n.kcpTransport.Start(); err != nil {
		n.Config.Logger.Warn("kcp start failed: %v", err)
		n.kcpTransport = nil
		return
	}
	n.Config.Logger.Info("kcp mux started")
}

// startReconnectManager wires reconnect callbacks when enabled.
func (n *Node) startReconnectManager() {
	if !n.Config.EnableReconnect {
		return
	}

	reconnectCfg := n.Config.ReconnectConfig
	if reconnectCfg == nil {
		reconnectCfg = DefaultReconnectConfig()
	}
	n.reconnectManager = NewReconnectManager(n, reconnectCfg)

	n.mu.RLock()
	if n.onReconnecting != nil {
		n.reconnectManager.SetOnReconnecting(n.onReconnecting)
	}
	if n.onReconnected != nil {
		n.reconnectManager.SetOnReconnected(n.onReconnected)
	}
	if n.onGaveUp != nil {
		n.reconnectManager.SetOnGaveUp(n.onGaveUp)
	}
	n.mu.RUnlock()

	n.Config.Logger.Info("reconnect manager enabled, max retries: %d", reconnectCfg.MaxRetries)
}

// startBackgroundLoops starts read/accept/heartbeat loops.
func (n *Node) startBackgroundLoops() {
	n.wg.Add(3)
	go n.handleRead()
	go n.acceptTCP()
	go n.heartbeatLoop()
}

// closeOnStartFailure rolls back partially initialized network resources.
func (n *Node) closeOnStartFailure() {
	if n.tcpListener != nil {
		_ = n.tcpListener.Close()
		n.tcpListener = nil
	}
	if n.mux != nil {
		_ = n.mux.Close()
		n.mux = nil
		n.tentConn = nil
	}
	if n.conn != nil {
		_ = n.conn.Close()
		n.conn = nil
	}
}
