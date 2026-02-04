package peer

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cykyes/tenet/crypto"
)

// ConnState 连接状态
type ConnState int

const (
	// StateDisconnected 已断开
	StateDisconnected ConnState = iota
	// StateConnecting 正在连接（发起握手中）
	StateConnecting
	// StateHandshaking 握手中（交换密钥）
	StateHandshaking
	// StateConnected 已连接
	StateConnected
	// StateDisconnecting 正在断开（发送关闭通知中）
	StateDisconnecting
)

func (s ConnState) String() string {
	switch s {
	case StateDisconnected:
		return "Disconnected"
	case StateConnecting:
		return "Connecting"
	case StateHandshaking:
		return "Handshaking"
	case StateConnected:
		return "Connected"
	case StateDisconnecting:
		return "Disconnecting"
	default:
		return "Unknown"
	}
}

// Peer represents a connected node
type Peer struct {
	ID              string
	Addr            net.Addr     // Generalized to net.Addr
	OriginalAddr    string       // 原始连接地址（用于重连）
	Conn            net.Conn     // Active TCP connection, if any
	Transport       string       // "tcp" or "udp"
	LinkMode        string       // "p2p" or "relay"
	RelayTarget     *net.UDPAddr // 通过中继访问的目标地址（发起方使用）
	Session         *crypto.Session
	PreviousSession *crypto.Session // 上一个会话（用于平滑切换）

	LastSeen time.Time

	// 连接状态
	State ConnState

	// 连接统计
	ConnectedAt    time.Time     // 连接建立时间
	BytesSent      int64         // 发送字节数
	BytesReceived  int64         // 接收字节数
	MessagesSent   int64         // 发送消息数
	MessagesRecv   int64         // 接收消息数
	ConnectLatency time.Duration // 连接建立延迟
	// 自动分包重组
	ReassemblyBuffer *bytes.Buffer // 重组缓冲区
	LastFrameTime    time.Time     // 上次收到分片的时间

	mu sync.RWMutex
}

// UpgradeTransport safely updates the peer connection info
func (p *Peer) UpgradeTransport(addr net.Addr, conn net.Conn, transport string, session *crypto.Session) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 防止从 TCP 降级到 UDP/KCP
	// 如果当前已经是 TCP，且新传输不是 TCP，则忽略升级（保留 TCP）
	if p.Transport == "tcp" && transport != "tcp" {
		// 但如果要更新 Session，还是允许的？
		// 不，通常 Transport 和 Session 是绑定的。
		// 如果对方试图切换回 KCP，我们应该保持 TCP 连接。
		return
	}

	// Close old connection if it exists and is different.
	// IMPORTANT: Only close if the previous transport was NOT UDP,
	// because UDP uses the shared Listener (n.conn) which must stay open.
	if p.Conn != nil && p.Conn != conn && p.Transport != "udp" {
		p.Conn.Close()
	}

	// 安全关闭旧的 Session，清除密钥材料
	// 策略变更：不要立即关闭，而是将其移动到 PreviousSession
	// 以便处理在传输切换期间仍在途中的旧加密包
	if p.Session != nil && p.Session != session {
		if p.PreviousSession != nil {
			p.PreviousSession.Close() // 关闭更早的会话
		}
		p.PreviousSession = p.Session
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

// GetLastSeen 返回最后活动时间
func (p *Peer) GetLastSeen() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.LastSeen
}

// GetOriginalAddr 返回原始连接地址
func (p *Peer) GetOriginalAddr() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.OriginalAddr
}

// Close 关闭连接并安全清理会话密钥
func (p *Peer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.State = StateDisconnected

	// 安全关闭加密会话，清除密钥材料
	if p.Session != nil {
		p.Session.Close()
		p.Session = nil
	}
	if p.PreviousSession != nil {
		p.PreviousSession.Close()
		p.PreviousSession = nil
	}

	if p.Conn != nil && p.Transport == "tcp" {
		p.Conn.Close()
		p.Conn = nil
	}
}

// SetState 设置连接状态
func (p *Peer) SetState(state ConnState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.State = state
	if state == StateConnected && p.ConnectedAt.IsZero() {
		p.ConnectedAt = time.Now()
	}
}

// GetState 获取连接状态
func (p *Peer) GetState() ConnState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.State
}

// AddBytesSent 增加发送字节统计
func (p *Peer) AddBytesSent(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.BytesSent += n
	p.MessagesSent++
}

// AddBytesReceived 增加接收字节统计
func (p *Peer) AddBytesReceived(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.BytesReceived += n
	p.MessagesRecv++
}

// GetStats 获取连接统计
func (p *Peer) GetStats() PeerStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return PeerStats{
		State:          p.State,
		Transport:      p.Transport,
		LinkMode:       p.LinkMode,
		ConnectedAt:    p.ConnectedAt,
		LastSeen:       p.LastSeen,
		BytesSent:      p.BytesSent,
		BytesReceived:  p.BytesReceived,
		MessagesSent:   p.MessagesSent,
		MessagesRecv:   p.MessagesRecv,
		ConnectLatency: p.ConnectLatency,
	}
}

// PeerStats 节点统计信息
type PeerStats struct {
	State          ConnState
	Transport      string
	LinkMode       string
	ConnectedAt    time.Time
	LastSeen       time.Time
	BytesSent      int64
	BytesReceived  int64
	MessagesSent   int64
	MessagesRecv   int64
	ConnectLatency time.Duration
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

// Decrypt 尝试使用当前或旧会话解密数据
func (p *Peer) Decrypt(payload []byte) ([]byte, error) {
	p.mu.RLock()
	session := p.Session
	prevSession := p.PreviousSession
	p.mu.RUnlock()

	if session == nil {
		return nil, fmt.Errorf("会话未建立")
	}

	plaintext, err := session.Decrypt(payload)
	if err == nil {
		return plaintext, nil
	}

	// 尝试回退到旧会话
	if prevSession != nil {
		if plaintext, err := prevSession.Decrypt(payload); err == nil {
			return plaintext, nil
		}
	}

	return nil, err
}
