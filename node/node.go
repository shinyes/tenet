package node

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/shinyes/tenet/crypto"
	"github.com/shinyes/tenet/metrics"
	"github.com/shinyes/tenet/nat"
	"github.com/shinyes/tenet/peer"
	"github.com/shinyes/tenet/transport"
)

const (
	//
	MagicBytes = "TENT"

	//
	PacketTypeHandshake     = 0x01 //
	PacketTypeData          = 0x02 //
	PacketTypeRelay         = 0x03 //
	PacketTypeDiscoveryReq  = 0x04 //
	PacketTypeDiscoveryResp = 0x05 //
	PacketTypeHeartbeat     = 0x06 //
	PacketTypeHeartbeatAck  = 0x07 //

	//
	RelayModeForward = 0x01 //
	RelayModeTarget  = 0x02 //

	//
	FrameTypeSingle = 0x01 //
	FrameTypeFirst  = 0x02 //
	FrameTypeMiddle = 0x03 //
	FrameTypeLast   = 0x04 //

	//
	MaxPayloadSize    = 60 * 1024        //
	MaxReassemblySize = 50 * 1024 * 1024 //
)

type Node struct {
	Config   *Config
	Identity *crypto.Identity
	Peers    *peer.PeerStore

	conn         *net.UDPConn
	mux          *transport.UDPMux //
	tentConn     net.PacketConn    //
	tcpListener  *net.TCPListener
	tcpLocalPort int //
	LocalAddr    *net.UDPAddr
	PublicAddr   *net.UDPAddr // Can be set after NAT discovery

	localPeerID       string //
	pendingHandshakes map[string]*crypto.HandshakeState
	addrToPeer        map[string]string // Addr.String() -> PeerID

	onReceive          func(peerID string, data []byte)
	onPeerConnected    func(peerID string)
	onPeerDisconnected func(peerID string)
	mu                 sync.RWMutex
	tcpWriteMuMap      sync.Map // map[net.Conn]*sync.Mutex
	tcpSessionSem      chan struct{}

	closing chan struct{}
	wg      sync.WaitGroup

	relayManager       *nat.RelayManager
	relayAddrSet       map[string]bool
	relayForward       map[string]*net.UDPAddr
	relayPendingTarget map[string]*net.UDPAddr // relayAddr -> targetAddr
	relayInbound       map[string]bool

	metrics   *metrics.Collector      //
	natProber *nat.NATProber          //
	natInfo   *nat.NATInfo            //
	relayAuth *nat.RelayAuthenticator //

	//
	kcpTransport *KCPTransport

	//
	reconnectManager *ReconnectManager

	//
	onReconnecting func(peerID string, attempt int, nextRetryIn time.Duration)
	onReconnected  func(peerID string, attempts int)
	onGaveUp       func(peerID string, attempts int, lastErr error)

	discoveryConnectSem  chan struct{}
	discoveryConnectSeen map[string]time.Time
	localChannelSet      map[[32]byte]struct{}
}

func NewNode(opts ...Option) (*Node, error) {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	//
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid node config: %w", err)
	}

	//
	var id *crypto.Identity
	var err error

	if cfg.Identity != nil {
		id = cfg.Identity
	} else {
		//
		id, err = crypto.NewIdentity()
		if err != nil {
			return nil, fmt.Errorf("create identity failed: %w", err)
		}
	}

	localChannelSet := make(map[[32]byte]struct{}, len(cfg.Channels))
	for _, ch := range cfg.Channels {
		localChannelSet[hashChannelName(ch)] = struct{}{}
	}

	return &Node{
		Config:             cfg,
		Identity:           id,
		Peers:              peer.NewPeerStore(),
		pendingHandshakes:  make(map[string]*crypto.HandshakeState),
		addrToPeer:         make(map[string]string),
		closing:            make(chan struct{}),
		relayAddrSet:       make(map[string]bool),
		relayForward:       make(map[string]*net.UDPAddr),
		relayPendingTarget: make(map[string]*net.UDPAddr),
		relayInbound:       make(map[string]bool),
		metrics:            metrics.NewCollector(),
		tcpSessionSem:      make(chan struct{}, maxConcurrentTCPHandlers),
		discoveryConnectSem: make(
			chan struct{},
			discoveryConnectConcurrencyLimit,
		),
		discoveryConnectSeen: make(map[string]time.Time),
		localChannelSet:      localChannelSet,
	}, nil
}

