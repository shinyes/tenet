// Package pool 提供高效的缓冲池实现，减少 GC 压力
package pool

import (
	"sync"
)

// 缓冲区大小常量
const (
	// LargeBufferSize 大缓冲区大小 (64KB)
	// 用于 UDP 接收和 TCP 帧读取
	LargeBufferSize = 65535

	// PacketBufferSize 通用数据包缓冲区大小 (MTU 大小)
	PacketBufferSize = 1500
)

// 缓冲池定义
var (
	// largeBufferPool 大缓冲池 (64KB)
	// 用于 UDP 接收缓冲和 TCP 帧读取
	largeBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, LargeBufferSize)
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

// GetLargeBuffer 从大缓冲池获取缓冲区 (64KB)
// 用于 UDP 和 TCP 数据读取，使用完毕后应调用 PutLargeBuffer 归还
func GetLargeBuffer() *[]byte {
	return largeBufferPool.Get().(*[]byte)
}

// PutLargeBuffer 归还大缓冲区到池
func PutLargeBuffer(buf *[]byte) {
	if buf == nil {
		return
	}
	// 重置长度但保留容量
	*buf = (*buf)[:cap(*buf)]
	largeBufferPool.Put(buf)
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

	if size <= PacketBufferSize {
		pool = &packetPool
		buf = packetPool.Get().(*[]byte)
	} else {
		pool = &largeBufferPool
		buf = largeBufferPool.Get().(*[]byte)
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
