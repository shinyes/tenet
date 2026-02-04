package transport

import (
	"net"
	"sync"
	"time"
)

// PunchMagic 是打洞包的魔术数 "PNCH"
const PunchMagic = 0x504E4348

// Packet 表示一个接收到的 UPD 数据包
type Packet struct {
	Data []byte
	Addr net.Addr
}

// UDPMux 实现 UDP 端口复用
// 它将单个 UDP 连接复用为两个逻辑连接：一个用于 KCP（业务），一个用于打洞（NAT）
type UDPMux struct {
	conn        *net.UDPConn
	kcpConn     *VirtualPacketConn // 默认 KCP 连接 (Listener)
	punchConn   *VirtualPacketConn
	tentConn    *VirtualPacketConn // TENT 协议数据包
	kcpHandlers sync.Map           // map[string]*VirtualPacketConn (特定远程地址的 KCP 连接)
	closed      chan struct{}
	wg          sync.WaitGroup
	bufferPool  sync.Pool
}

// NewUDPMux 创建一个新的 UDP 复用器
func NewUDPMux(conn *net.UDPConn) *UDPMux {
	mux := &UDPMux{
		conn:   conn,
		closed: make(chan struct{}),
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 65535)
			},
		},
	}

	// 创建虚拟连接，设置缓冲区大小为 128 (避免阻塞)
	mux.kcpConn = newVirtualPacketConn(mux, 128)
	mux.punchConn = newVirtualPacketConn(mux, 16)
	mux.tentConn = newVirtualPacketConn(mux, 128)

	return mux
}

// Start 启动读取循环
func (m *UDPMux) Start() {
	m.wg.Add(1)
	go m.readLoop()
}

// readLoop 读取 UDP 数据包并分发
func (m *UDPMux) readLoop() {
	defer m.wg.Done()

	for {
		select {
		case <-m.closed:
			return
		default:
		}

		// 从池中获取缓冲区
		buf := m.bufferPool.Get().([]byte)

		// 读取数据
		n, addr, err := m.conn.ReadFrom(buf)
		if err != nil {
			m.bufferPool.Put(buf)
			// 检查是否已关闭
			select {
			case <-m.closed:
				return
			default:
				// 临时错误处理...
				continue
			}
		}

		// 复制数据
		data := make([]byte, n)
		copy(data, buf[:n])
		m.bufferPool.Put(buf)

		packet := Packet{
			Data: data,
			Addr: addr,
		}

		// 1. 打洞包检查
		if m.isPunchPacket(data) {
			select {
			case m.punchConn.readChan <- packet:
			default:
			}
			continue
		}

		// 2. TENT 协议包检查
		// TENT packets go to tentConn
		if m.isTentPacket(data) {
			select {
			case m.tentConn.readChan <- packet:
			default:
			}
			continue
		}

		// 3. KCP 包分发
		// 检查是否有特定地址的处理器 (用于 Dial 出去的连接)
		if val, ok := m.kcpHandlers.Load(addr.String()); ok {
			handler := val.(*VirtualPacketConn)
			select {
			case handler.readChan <- packet:
			default:
				// 缓冲区满，丢弃
			}
			continue
		}

		// 4. 默认 KCP 处理器 (用于 Listener)
		select {
		case m.kcpConn.readChan <- packet:
		default:
			// 缓冲区满，丢弃
		}
	}
}

// isPunchPacket 检查是否为打洞包
func (m *UDPMux) isPunchPacket(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// 检查前4个字节是否为 "PNCH"
	// 0x50, 0x4E, 0x43, 0x48
	return data[0] == 0x50 && data[1] == 0x4E && data[2] == 0x43 && data[3] == 0x48
}

// isTentPacket 检查是否为 TENT 协议包
func (m *UDPMux) isTentPacket(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// 检查前4个字节是否为 "TENT"
	return string(data[:4]) == "TENT"
}

// RegisterKCPHandler 为特定远程地址注册 KCP 处理器
// 用于主动 Dial 连接时，将来自该地址的包分离出来，避免被 Listener 抢占
func (m *UDPMux) RegisterKCPHandler(remoteAddr string) net.PacketConn {
	handler := newVirtualPacketConn(m, 128)
	m.kcpHandlers.Store(remoteAddr, handler)
	return handler
}

