package peer

import (
	"net"
	"sync"
	"time"

	"github.com/cykyes/tenet/crypto"
)

// Peer represents a connected node
type Peer struct {
	ID          string
	Addr        net.Addr     // Generalized to net.Addr
	Conn        net.Conn     // Active TCP connection, if any
	Transport   string       // "tcp" or "udp"
	LinkMode    string       // "p2p" or "relay"
	RelayTarget *net.UDPAddr // 通过中继访问的目标地址（发起方使用）
	Session     *crypto.Session
	LastSeen    time.Time
	mu          sync.RWMutex
}

// UpgradeTransport safely updates the peer connection info
func (p *Peer) UpgradeTransport(addr net.Addr, conn net.Conn, transport string, session *crypto.Session) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Close old connection if it exists and is different.
	// IMPORTANT: Only close if the previous transport was NOT UDP,
	// because UDP uses the shared Listener (n.conn) which must stay open.
	if p.Conn != nil && p.Conn != conn && p.Transport != "udp" {
		p.Conn.Close()
	}

	p.Addr = addr
	p.Conn = conn
	p.Transport = transport
	p.Session = session
	p.LastSeen = time.Now()
}

// SetLinkMode 设置链路模式和中继目标
func (p *Peer) SetLinkMode(mode string, relayTarget *net.UDPAddr) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.LinkMode = mode
	p.RelayTarget = relayTarget
}

// GetTransportInfo returns the current transport and address
func (p *Peer) GetTransportInfo() (string, net.Addr, net.Conn) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Transport, p.Addr, p.Conn
}

// UpdateLastSeen updates the last seen timestamp
func (p *Peer) UpdateLastSeen() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.LastSeen = time.Now()
}

// PeerStore manages connected peers in a thread-safe way
type PeerStore struct {
	peers map[string]*Peer
	mu    sync.RWMutex
}

// NewPeerStore creates a new PeerStore
func NewPeerStore() *PeerStore {
	return &PeerStore{
		peers: make(map[string]*Peer),
	}
}

// Add adds or updates a peer
func (ps *PeerStore) Add(p *Peer) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.peers[p.ID] = p
}

// Remove removes a peer by ID
func (ps *PeerStore) Remove(id string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.peers, id)
}

// Get retrieves a peer by ID
func (ps *PeerStore) Get(id string) (*Peer, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.peers[id]
	return p, ok
}

// IDs returns a list of all peer IDs
func (ps *PeerStore) IDs() []string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	ids := make([]string, 0, len(ps.peers))
	for id := range ps.peers {
		ids = append(ids, id)
	}
	return ids
}

// Count returns the number of connected peers
func (ps *PeerStore) Count() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.peers)
}
