package peer

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/shinyes/tenet/crypto"
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
	ID           string
	Addr         net.Addr     // Generalized to net.Addr
	OriginalAddr string       // 原始连接地址（用于重连）
	Conn         net.Conn     // Active TCP connection, if any
	Transport    string       // "tcp" or "udp"
	LinkMode     string       // "p2p" or "relay"
	RelayTarget  *net.UDPAddr // 通过中继访问的目标地址（发起方使用）
	Session      *crypto.Session

	LastSeen time.Time

	// RemoteChannels 对方订阅的频道列表 (存储 Channel Hash)
	RemoteChannels map[string]struct{}

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
	// sendMu makes data-plane switch and send operation mutually exclusive.
	sendMu sync.Mutex

	consecutiveDecryptFails     int
	consecutiveRehandshakeFails int
	rehandshakeInFlight         bool
	rehandshakeBackoff          time.Duration
	rehandshakeNextAttempt      time.Time
	rehandshakeWindowStart      time.Time
	rehandshakeWindowCount      int
}

// ResetReassembly clears fragment reassembly state.
func (p *Peer) ResetReassembly() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resetReassemblyLocked()
}

// StartReassembly resets and initializes reassembly buffer with the first fragment.
func (p *Peer) StartReassembly(frameData []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ReassemblyBuffer == nil {
		p.ReassemblyBuffer = new(bytes.Buffer)
	} else {
		p.ReassemblyBuffer.Reset()
	}
	_, _ = p.ReassemblyBuffer.Write(frameData)
	p.LastFrameTime = time.Now()
}

// AppendReassembly appends a middle fragment.
// It returns whether append succeeded and whether an overflow was detected.
func (p *Peer) AppendReassembly(frameData []byte, maxSize int) (bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ReassemblyBuffer == nil {
		return false, false
	}
	if p.ReassemblyBuffer.Len()+len(frameData) > maxSize {
		p.resetReassemblyLocked()
		return false, true
	}
	_, _ = p.ReassemblyBuffer.Write(frameData)
	p.LastFrameTime = time.Now()
	return true, false
}

// FinishReassembly appends the last fragment and returns a copy of the complete payload.
// It returns complete payload, whether completion succeeded, and whether overflow happened.
func (p *Peer) FinishReassembly(frameData []byte, maxSize int) ([]byte, bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ReassemblyBuffer == nil {
		return nil, false, false
	}
	if p.ReassemblyBuffer.Len()+len(frameData) > maxSize {
		p.resetReassemblyLocked()
		return nil, false, true
	}
	_, _ = p.ReassemblyBuffer.Write(frameData)

	completeData := make([]byte, p.ReassemblyBuffer.Len())
	copy(completeData, p.ReassemblyBuffer.Bytes())
	p.resetReassemblyLocked()
	return completeData, true, false
}

func (p *Peer) resetReassemblyLocked() {
	if p.ReassemblyBuffer != nil {
		p.ReassemblyBuffer.Reset()
		p.ReassemblyBuffer = nil
	}
	p.LastFrameTime = time.Time{}
}

// UpgradeTransport safely updates the peer connection info
func (p *Peer) UpgradeTransport(addr net.Addr, conn net.Conn, transport string, session *crypto.Session) {
	p.sendMu.Lock()
	defer p.sendMu.Unlock()

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

	// 关闭旧 Session 并清理密钥材料。
	if p.Session != nil && p.Session != session {
		p.Session.Close()
	}

	p.Addr = addr
	p.Conn = conn
	p.Transport = transport
	p.Session = session
	p.LastSeen = time.Now()
	p.consecutiveDecryptFails = 0
	p.consecutiveRehandshakeFails = 0
	p.rehandshakeBackoff = 0
	p.rehandshakeNextAttempt = time.Time{}
	p.rehandshakeWindowStart = time.Time{}
	p.rehandshakeWindowCount = 0
	p.rehandshakeInFlight = false
}

// SetLinkMode 设置链路模式和中继目标
func (p *Peer) SetLinkMode(mode string, relayTarget *net.UDPAddr) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.LinkMode = mode
	p.RelayTarget = relayTarget
}

// AddRemoteChannel 记录对方加入了某个频道 (Hash)
func (p *Peer) AddRemoteChannel(channelHash []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.RemoteChannels == nil {
		p.RemoteChannels = make(map[string]struct{})
	}
	p.RemoteChannels[string(channelHash)] = struct{}{}
}

// RemoveRemoteChannel 记录对方离开了某个频道 (Hash)
func (p *Peer) RemoveRemoteChannel(channelHash []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.RemoteChannels != nil {
		delete(p.RemoteChannels, string(channelHash))
	}
}

// HasRemoteChannel 检查对方是否在某个频道 (Hash)
func (p *Peer) HasRemoteChannel(channelHash []byte) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.RemoteChannels == nil {
		return false
	}
	_, ok := p.RemoteChannels[string(channelHash)]
	return ok
}