// UnregisterKCPHandler 注销特定远程地址的 KCP 处理器
func (m *UDPMux) UnregisterKCPHandler(remoteAddr string) {
	if val, ok := m.kcpHandlers.LoadAndDelete(remoteAddr); ok {
		handler := val.(*VirtualPacketConn)
		handler.safeClose()
	}
}

// Close 关闭复用器
func (m *UDPMux) Close() error {
	select {
	case <-m.closed:
		return nil
	default:
		close(m.closed)
	}

	err := m.conn.Close()
	m.wg.Wait()

	// 关闭虚拟连接的通道
	m.kcpConn.safeClose()
	m.punchConn.safeClose()
	m.tentConn.safeClose()

	// 关闭所有特定处理器
	m.kcpHandlers.Range(func(key, value interface{}) bool {
		handler := value.(*VirtualPacketConn)
		handler.safeClose()
		return true
	})

	return err
}

// GetKCPConn 获取默认的 KCP 虚拟连接 (用于 Listener)
func (m *UDPMux) GetKCPConn() net.PacketConn {
	return m.kcpConn
}

// GetPunchConn 获取用于打洞的虚拟连接
func (m *UDPMux) GetPunchConn() net.PacketConn {
	return m.punchConn
}

// GetTentConn 获取用于 TENT 协议的虚拟连接
func (m *UDPMux) GetTentConn() net.PacketConn {
	return m.tentConn
}

// LocalAddr 返回本地地址
func (m *UDPMux) LocalAddr() net.Addr {
	return m.conn.LocalAddr()
}

// VirtualPacketConn 实现 net.PacketConn 接口
type VirtualPacketConn struct {
	mux      *UDPMux
	readChan chan Packet
	deadline time.Time
	once     sync.Once
}

func newVirtualPacketConn(mux *UDPMux, bufferSize int) *VirtualPacketConn {
	return &VirtualPacketConn{
		mux:      mux,
		readChan: make(chan Packet, bufferSize),
	}
}

// safeClose 安全关闭读取通道
func (v *VirtualPacketConn) safeClose() {
	v.once.Do(func() {
		close(v.readChan)
	})
}

// ReadFrom 从通道读取数据
func (v *VirtualPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	// 检查 deadline
	if !v.deadline.IsZero() && time.Now().After(v.deadline) {
		return 0, nil, timeoutError{}
	}

	var timeout <-chan time.Time
	if !v.deadline.IsZero() {
		timeout = time.After(time.Until(v.deadline))
	}

	select {
	case packet, ok := <-v.readChan:
		if !ok {
			return 0, nil, net.ErrClosed
		}
		n = copy(p, packet.Data)
		return n, packet.Addr, nil
	case <-timeout:
		return 0, nil, timeoutError{}
	case <-v.mux.closed:
		return 0, nil, net.ErrClosed
	}
}

// WriteTo 直接写入底层的 UDP 连接
func (v *VirtualPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	return v.mux.conn.WriteTo(p, addr)
}

// Close 不做任何事，真正的关闭由 Mux 控制
func (v *VirtualPacketConn) Close() error {
	return nil
}

// LocalAddr 返回 Mux 的地址
func (v *VirtualPacketConn) LocalAddr() net.Addr {
	return v.mux.LocalAddr()
}

// SetDeadline 设置读写超时
func (v *VirtualPacketConn) SetDeadline(t time.Time) error {
	v.deadline = t
	return nil
}

// SetReadDeadline 设置读超时
func (v *VirtualPacketConn) SetReadDeadline(t time.Time) error {
	v.deadline = t
	return nil
}

// SetWriteDeadline 设置写超时
func (v *VirtualPacketConn) SetWriteDeadline(t time.Time) error {
	// 写操作直接调用底层 WriteTo，通常不阻塞（除非系统缓冲区满）
	// 这里可以简单忽略或传递给底层
	return v.mux.conn.SetWriteDeadline(t)
}

// SetReadBuffer 设置读取缓冲区大小
func (v *VirtualPacketConn) SetReadBuffer(bytes int) error {
	return v.mux.conn.SetReadBuffer(bytes)
}

// SetWriteBuffer 设置写入缓冲区大小
func (v *VirtualPacketConn) SetWriteBuffer(bytes int) error {
	return v.mux.conn.SetWriteBuffer(bytes)
}

// timeoutError 实现 net.Error 接口
type timeoutError struct{}

func (e timeoutError) Error() string   { return "i/o timeout" }
func (e timeoutError) Timeout() bool   { return true }
func (e timeoutError) Temporary() bool { return true }
