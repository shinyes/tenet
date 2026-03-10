package node

import (
	"fmt"
	"net"
	"sync"
	"time"

	tenetlog "github.com/shinyes/tenet/log"
	"github.com/xtaci/kcp-go/v5"
)

// KCPConfig KCP 配置参数
type KCPConfig struct {
	// NoDelay 模式: 0=关闭, 1=开启
	NoDelay int
	// Interval 内部更新间隔(ms)
	Interval int
	// Resend 快速重传触发次数，0=关闭
	Resend int
	// NC 拥塞控制：0=正常, 1=关闭
	NC int
	// SndWnd 发送窗口大小
	SndWnd int
	// RcvWnd 接收窗口大小
	RcvWnd int
	// MTU 最大传输单元
	MTU int
	// StreamMode 流模式
	StreamMode bool
}

// DefaultKCPConfig 返回平衡模式的 KCP 配置
func DefaultKCPConfig() *KCPConfig {
	return &KCPConfig{
		NoDelay:    0,    // 关闭 nodelay，节省带宽
		Interval:   30,   // 30ms 更新间隔
		Resend:     2,    // 2 次 ACK 后快速重传
		NC:         1,    // 关闭拥塞控制（P2P 场景）
		SndWnd:     64,   // 发送窗口
		RcvWnd:     64,   // 接收窗口
		MTU:        1350, // MTU（留余量给加密头）
		StreamMode: true, // 流模式
	}
}

// KCPSession 封装 KCP 会话
type KCPSession struct {
	session  *kcp.UDPSession
	peerAddr *net.UDPAddr
	config   *KCPConfig
	mu       sync.RWMutex
	closed   bool
}

// KCPManager 管理所有 KCP 会话
type KCPManager struct {
	listener *kcp.Listener
	sessions map[string]*KCPSession // peerAddr -> session
	config   *KCPConfig
	logger   tenetlog.Logger
	mu       sync.RWMutex
	closing  chan struct{}

	// 新连接回调
	onNewSession func(session *KCPSession, remoteAddr *net.UDPAddr)
}

// NewKCPManager 创建 KCP 管理器
func NewKCPManager(config *KCPConfig, logger ...tenetlog.Logger) *KCPManager {
	if config == nil {
		config = DefaultKCPConfig()
	}
	l := tenetlog.Nop()
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	}
	return &KCPManager{
		sessions: make(map[string]*KCPSession),
		config:   config,
		logger:   l,
		closing:  make(chan struct{}),
	}
}

// Listen 在指定端口监听 KCP 连接
func (m *KCPManager) Listen(port int) error {
	// 使用 [::] 以支持双栈 (IPv4 + IPv6)
	addr := fmt.Sprintf("[::]:%d", port)

	// 使用无 FEC 的 KCP（我们已经有 Noise 加密）
	listener, err := kcp.ListenWithOptions(addr, nil, 0, 0)
	if err != nil {
		return err
	}

	m.listener = listener

	// 应用配置
	if err := listener.SetReadBuffer(4 * 1024 * 1024); err != nil {
		listener.Close()
		return err
	}
	if err := listener.SetWriteBuffer(4 * 1024 * 1024); err != nil {
		listener.Close()
		return err
	}

	return nil
}

// ListenOnConn 在已有的 UDP 连接上监听 KCP
// 这允许与现有的 UDP 握手共用同一端口
func (m *KCPManager) ListenOnConn(conn net.PacketConn) error {
	listener, err := kcp.ServeConn(nil, 0, 0, conn)
	if err != nil {
		m.logger.Error("KCP ServeConn failed: %v", err)
		return err
	}

	m.listener = listener

	// 应用缓冲区配置
	if err := listener.SetReadBuffer(4 * 1024 * 1024); err != nil {
		m.logger.Warn("KCP SetReadBuffer failed: %v", err)
	}
	if err := listener.SetWriteBuffer(4 * 1024 * 1024); err != nil {
		m.logger.Warn("KCP SetWriteBuffer failed: %v", err)
	}

	return nil
}

