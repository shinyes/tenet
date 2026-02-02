// Package metrics 提供 tenet P2P 网络的监控与统计。
package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Collector 指标收集器
type Collector struct {
	// 连接统计
	connectionsTotal   int64 // 总连接次数
	connectionsFailed  int64 // 连接失败次数
	connectionsActive  int64 // 当前活跃连接数
	disconnectsTotal   int64 // 断开连接次数
	handshakesTotal    int64 // 握手总次数
	handshakesFailed   int64 // 握手失败次数
	handshakeLatencyNs int64 // 握手延迟累计（纳秒）
	handshakeCount     int64 // 握手次数（用于计算平均值）

	// 流量统计
	bytesSent     int64 // 发送字节数
	bytesReceived int64 // 接收字节数
	packetsSent   int64 // 发送包数
	packetsRecv   int64 // 接收包数

	// NAT 打洞统计
	punchAttemptsUDP int64 // UDP 打洞尝试次数
	punchSuccessUDP  int64 // UDP 打洞成功次数
	punchAttemptsTCP int64 // TCP 打洞尝试次数
	punchSuccessTCP  int64 // TCP 打洞成功次数

	// 中继统计
	relayPackets    int64 // 中继转发包数
	relayBytes      int64 // 中继转发字节数
	relayConnects   int64 // 通过中继建立的连接数
	relayAuthFailed int64 // 中继认证失败次数

	// 错误统计
	errorsTotal int64 // 总错误数

	// 启动时间
	startTime time.Time

	// 历史记录（用于计算速率）
	mu            sync.RWMutex
	bytesSentHist []int64 // 每秒发送字节数历史
	bytesRecvHist []int64 // 每秒接收字节数历史
	histIndex     int
	histSize      int
}

// NewCollector 创建新的指标收集器
func NewCollector() *Collector {
	histSize := 60 // 保留60秒历史
	return &Collector{
		startTime:     time.Now(),
		bytesSentHist: make([]int64, histSize),
		bytesRecvHist: make([]int64, histSize),
		histSize:      histSize,
	}
}

// --- 连接统计 ---

// IncConnectionsTotal 增加总连接次数
func (c *Collector) IncConnectionsTotal() {
	atomic.AddInt64(&c.connectionsTotal, 1)
}

// IncConnectionsFailed 增加连接失败次数
func (c *Collector) IncConnectionsFailed() {
	atomic.AddInt64(&c.connectionsFailed, 1)
}

// SetConnectionsActive 设置当前活跃连接数
func (c *Collector) SetConnectionsActive(n int64) {
	atomic.StoreInt64(&c.connectionsActive, n)
}

// IncDisconnectsTotal 增加断开连接次数
func (c *Collector) IncDisconnectsTotal() {
	atomic.AddInt64(&c.disconnectsTotal, 1)
}

// IncHandshakesTotal 增加握手总次数
func (c *Collector) IncHandshakesTotal() {
	atomic.AddInt64(&c.handshakesTotal, 1)
}

// IncHandshakesFailed 增加握手失败次数
func (c *Collector) IncHandshakesFailed() {
	atomic.AddInt64(&c.handshakesFailed, 1)
}

// RecordHandshakeLatency 记录握手延迟
func (c *Collector) RecordHandshakeLatency(d time.Duration) {
	atomic.AddInt64(&c.handshakeLatencyNs, int64(d))
	atomic.AddInt64(&c.handshakeCount, 1)
}

// --- 流量统计 ---

// AddBytesSent 增加发送字节数
func (c *Collector) AddBytesSent(n int64) {
	atomic.AddInt64(&c.bytesSent, n)
	atomic.AddInt64(&c.packetsSent, 1)
}

// AddBytesReceived 增加接收字节数
func (c *Collector) AddBytesReceived(n int64) {
	atomic.AddInt64(&c.bytesReceived, n)
	atomic.AddInt64(&c.packetsRecv, 1)
}

// --- NAT 打洞统计 ---

// IncPunchAttemptUDP 增加 UDP 打洞尝试次数
func (c *Collector) IncPunchAttemptUDP() {
	atomic.AddInt64(&c.punchAttemptsUDP, 1)
}

// IncPunchSuccessUDP 增加 UDP 打洞成功次数
func (c *Collector) IncPunchSuccessUDP() {
	atomic.AddInt64(&c.punchSuccessUDP, 1)
}

// IncPunchAttemptTCP 增加 TCP 打洞尝试次数
func (c *Collector) IncPunchAttemptTCP() {
	atomic.AddInt64(&c.punchAttemptsTCP, 1)
}

// IncPunchSuccessTCP 增加 TCP 打洞成功次数
func (c *Collector) IncPunchSuccessTCP() {
	atomic.AddInt64(&c.punchSuccessTCP, 1)
}

// --- 中继统计 ---

// IncRelayPackets 增加中继转发包数
func (c *Collector) IncRelayPackets() {
	atomic.AddInt64(&c.relayPackets, 1)
}

// AddRelayBytes 增加中继转发字节数
func (c *Collector) AddRelayBytes(n int64) {
	atomic.AddInt64(&c.relayBytes, n)
}

// IncRelayConnects 增加通过中继建立的连接数
func (c *Collector) IncRelayConnects() {
	atomic.AddInt64(&c.relayConnects, 1)
}