// GetTransportInfo returns the current transport and address
func (p *Peer) GetTransportInfo() (string, net.Addr, net.Conn) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Transport, p.Addr, p.Conn
}

// GetSession returns the current crypto session.
func (p *Peer) GetSession() *crypto.Session {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Session
}

// GetLinkMode returns current link mode.
func (p *Peer) GetLinkMode() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.LinkMode
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
	p.sendMu.Lock()
	defer p.sendMu.Unlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	p.State = StateDisconnected

	// 安全关闭加密会话，清除密钥材料
	if p.Session != nil {
		p.Session.Close()
		p.Session = nil
	}

	if p.Conn != nil && p.Transport == "tcp" {
		p.Conn.Close()
		p.Conn = nil
	}
	p.consecutiveDecryptFails = 0
	p.consecutiveRehandshakeFails = 0
	p.rehandshakeBackoff = 0
	p.rehandshakeNextAttempt = time.Time{}
	p.rehandshakeWindowStart = time.Time{}
	p.rehandshakeWindowCount = 0
	p.rehandshakeInFlight = false
}

// LockDataSend serializes application data sending with transport switch.
func (p *Peer) LockDataSend() {
	p.sendMu.Lock()
}

// UnlockDataSend releases the send lock.
func (p *Peer) UnlockDataSend() {
	p.sendMu.Unlock()
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
	p.mu.RUnlock()

	if session == nil {
		return nil, fmt.Errorf("会话未建立")
	}

	return session.Decrypt(payload)
}

// IncDecryptFailures increases consecutive decrypt failures and returns the latest count.
func (p *Peer) IncDecryptFailures() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consecutiveDecryptFails++
	return p.consecutiveDecryptFails
}

// ResetDecryptFailures clears consecutive decrypt failure count.
func (p *Peer) ResetDecryptFailures() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consecutiveDecryptFails = 0
}

// TryBeginRehandshake marks a fast re-handshake as in-flight.
// It returns false when another attempt is already running.
func (p *Peer) TryBeginRehandshake() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rehandshakeInFlight {
		return false
	}
	p.rehandshakeInFlight = true
	return true
}

// EndRehandshake clears the in-flight re-handshake mark.
func (p *Peer) EndRehandshake() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rehandshakeInFlight = false
}

// ReserveRehandshakeAttempt checks backoff/window gates and reserves one attempt.
func (p *Peer) ReserveRehandshakeAttempt(now time.Time, window time.Duration, maxInWindow int) (bool, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.rehandshakeNextAttempt.IsZero() && now.Before(p.rehandshakeNextAttempt) {
		return false, p.rehandshakeNextAttempt.Sub(now)
	}

	if window > 0 {
		if p.rehandshakeWindowStart.IsZero() || now.Sub(p.rehandshakeWindowStart) >= window {
			p.rehandshakeWindowStart = now
			p.rehandshakeWindowCount = 0
		}
		if maxInWindow > 0 && p.rehandshakeWindowCount >= maxInWindow {
			wait := p.rehandshakeWindowStart.Add(window).Sub(now)
			if wait < 0 {
				wait = 0
			}
			return false, wait
		}
	}

	p.rehandshakeWindowCount++
	return true, 0
}

// RecordRehandshakeFailure applies exponential backoff and returns fail count and delay.
func (p *Peer) RecordRehandshakeFailure(now time.Time, baseBackoff, maxBackoff time.Duration) (int, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if baseBackoff <= 0 {
		baseBackoff = time.Second
	}
	if maxBackoff > 0 && maxBackoff < baseBackoff {
		maxBackoff = baseBackoff
	}

	p.consecutiveRehandshakeFails++
	if p.rehandshakeBackoff <= 0 {
		p.rehandshakeBackoff = baseBackoff
	} else {
		p.rehandshakeBackoff *= 2
		if p.rehandshakeBackoff < baseBackoff {
			p.rehandshakeBackoff = baseBackoff
		}
	}
	if maxBackoff > 0 && p.rehandshakeBackoff > maxBackoff {
		p.rehandshakeBackoff = maxBackoff
	}

	p.rehandshakeNextAttempt = now.Add(p.rehandshakeBackoff)
	return p.consecutiveRehandshakeFails, p.rehandshakeBackoff
}

// RecordRehandshakeSuccess clears fail counters and applies a short cooldown.
func (p *Peer) RecordRehandshakeSuccess(now time.Time, cooldown time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.consecutiveRehandshakeFails = 0
	p.rehandshakeBackoff = 0
	if cooldown > 0 {
		p.rehandshakeNextAttempt = now.Add(cooldown)
	} else {
		p.rehandshakeNextAttempt = time.Time{}
	}
}