// Accept 接受新的 KCP 连接
func (m *KCPManager) Accept() (*KCPSession, error) {
	if m.listener == nil {
		return nil, net.ErrClosed
	}

	session, err := m.listener.AcceptKCP()
	if err != nil {
		return nil, err
	}

	// 应用平衡模式配置
	m.configureSession(session)

	remoteAddr := session.RemoteAddr().(*net.UDPAddr)
	kcpSession := &KCPSession{
		session:  session,
		peerAddr: remoteAddr,
		config:   m.config,
	}

	m.mu.Lock()
	m.sessions[remoteAddr.String()] = kcpSession
	m.mu.Unlock()

	return kcpSession, nil
}

// Dial 主动连接远程 KCP 节点
func (m *KCPManager) Dial(remoteAddr string) (*KCPSession, error) {
	// 使用无 FEC 的 KCP
	session, err := kcp.DialWithOptions(remoteAddr, nil, 0, 0)
	if err != nil {
		return nil, err
	}

	// 应用平衡模式配置
	m.configureSession(session)

	peerAddr := session.RemoteAddr().(*net.UDPAddr)
	kcpSession := &KCPSession{
		session:  session,
		peerAddr: peerAddr,
		config:   m.config,
	}

	m.mu.Lock()
	m.sessions[peerAddr.String()] = kcpSession
	m.mu.Unlock()

	return kcpSession, nil
}

// DialWithLocalAddr 使用指定本地地址连接远程 KCP 节点
func (m *KCPManager) DialWithLocalAddr(localAddr, remoteAddr string) (*KCPSession, error) {
	// 解析地址
	laddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		return nil, err
	}
	raddr, err := net.ResolveUDPAddr("udp", remoteAddr)
	if err != nil {
		return nil, err
	}

	// 创建本地 UDP 连接
	conn, err := net.DialUDP("udp", laddr, raddr)
	if err != nil {
		return nil, err
	}

	// 在连接上创建 KCP 会话
	session, err := kcp.NewConn(remoteAddr, nil, 0, 0, &connWrapper{conn})
	if err != nil {
		conn.Close()
		return nil, err
	}

	// 应用平衡模式配置
	m.configureSession(session)

	kcpSession := &KCPSession{
		session:  session,
		peerAddr: raddr,
		config:   m.config,
	}

	m.mu.Lock()
	m.sessions[raddr.String()] = kcpSession
	m.mu.Unlock()

	return kcpSession, nil
}

// DialWithConn 使用提供的 PacketConn 连接远程 KCP 节点
func (m *KCPManager) DialWithConn(remoteAddr string, conn net.PacketConn) (*KCPSession, error) {
	raddr, err := net.ResolveUDPAddr("udp", remoteAddr)
	if err != nil {
		return nil, err
	}

	// 使用提供的连接创建 KCP 会话
	session, err := kcp.NewConn(remoteAddr, nil, 0, 0, conn)
	if err != nil {
		return nil, err
	}

	// 应用平衡模式配置
	m.configureSession(session)

	kcpSession := &KCPSession{
		session:  session,
		peerAddr: raddr,
		config:   m.config,
	}

	m.mu.Lock()
	m.sessions[raddr.String()] = kcpSession
	m.mu.Unlock()

	return kcpSession, nil
}

// configureSession 应用 KCP 配置到会话
func (m *KCPManager) configureSession(session *kcp.UDPSession) {
	cfg := m.config

	// 平衡模式配置
	session.SetNoDelay(cfg.NoDelay, cfg.Interval, cfg.Resend, cfg.NC)

	// 窗口大小
	session.SetWindowSize(cfg.SndWnd, cfg.RcvWnd)

	// MTU
	session.SetMtu(cfg.MTU)

	// 流模式
	session.SetStreamMode(cfg.StreamMode)

	// 设置读写超时
	session.SetReadDeadline(time.Time{})  // 无超时
	session.SetWriteDeadline(time.Time{}) // 无超时
}

