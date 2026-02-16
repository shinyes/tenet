package transport

import (
	"net"
	"sync"
	"time"
)

// PunchMagic is the magic number for NAT hole punching packets ("PNCH").
const PunchMagic = 0x504E4348

// Packet is a UDP datagram with source address.
type Packet struct {
	Data []byte
	Addr net.Addr
}

// UDPMux multiplexes one UDP socket into logical packet connections.
type UDPMux struct {
	conn      *net.UDPConn
	kcpConn   *VirtualPacketConn
	punchConn *VirtualPacketConn
	tentConn  *VirtualPacketConn

	// kcpHandlers maps remote addr string -> dedicated KCP packet conn.
	kcpHandlers sync.Map

	closed     chan struct{}
	wg         sync.WaitGroup
	bufferPool sync.Pool
}

// NewUDPMux creates a new UDP multiplexer on top of conn.
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

	mux.kcpConn = newVirtualPacketConn(mux, 128)
	mux.punchConn = newVirtualPacketConn(mux, 16)
	mux.tentConn = newVirtualPacketConn(mux, 128)
	return mux
}

// Start launches the UDP read loop.
func (m *UDPMux) Start() {
	m.wg.Add(1)
	go m.readLoop()
}

func (m *UDPMux) readLoop() {
	defer m.wg.Done()

	for {
		select {
		case <-m.closed:
			return
		default:
		}

		buf := m.bufferPool.Get().([]byte)
		n, addr, err := m.conn.ReadFrom(buf)
		if err != nil {
			m.bufferPool.Put(buf)
			select {
			case <-m.closed:
				return
			default:
				continue
			}
		}

		data := make([]byte, n)
		copy(data, buf[:n])
		m.bufferPool.Put(buf)

		m.routePacket(Packet{Data: data, Addr: addr})
	}
}

func (m *UDPMux) routePacket(packet Packet) {
	if m.isPunchPacket(packet.Data) {
		m.enqueuePacket(m.punchConn, packet)
		return
	}

	if m.isProbePacket(packet.Data) || m.isTentPacket(packet.Data) || m.isRawRelayPacket(packet.Data) {
		m.enqueuePacket(m.tentConn, packet)
		return
	}

	if val, ok := m.kcpHandlers.Load(packet.Addr.String()); ok {
		m.enqueuePacket(val.(*VirtualPacketConn), packet)
		return
	}

	m.enqueuePacket(m.kcpConn, packet)
}

func (m *UDPMux) enqueuePacket(conn *VirtualPacketConn, packet Packet) {
	select {
	case conn.readChan <- packet:
	default:
	}
}

func (m *UDPMux) isPunchPacket(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return data[0] == 0x50 && data[1] == 0x4E && data[2] == 0x43 && data[3] == 0x48
}

func (m *UDPMux) isTentPacket(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return string(data[:4]) == "TENT"
}

func (m *UDPMux) isProbePacket(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return data[0] == 'N' && data[1] == 'A' && data[2] == 'T' && data[3] == 'P'
}

func (m *UDPMux) isRawRelayPacket(data []byte) bool {
	return len(data) > 0 && data[0] == 'R'
}

// RegisterKCPHandler registers a dedicated KCP handler for a remote address.
func (m *UDPMux) RegisterKCPHandler(remoteAddr string) net.PacketConn {
	handler := newVirtualPacketConn(m, 128)
	m.kcpHandlers.Store(remoteAddr, handler)
	return handler
}

// UnregisterKCPHandler removes and closes a dedicated KCP handler.
func (m *UDPMux) UnregisterKCPHandler(remoteAddr string) {
	if val, ok := m.kcpHandlers.LoadAndDelete(remoteAddr); ok {
		handler := val.(*VirtualPacketConn)
		handler.safeClose()
	}
}

// Close closes mux and all virtual packet conns.
func (m *UDPMux) Close() error {
	select {
	case <-m.closed:
		return nil
	default:
		close(m.closed)
	}

	err := m.conn.Close()
	m.wg.Wait()

	m.kcpConn.safeClose()
	m.punchConn.safeClose()
	m.tentConn.safeClose()

	m.kcpHandlers.Range(func(_, value interface{}) bool {
		handler := value.(*VirtualPacketConn)
		handler.safeClose()
		return true
	})

	return err
}

func (m *UDPMux) GetKCPConn() net.PacketConn {
	return m.kcpConn
}

func (m *UDPMux) GetPunchConn() net.PacketConn {
	return m.punchConn
}

func (m *UDPMux) GetTentConn() net.PacketConn {
	return m.tentConn
}

func (m *UDPMux) LocalAddr() net.Addr {
	return m.conn.LocalAddr()
}

// VirtualPacketConn implements net.PacketConn on top of a mux queue.
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

func (v *VirtualPacketConn) safeClose() {
	v.once.Do(func() {
		close(v.readChan)
	})
}

func (v *VirtualPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
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

func (v *VirtualPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	return v.mux.conn.WriteTo(p, addr)
}

func (v *VirtualPacketConn) Close() error {
	return nil
}

func (v *VirtualPacketConn) LocalAddr() net.Addr {
	return v.mux.LocalAddr()
}

func (v *VirtualPacketConn) SetDeadline(t time.Time) error {
	v.deadline = t
	return nil
}

func (v *VirtualPacketConn) SetReadDeadline(t time.Time) error {
	v.deadline = t
	return nil
}

func (v *VirtualPacketConn) SetWriteDeadline(t time.Time) error {
	return v.mux.conn.SetWriteDeadline(t)
}

func (v *VirtualPacketConn) SetReadBuffer(bytes int) error {
	return v.mux.conn.SetReadBuffer(bytes)
}

func (v *VirtualPacketConn) SetWriteBuffer(bytes int) error {
	return v.mux.conn.SetWriteBuffer(bytes)
}

type timeoutError struct{}

func (e timeoutError) Error() string   { return "i/o timeout" }
func (e timeoutError) Timeout() bool   { return true }
func (e timeoutError) Temporary() bool { return true }
