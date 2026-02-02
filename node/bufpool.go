package node

import (
	"sync"
)

// 缓冲区大小常量
const (
	// UDPBufferSize UDP 读取缓冲区大小 (64KB)
	UDPBufferSize = 65535

	// TCPFrameSize TCP 帧最大大小 (32KB)
	TCPFrameSize = 32 * 1024

	// PacketBufferSize 通用数据包缓冲区大小 (MTU 大小)
	PacketBufferSize = 1500
)

// 缓冲池定义
var (
	// udpBufferPool UDP 大缓冲池 (65535 bytes)
	// 用于 handleRead 中的 UDP 接收缓冲
	udpBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, UDPBufferSize)
			return &buf
		},
	}

	// tcpFramePool TCP 帧池 (32KB)
	// 用于 handleTCP 中的帧读取
	tcpFramePool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, TCPFrameSize)
			return &buf
		},
	}

	// packetPool 通用小包池 (1500 bytes - MTU)
	// 用于握手包、心跳包、数据包等
	packetPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, PacketBufferSize)
			return &buf
		},
	}
)

// GetUDPBuffer 从 UDP 缓冲池获取缓冲区
// 返回 *[]byte，使用完毕后应调用 PutUDPBuffer 归还
func GetUDPBuffer() *[]byte {
	return udpBufferPool.Get().(*[]byte)
}

// PutUDPBuffer 归还 UDP 缓冲区到池
func PutUDPBuffer(buf *[]byte) {
	if buf == nil {
		return
	}
	// 重置长度但保留容量
	*buf = (*buf)[:cap(*buf)]
	udpBufferPool.Put(buf)
}

// GetTCPFrameBuffer 从 TCP 帧池获取缓冲区
func GetTCPFrameBuffer() *[]byte {
	return tcpFramePool.Get().(*[]byte)
}

// PutTCPFrameBuffer 归还 TCP 帧缓冲区到池
func PutTCPFrameBuffer(buf *[]byte) {
	if buf == nil {
		return
	}
	*buf = (*buf)[:cap(*buf)]
	tcpFramePool.Put(buf)
}

// GetPacketBuffer 从通用包池获取缓冲区
func GetPacketBuffer() *[]byte {
	return packetPool.Get().(*[]byte)
}

// PutPacketBuffer 归还通用包缓冲区到池
func PutPacketBuffer(buf *[]byte) {
	if buf == nil {
		return
	}
	*buf = (*buf)[:cap(*buf)]
	packetPool.Put(buf)
}

// Buffer 是一个可重用的缓冲区包装器
// 提供更安全的缓冲区管理
type Buffer struct {
	data *[]byte
	pool *sync.Pool
	len  int
}

// NewBuffer 从指定池创建缓冲区
func NewBuffer(size int) *Buffer {
	var pool *sync.Pool
	var buf *[]byte

	switch {
	case size <= PacketBufferSize:
		pool = &packetPool
		buf = packetPool.Get().(*[]byte)
	case size <= TCPFrameSize:
		pool = &tcpFramePool
		buf = tcpFramePool.Get().(*[]byte)
	default:
		pool = &udpBufferPool
		buf = udpBufferPool.Get().(*[]byte)
	}

	return &Buffer{
		data: buf,
		pool: pool,
		len:  size,
	}
}

// Bytes 返回缓冲区数据（按实际使用长度切片）
func (b *Buffer) Bytes() []byte {
	if b.data == nil {
		return nil
	}
	if b.len > len(*b.data) {
		return *b.data
	}
	return (*b.data)[:b.len]
}

// SetLen 设置有效数据长度
func (b *Buffer) SetLen(n int) {
	if n > cap(*b.data) {
		n = cap(*b.data)
	}
	b.len = n
}

// Cap 返回缓冲区容量
func (b *Buffer) Cap() int {
	if b.data == nil {
		return 0
	}
	return cap(*b.data)
}

// Release 归还缓冲区到池
func (b *Buffer) Release() {
	if b.data == nil || b.pool == nil {
		return
	}
	*b.data = (*b.data)[:cap(*b.data)]
	b.pool.Put(b.data)
	b.data = nil
	b.pool = nil
}

// --- 公共辅助函数 ---

// BuildHandshakePacket 构造握手包
func BuildHandshakePacket(msg []byte) []byte {
	packet := make([]byte, 5+len(msg))
	copy(packet[0:4], []byte("TENT"))
	packet[4] = 0x01 // PacketTypeHandshake
	copy(packet[5:], msg)
	return packet
}

// EncodeTCPFrame 编码 TCP 帧（添加长度前缀）
func EncodeTCPFrame(packet []byte) []byte {
	length := uint16(len(packet))
	frame := make([]byte, 2+len(packet))
	frame[0] = byte(length >> 8)
	frame[1] = byte(length)
	copy(frame[2:], packet)
	return frame
}

// PutTimestamp 将时间戳写入字节数组（小端序）
func PutTimestamp(buf []byte, timestamp int64) {
	buf[0] = byte(timestamp)
	buf[1] = byte(timestamp >> 8)
	buf[2] = byte(timestamp >> 16)
	buf[3] = byte(timestamp >> 24)
	buf[4] = byte(timestamp >> 32)
	buf[5] = byte(timestamp >> 40)
	buf[6] = byte(timestamp >> 48)
	buf[7] = byte(timestamp >> 56)
}
