package pool

import (
	"testing"
)

func TestGetLargeBuffer(t *testing.T) {
	buf := GetLargeBuffer()
	if buf == nil {
		t.Fatal("GetLargeBuffer 返回 nil")
	}
	if len(*buf) != LargeBufferSize {
		t.Errorf("缓冲区大小期望 %d，实际 %d", LargeBufferSize, len(*buf))
	}
	PutLargeBuffer(buf)
}

func TestGetPacketBuffer(t *testing.T) {
	buf := GetPacketBuffer()
	if buf == nil {
		t.Fatal("GetPacketBuffer 返回 nil")
	}
	if len(*buf) != PacketBufferSize {
		t.Errorf("缓冲区大小期望 %d，实际 %d", PacketBufferSize, len(*buf))
	}
	PutPacketBuffer(buf)
}

func TestNewBuffer(t *testing.T) {
	// 测试小缓冲区
	small := NewBuffer(100)
	if small == nil {
		t.Fatal("NewBuffer(100) 返回 nil")
	}
	if small.Cap() < 100 {
		t.Errorf("缓冲区容量期望 >= 100，实际 %d", small.Cap())
	}
	small.Release()

	// 测试大缓冲区
	large := NewBuffer(10000)
	if large == nil {
		t.Fatal("NewBuffer(10000) 返回 nil")
	}
	if large.Cap() < 10000 {
		t.Errorf("缓冲区容量期望 >= 10000，实际 %d", large.Cap())
	}
	large.Release()
}

func TestBufferSetLen(t *testing.T) {
	buf := NewBuffer(100)
	defer buf.Release()

	buf.SetLen(50)
	if len(buf.Bytes()) != 50 {
		t.Errorf("SetLen(50) 后长度期望 50，实际 %d", len(buf.Bytes()))
	}
}

func TestPutNilBuffer(t *testing.T) {
	// 确保 nil 不会 panic
	PutLargeBuffer(nil)
	PutPacketBuffer(nil)
}

func BenchmarkGetPutLargeBuffer(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := GetLargeBuffer()
		PutLargeBuffer(buf)
	}
}

func BenchmarkGetPutPacketBuffer(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := GetPacketBuffer()
		PutPacketBuffer(buf)
	}
}
