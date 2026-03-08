// Package protocol 提供协议编解码工具函数
package protocol

import (
	"encoding/binary"
	"math"
)

// MagicBytes 协议魔术数
const MagicBytes = "TENT"

// 包类型常量
const (
	PacketTypeHandshake     = 0x01 // 握手
	PacketTypeData          = 0x02 // 加密数据
	PacketTypeRelay         = 0x03 // 中继封装
	PacketTypeDiscoveryReq  = 0x04 // 节点发现请求
	PacketTypeDiscoveryResp = 0x05 // 节点发现响应
	PacketTypeHeartbeat     = 0x06 // 心跳请求
	PacketTypeHeartbeatAck  = 0x07 // 心跳响应
)

// 中继模式
const (
	RelayModeForward = 0x01 // 请求转发
	RelayModeTarget  = 0x02 // 目标侧接收
)

// BuildHandshakePacket 构造握手包
func BuildHandshakePacket(msg []byte) []byte {
	packet := make([]byte, 5+len(msg))
	copy(packet[0:4], []byte(MagicBytes))
	packet[4] = PacketTypeHandshake
	copy(packet[5:], msg)
	return packet
}

// BuildDataPacket 构造数据包
func BuildDataPacket(payload []byte) []byte {
	packet := make([]byte, 5+len(payload))
	copy(packet[0:4], []byte(MagicBytes))
	packet[4] = PacketTypeData
	copy(packet[5:], payload)
	return packet
}

// BuildHeartbeatPacket 构造心跳包
func BuildHeartbeatPacket(timestamp int64) []byte {
	packet := make([]byte, 13)
	copy(packet[0:4], []byte(MagicBytes))
	packet[4] = PacketTypeHeartbeat
	PutTimestamp(packet[5:], timestamp)
	return packet
}

// BuildHeartbeatAckPacket 构造心跳响应包
func BuildHeartbeatAckPacket(timestamp int64) []byte {
	packet := make([]byte, 13)
	copy(packet[0:4], []byte(MagicBytes))
	packet[4] = PacketTypeHeartbeatAck
	PutTimestamp(packet[5:], timestamp)
	return packet
}

// EncodeTCPFrame 编码 TCP 帧（添加长度前缀）
func EncodeTCPFrame(packet []byte) []byte {
	packetLen := len(packet)
	if packetLen > math.MaxUint16 {
		return nil
	}
	length := uint16(packetLen)
	frame := make([]byte, 2+packetLen)
	binary.BigEndian.PutUint16(frame[:2], length)
	copy(frame[2:], packet)
	return frame
}

// DecodeTCPFrameLength 解码 TCP 帧长度（从前 2 字节）
func DecodeTCPFrameLength(header []byte) uint16 {
	if len(header) < 2 {
		return 0
	}
	return uint16(header[0])<<8 | uint16(header[1])
}

// PutTimestamp 将时间戳写入字节数组（小端序）
func PutTimestamp(buf []byte, timestamp int64) {
	if len(buf) < 8 {
		return
	}
	if timestamp < 0 {
		timestamp = 0
	}
	binary.LittleEndian.PutUint64(buf[:8], uint64(timestamp))
}

// GetTimestamp 从字节数组读取时间戳（小端序）
func GetTimestamp(buf []byte) int64 {
	if len(buf) < 8 {
		return 0
	}
	timestamp := binary.LittleEndian.Uint64(buf[:8])
	if timestamp > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(timestamp)
}

// ValidateMagic 验证魔术数
func ValidateMagic(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return string(data[0:4]) == MagicBytes
}

// GetPacketType 获取包类型
func GetPacketType(data []byte) byte {
	if len(data) < 5 {
		return 0
	}
	return data[4]
}

// GetPayload 获取包负载
func GetPayload(data []byte) []byte {
	if len(data) <= 5 {
		return nil
	}
	return data[5:]
}