// GetSession 获取指定地址的 KCP 会话
func (m *KCPManager) GetSession(addr string) (*KCPSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[addr]
	return session, ok
}

// RemoveSession 移除 KCP 会话
func (m *KCPManager) RemoveSession(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, ok := m.sessions[addr]; ok {
		session.Close()
		delete(m.sessions, addr)
	}
}

// Close 关闭 KCP 管理器
func (m *KCPManager) Close() error {
	close(m.closing)

	m.mu.Lock()
	defer m.mu.Unlock()

	// 关闭所有会话
	for addr, session := range m.sessions {
		session.Close()
		delete(m.sessions, addr)
	}

	// 关闭监听器
	if m.listener != nil {
		return m.listener.Close()
	}

	return nil
}

// LocalAddr 返回本地监听地址
func (m *KCPManager) LocalAddr() net.Addr {
	if m.listener != nil {
		return m.listener.Addr()
	}
	return nil
}

// ---- KCPSession 方法 ----

// Read 从 KCP 会话读取数据
func (s *KCPSession) Read(b []byte) (int, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return 0, net.ErrClosed
	}
	session := s.session
	s.mu.RUnlock()

	return session.Read(b)
}

// Write 向 KCP 会话写入数据
func (s *KCPSession) Write(b []byte) (int, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return 0, net.ErrClosed
	}
	session := s.session
	s.mu.RUnlock()

	return session.Write(b)
}

// Close 关闭 KCP 会话
func (s *KCPSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.session != nil {
		return s.session.Close()
	}
	return nil
}

// RemoteAddr 返回远程地址
func (s *KCPSession) RemoteAddr() *net.UDPAddr {
	return s.peerAddr
}

// LocalAddr 返回本地地址
func (s *KCPSession) LocalAddr() net.Addr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.session != nil {
		return s.session.LocalAddr()
	}
	return nil
}

// SetReadDeadline 设置读取超时
func (s *KCPSession) SetReadDeadline(t time.Time) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.session != nil {
		return s.session.SetReadDeadline(t)
	}
	return nil
}

// SetWriteDeadline 设置写入超时
func (s *KCPSession) SetWriteDeadline(t time.Time) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.session != nil {
		return s.session.SetWriteDeadline(t)
	}
	return nil
}

// GetRTT 获取往返时延估算（毫秒）
// 注意：kcp-go v5 不直接暴露 RTT，返回 0
func (s *KCPSession) GetRTT() int32 {
	// kcp-go v5 没有直接暴露 GetRTT 方法
	// 如需 RTT 信息，可以通过应用层 ping 测量
	return 0
}

// GetRTTVar 获取 RTT 方差
// 注意：kcp-go v5 不直接暴露 RTTVar，返回 0
func (s *KCPSession) GetRTTVar() int32 {
	return 0
}

// connWrapper 将 *net.UDPConn 包装为 net.PacketConn
type connWrapper struct {
	conn *net.UDPConn
}

func (w *connWrapper) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, addr, err = w.conn.ReadFromUDP(p)
	return
}

func (w *connWrapper) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	udpAddr := addr.(*net.UDPAddr)
	return w.conn.WriteToUDP(p, udpAddr)
}

func (w *connWrapper) Close() error {
	return w.conn.Close()
}

func (w *connWrapper) LocalAddr() net.Addr {
	return w.conn.LocalAddr()
}

func (w *connWrapper) SetDeadline(t time.Time) error {
	return w.conn.SetDeadline(t)
}

func (w *connWrapper) SetReadDeadline(t time.Time) error {
	return w.conn.SetReadDeadline(t)
}

func (w *connWrapper) SetWriteDeadline(t time.Time) error {
	return w.conn.SetWriteDeadline(t)
}
