package nat

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// RelayInfo 中继节点信息
type RelayInfo struct {
	ID        string
	Addr      *net.UDPAddr
	Latency   time.Duration
	Bandwidth int64 // bytes per second
	LastCheck time.Time
}

// RelayMetrics 中继节点指标（用于选择最优中继）
type RelayMetrics struct {
	Latency     time.Duration
	PacketLoss  float64
	Bandwidth   int64
	Reliability float64 // 0-1
}

// Score 计算中继评分（越高越好）
func (m *RelayMetrics) Score() float64 {
	// 综合评分公式：
	// - 延迟越低越好（权重40%）
	// - 带宽越高越好（权重30%）
	// - 可靠性越高越好（权重30%）
	latencyScore := 1000.0 / (float64(m.Latency.Milliseconds()) + 1.0)
	bandwidthScore := float64(m.Bandwidth) / 1000000.0 // Mbps
	reliabilityScore := m.Reliability * 100.0

	return latencyScore*0.4 + bandwidthScore*0.3 + reliabilityScore*0.3
}

// RelayManager 中继管理器
type RelayManager struct {
	conn          *net.UDPConn
	relays        map[string]*RelayInfo
	relayMetrics  map[string]*RelayMetrics
	activeRelays  map[string]bool // 活跃的中继会话
	mu            sync.RWMutex
	probeInterval time.Duration
}

// NewRelayManager 创建中继管理器
func NewRelayManager(conn *net.UDPConn) *RelayManager {
	return &RelayManager{
		conn:          conn,
		relays:        make(map[string]*RelayInfo),
		relayMetrics:  make(map[string]*RelayMetrics),
		activeRelays:  make(map[string]bool),
		probeInterval: 30 * time.Second,
	}
}

// AddRelay 添加中继节点
func (rm *RelayManager) AddRelay(id string, addr *net.UDPAddr) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.relays[id] = &RelayInfo{
		ID:        id,
		Addr:      addr,
		LastCheck: time.Now(),
	}
}

// RemoveRelay 移除中继节点
func (rm *RelayManager) RemoveRelay(id string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	delete(rm.relays, id)
	delete(rm.relayMetrics, id)
	delete(rm.activeRelays, id)
}

// SelectBestRelay 选择最优中继节点
// 优先选择：低延迟 + 高带宽 + 高可靠性
func (rm *RelayManager) SelectBestRelay() (*RelayInfo, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if len(rm.relays) == 0 {
		return nil, fmt.Errorf("没有可用中继节点")
	}

	var bestRelay *RelayInfo
	var bestScore float64 = -1

	for id, relay := range rm.relays {
		metrics, ok := rm.relayMetrics[id]
		if !ok {
			// 没有指标的节点给默认评分
			metrics = &RelayMetrics{
				Latency:     100 * time.Millisecond,
				Bandwidth:   1000000,
				Reliability: 0.5,
			}
		}

		score := metrics.Score()
		if score > bestScore {
			bestScore = score
			bestRelay = relay
		}
	}

	if bestRelay == nil {
		return nil, fmt.Errorf("无法选择中继节点")
	}

	return bestRelay, nil
}

// ProbeRelay 探测中继节点质量
func (rm *RelayManager) ProbeRelay(relay *RelayInfo) (*RelayMetrics, error) {
	startTime := time.Now()

	// 发送探测包
	probePacket := []byte{0x50, 0x52, 0x4F, 0x42, 0x45} // "PROBE"
	// Prefer a dedicated socket to avoid interfering with shared reads
	if probeConn, dialErr := net.DialUDP("udp", nil, relay.Addr); dialErr == nil {
		defer probeConn.Close()
		probeConn.SetDeadline(time.Now().Add(5 * time.Second))

		if _, err := probeConn.Write(probePacket); err != nil {
			return nil, fmt.Errorf("发送探测包失败: %w", err)
		}

		buf := make([]byte, 64)
		for {
			n, readErr := probeConn.Read(buf)
			if readErr != nil {
				return nil, fmt.Errorf("接收探测响应失败: %w", readErr)
			}
			if n < 5 || string(buf[:5]) != "PROBE" {
				continue
			}
			break
		}
	} else {
		_, err := rm.conn.WriteToUDP(probePacket, relay.Addr)
		if err != nil {
			return nil, fmt.Errorf("发送探测包失败: %w", err)
		}

		// 等待响应
		rm.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer rm.conn.SetReadDeadline(time.Time{})

		buf := make([]byte, 64)
		for {
			n, fromAddr, readErr := rm.conn.ReadFromUDP(buf)
			if readErr != nil {
				return nil, fmt.Errorf("接收探测响应失败: %w", readErr)
			}
			if fromAddr == nil || !fromAddr.IP.Equal(relay.Addr.IP) || fromAddr.Port != relay.Addr.Port {
				continue
			}
			if n < 5 || string(buf[:5]) != "PROBE" {
				continue
			}
			break
		}
	}

	latency := time.Since(startTime) / 2 // 单程延迟

	metrics := &RelayMetrics{
		Latency:     latency,
		Bandwidth:   1000000, // 默认1Mbps
		Reliability: 1.0,
	}

	rm.mu.Lock()
	rm.relayMetrics[relay.ID] = metrics
	relay.Latency = latency
	relay.LastCheck = time.Now()
	rm.mu.Unlock()

	return metrics, nil
}

// ProbeAllRelays 探测所有中继节点
func (rm *RelayManager) ProbeAllRelays() {
	rm.mu.RLock()
	relays := make([]*RelayInfo, 0, len(rm.relays))
	for _, r := range rm.relays {
		relays = append(relays, r)
	}
	rm.mu.RUnlock()

	for _, relay := range relays {
		rm.ProbeRelay(relay)
	}
}

// RelayPacket 中继数据包格式
type RelayPacket struct {
	SrcID [16]byte // 源节点ID
	DstID [16]byte // 目标节点ID
	Data  []byte   // 原始数据
}

// EncodeRelayPacket 编码中继包
func EncodeRelayPacket(srcID, dstID [16]byte, data []byte) []byte {
	packet := make([]byte, 1+32+len(data))
	packet[0] = 0x52 // 'R' for Relay
	copy(packet[1:17], srcID[:])
	copy(packet[17:33], dstID[:])
	copy(packet[33:], data)
	return packet
}

// DecodeRelayPacket 解码中继包
func DecodeRelayPacket(packet []byte) (*RelayPacket, error) {
	if len(packet) < 33 || packet[0] != 0x52 {
		return nil, fmt.Errorf("无效的中继包")
	}

	rp := &RelayPacket{
		Data: make([]byte, len(packet)-33),
	}
	copy(rp.SrcID[:], packet[1:17])
	copy(rp.DstID[:], packet[17:33])
	copy(rp.Data, packet[33:])

	return rp, nil
}

// RelayThrough 通过指定节点中继数据
func (rm *RelayManager) RelayThrough(relay *RelayInfo, srcID, dstID [16]byte, data []byte) error {
	packet := EncodeRelayPacket(srcID, dstID, data)
	_, err := rm.conn.WriteToUDP(packet, relay.Addr)
	return err
}