func (n *Node) ID() string {
	return n.Identity.ID.String()
}

func (n *Node) Start() error {
	listenAddr := fmt.Sprintf("[::]:%d", n.Config.ListenPort)
	if err := n.startUDP(listenAddr); err != nil {
		return err
	}
	if err := n.startTCP(); err != nil {
		n.closeOnStartFailure()
		return err
	}
	n.startRelay()
	n.startNATAndKCP()
	n.startReconnectManager()
	n.startBackgroundLoops()

	n.Config.Logger.Info("node started, listening %s (ID: %s)", listenAddr, n.localPeerID[:8])
	return nil
}

func (n *Node) Stop() error {
	return n.GracefulStop(context.Background())
}

func (n *Node) GracefulStop(ctx context.Context) error {
	select {
	case <-n.closing:
		return nil
	default:
	}

	n.Config.Logger.Info("node is shutting down")

	// ... (peer closing same)
	peerIDs := n.Peers.IDs()
	for _, peerID := range peerIDs {
		p, ok := n.Peers.Get(peerID)
		if !ok {
			continue
		}
		p.SetState(peer.StateDisconnecting)
		transport, addr, conn := p.GetTransportInfo()
		goodbyePacket := n.buildGoodbyePacket()
		n.sendRaw(conn, addr, transport, goodbyePacket)
	}

	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
	}

	close(n.closing)

	if n.mux != nil {
		n.mux.Close() // This closes n.conn too
	} else if n.conn != nil {
		n.conn.Close()
	}

	if n.tcpListener != nil {
		n.tcpListener.Close()
	}

	if n.natProber != nil {
		n.natProber.Close()
	}

	if n.kcpTransport != nil {
		n.kcpTransport.Close()
	}

	if n.reconnectManager != nil {
		n.reconnectManager.Close()
	}

	for _, peerID := range peerIDs {
		if p, ok := n.Peers.Get(peerID); ok {
			p.Close()
		}
	}

	n.wg.Wait()
	n.Config.Logger.Info("node closed")
	return nil
}

func (n *Node) buildGoodbyePacket() []byte {
	//
	packet := make([]byte, 6)
	copy(packet[0:4], []byte(MagicBytes))
	packet[4] = PacketTypeHeartbeat
	packet[5] = 0xFF //
	return packet
}

func (n *Node) Connect(addrStr string) error {
	return n.ConnectContext(context.Background(), addrStr)
}

// ConnectContext starts TCP/UDP hole punching with context cancellation support.
func (n *Node) ConnectContext(ctx context.Context, addrStr string) error {
	return n.connectContext(ctx, addrStr)
}

func (n *Node) OnReceive(f func(string, []byte)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onReceive = f
}

func (n *Node) OnPeerConnected(f func(string)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onPeerConnected = f
}

func (n *Node) OnPeerDisconnected(f func(string)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onPeerDisconnected = f
}

func (n *Node) handlePacket(conn net.Conn, remoteAddr net.Addr, transport string, packetType byte, payload []byte) {
	switch packetType {

	case PacketTypeHandshake:
		n.processHandshake(conn, remoteAddr, transport, payload)
	case PacketTypeData:
		n.processData(remoteAddr, payload)
	case PacketTypeDiscoveryReq:
		n.processDiscoveryRequest(conn, remoteAddr, transport, payload)
	case PacketTypeDiscoveryResp:
		n.processDiscoveryResponse(remoteAddr, payload)
	case PacketTypeHeartbeat:
		n.processHeartbeat(conn, remoteAddr, transport)
	case PacketTypeHeartbeatAck:
		n.processHeartbeatAck(remoteAddr)
	}
}