// IncRelayAuthFailed 增加中继认证失败次数
func (c *Collector) IncRelayAuthFailed() {
	atomic.AddInt64(&c.relayAuthFailed, 1)
}

// --- 错误统计 ---

// IncErrorsTotal 增加总错误数
func (c *Collector) IncErrorsTotal() {
	atomic.AddInt64(&c.errorsTotal, 1)
}

// --- 快照获取 ---

// Snapshot 指标快照
type Snapshot struct {
	// 运行时间
	Uptime time.Duration

	// 连接统计
	ConnectionsTotal    int64
	ConnectionsFailed   int64
	ConnectionsActive   int64
	DisconnectsTotal    int64
	HandshakesTotal     int64
	HandshakesFailed    int64
	AvgHandshakeLatency time.Duration

	// 流量统计
	BytesSent     int64
	BytesReceived int64
	PacketsSent   int64
	PacketsRecv   int64

	// NAT 打洞统计
	PunchAttemptsUDP int64
	PunchSuccessUDP  int64
	PunchAttemptsTCP int64
	PunchSuccessTCP  int64
	PunchSuccessRate float64 // 打洞成功率

	// 中继统计
	RelayPackets    int64
	RelayBytes      int64
	RelayConnects   int64
	RelayAuthFailed int64

	// 错误统计
	ErrorsTotal int64

	// 计算的速率
	BytesSentPerSec int64
	BytesRecvPerSec int64
}

// GetSnapshot 获取当前指标快照
func (c *Collector) GetSnapshot() Snapshot {
	s := Snapshot{
		Uptime: time.Since(c.startTime),

		ConnectionsTotal:  atomic.LoadInt64(&c.connectionsTotal),
		ConnectionsFailed: atomic.LoadInt64(&c.connectionsFailed),
		ConnectionsActive: atomic.LoadInt64(&c.connectionsActive),
		DisconnectsTotal:  atomic.LoadInt64(&c.disconnectsTotal),
		HandshakesTotal:   atomic.LoadInt64(&c.handshakesTotal),
		HandshakesFailed:  atomic.LoadInt64(&c.handshakesFailed),

		BytesSent:     atomic.LoadInt64(&c.bytesSent),
		BytesReceived: atomic.LoadInt64(&c.bytesReceived),
		PacketsSent:   atomic.LoadInt64(&c.packetsSent),
		PacketsRecv:   atomic.LoadInt64(&c.packetsRecv),

		PunchAttemptsUDP: atomic.LoadInt64(&c.punchAttemptsUDP),
		PunchSuccessUDP:  atomic.LoadInt64(&c.punchSuccessUDP),
		PunchAttemptsTCP: atomic.LoadInt64(&c.punchAttemptsTCP),
		PunchSuccessTCP:  atomic.LoadInt64(&c.punchSuccessTCP),

		RelayPackets:    atomic.LoadInt64(&c.relayPackets),
		RelayBytes:      atomic.LoadInt64(&c.relayBytes),
		RelayConnects:   atomic.LoadInt64(&c.relayConnects),
		RelayAuthFailed: atomic.LoadInt64(&c.relayAuthFailed),

		ErrorsTotal: atomic.LoadInt64(&c.errorsTotal),
	}

	// 计算平均握手延迟
	count := atomic.LoadInt64(&c.handshakeCount)
	if count > 0 {
		totalNs := atomic.LoadInt64(&c.handshakeLatencyNs)
		s.AvgHandshakeLatency = time.Duration(totalNs / count)
	}

	// 计算打洞成功率
	totalAttempts := s.PunchAttemptsUDP + s.PunchAttemptsTCP
	totalSuccess := s.PunchSuccessUDP + s.PunchSuccessTCP
	if totalAttempts > 0 {
		s.PunchSuccessRate = float64(totalSuccess) / float64(totalAttempts)
	}

	return s
}

// Reset 重置所有指标
func (c *Collector) Reset() {
	atomic.StoreInt64(&c.connectionsTotal, 0)
	atomic.StoreInt64(&c.connectionsFailed, 0)
	atomic.StoreInt64(&c.connectionsActive, 0)
	atomic.StoreInt64(&c.disconnectsTotal, 0)
	atomic.StoreInt64(&c.handshakesTotal, 0)
	atomic.StoreInt64(&c.handshakesFailed, 0)
	atomic.StoreInt64(&c.handshakeLatencyNs, 0)
	atomic.StoreInt64(&c.handshakeCount, 0)

	atomic.StoreInt64(&c.bytesSent, 0)
	atomic.StoreInt64(&c.bytesReceived, 0)
	atomic.StoreInt64(&c.packetsSent, 0)
	atomic.StoreInt64(&c.packetsRecv, 0)

	atomic.StoreInt64(&c.punchAttemptsUDP, 0)
	atomic.StoreInt64(&c.punchSuccessUDP, 0)
	atomic.StoreInt64(&c.punchAttemptsTCP, 0)
	atomic.StoreInt64(&c.punchSuccessTCP, 0)

	atomic.StoreInt64(&c.relayPackets, 0)
	atomic.StoreInt64(&c.relayBytes, 0)
	atomic.StoreInt64(&c.relayConnects, 0)
	atomic.StoreInt64(&c.relayAuthFailed, 0)

	atomic.StoreInt64(&c.errorsTotal, 0)

	c.startTime = time.Now()
}

// Global 全局指标收集器
var Global = NewCollector()
