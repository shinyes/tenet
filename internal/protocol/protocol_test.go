package protocol

import (
	"testing"
)

func TestBuildHandshakePacket(t *testing.T) {
	msg := []byte("test handshake")
	packet := BuildHandshakePacket(msg)

	if len(packet) != 5+len(msg) {
		t.Errorf("包长度期望 %d，实际 %d", 5+len(msg), len(packet))
	}
	if string(packet[0:4]) != MagicBytes {
		t.Errorf("魔术数期望 %s，实际 %s", MagicBytes, string(packet[0:4]))
	}
	if packet[4] != PacketTypeHandshake {
		t.Errorf("包类型期望 %d，实际 %d", PacketTypeHandshake, packet[4])
	}
}

func TestEncodeTCPFrame(t *testing.T) {
	data := []byte("hello")
	frame := EncodeTCPFrame(data)

	if len(frame) != 2+len(data) {
		t.Errorf("帧长度期望 %d，实际 %d", 2+len(data), len(frame))
	}

	length := DecodeTCPFrameLength(frame)
	if length != uint16(len(data)) {
		t.Errorf("解码长度期望 %d，实际 %d", len(data), length)
	}
}

func TestPutGetTimestamp(t *testing.T) {
	buf := make([]byte, 8)
	ts := int64(1234567890123456789)

	PutTimestamp(buf, ts)
	got := GetTimestamp(buf)

	if got != ts {
		t.Errorf("时间戳期望 %d，实际 %d", ts, got)
	}
}

func TestValidateMagic(t *testing.T) {
	tests := []struct {
		data   []byte
		expect bool
	}{
		{[]byte("TENT1234"), true},
		{[]byte("TENT"), true},
		{[]byte("TEN"), false},
		{[]byte("ABCD"), false},
		{nil, false},
	}

	for _, tt := range tests {
		if got := ValidateMagic(tt.data); got != tt.expect {
			t.Errorf("ValidateMagic(%v) = %v, 期望 %v", tt.data, got, tt.expect)
		}
	}
}

func TestGetPacketType(t *testing.T) {
	packet := []byte{'T', 'E', 'N', 'T', 0x02, 'p', 'a', 'y'}
	if got := GetPacketType(packet); got != 0x02 {
		t.Errorf("GetPacketType = %d, 期望 2", got)
	}

	// 短包
	if got := GetPacketType([]byte{'T', 'E'}); got != 0 {
		t.Errorf("短包 GetPacketType = %d, 期望 0", got)
	}
}

func TestGetPayload(t *testing.T) {
	packet := []byte{'T', 'E', 'N', 'T', 0x02, 'h', 'e', 'l', 'l', 'o'}
	payload := GetPayload(packet)

	if string(payload) != "hello" {
		t.Errorf("GetPayload = %s, 期望 hello", string(payload))
	}

	// 无负载
	if got := GetPayload([]byte{'T', 'E', 'N', 'T', 0x02}); got != nil {
		t.Errorf("无负载包 GetPayload = %v, 期望 nil", got)
	}
}