func (n *Node) GetPeerTransport(peerID string) string {
	p, ok := n.Peers.Get(peerID)
	if !ok {
		return ""
	}
	transport, _, _ := p.GetTransportInfo()
	if transport == "" {
		return "udp"
	}
	return transport
}

func (n *Node) GetPeerLinkMode(peerID string) string {
	p, ok := n.Peers.Get(peerID)
	if !ok {
		return ""
	}
	linkMode := p.GetLinkMode()
	if linkMode == "" {
		return "p2p"
	}
	return linkMode
}

func (n *Node) GetMetrics() metrics.Snapshot {
	if n.metrics == nil {
		return metrics.Snapshot{}
	}
	n.metrics.SetConnectionsActive(int64(n.Peers.Count()))
	return n.metrics.GetSnapshot()
}

func (n *Node) GetNATInfo() *nat.NATInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.natInfo
}

func (n *Node) ProbeNAT() (*nat.ProbeResult, error) {
	if n.natProber == nil {
		return nil, fmt.Errorf("nat prober is not initialized")
	}

	peerIDs := n.Peers.IDs()
	if len(peerIDs) == 0 {
		return nil, fmt.Errorf("no connected peers available for nat probe")
	}

	var helperAddrs []*net.UDPAddr
	for _, peerID := range peerIDs {
		p, ok := n.Peers.Get(peerID)
		if !ok {
			continue
		}
		_, addr, _ := p.GetTransportInfo()
		if udpAddr, ok := addr.(*net.UDPAddr); ok {
			helperAddrs = append(helperAddrs, udpAddr)
		}
	}

	if len(helperAddrs) == 0 {
		return nil, fmt.Errorf("no udp helper peers available for nat probe")
	}

	result, err := n.natProber.ProbeViaHelper(helperAddrs)
	if err != nil {
		return nil, err
	}

	n.mu.Lock()
	n.natInfo = &nat.NATInfo{
		Type:            result.NATType,
		PublicAddr:      result.PublicAddr,
		PortPredictable: result.PortPredictable,
		PortDelta:       result.PortDelta,
		LastProbe:       result.ProbeTime,
	}
	n.mu.Unlock()

	return result, nil
}

func (n *Node) GetPeerStats(peerID string) (peer.PeerStats, bool) {
	p, ok := n.Peers.Get(peerID)
	if !ok {
		return peer.PeerStats{}, false
	}
	return p.GetStats(), true
}

//

func (n *Node) OnReconnecting(handler func(peerID string, attempt int, nextRetryIn time.Duration)) {
	n.mu.Lock()
	n.onReconnecting = handler
	if n.reconnectManager != nil {
		n.reconnectManager.SetOnReconnecting(handler)
	}
	n.mu.Unlock()
}

func (n *Node) OnReconnected(handler func(peerID string, attempts int)) {
	n.mu.Lock()
	n.onReconnected = handler
	if n.reconnectManager != nil {
		n.reconnectManager.SetOnReconnected(handler)
	}
	n.mu.Unlock()
}

func (n *Node) OnGaveUp(handler func(peerID string, attempts int, lastErr error)) {
	n.mu.Lock()
	n.onGaveUp = handler
	if n.reconnectManager != nil {
		n.reconnectManager.SetOnGaveUp(handler)
	}
	n.mu.Unlock()
}

func (n *Node) GetReconnectInfo(peerID string) *ReconnectInfo {
	if n.reconnectManager == nil {
		return nil
	}
	return n.reconnectManager.GetReconnectInfo(peerID)
}

func (n *Node) GetAllReconnectInfo() []*ReconnectInfo {
	if n.reconnectManager == nil {
		return nil
	}
	return n.reconnectManager.GetAllReconnectInfo()
}

func (n *Node) CancelReconnect(peerID string) {
	if n.reconnectManager != nil {
		n.reconnectManager.CancelReconnect(peerID)
	}
}

func (n *Node) CancelAllReconnects() {
	if n.reconnectManager != nil {
		n.reconnectManager.CancelAll()
	}
}
