package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Collector collects runtime metrics for a node.
type Collector struct {
	// Connection stats.
	connectionsTotal   int64
	connectionsFailed  int64
	connectionsActive  int64
	disconnectsTotal   int64
	handshakesTotal    int64
	handshakesFailed   int64
	handshakeLatencyNs int64
	handshakeCount     int64

	// Traffic stats.
	bytesSent     int64
	bytesReceived int64
	packetsSent   int64
	packetsRecv   int64

	// NAT punch stats.
	punchAttemptsUDP int64
	punchSuccessUDP  int64
	punchAttemptsTCP int64
	punchSuccessTCP  int64

	// Relay stats.
	relayPackets    int64
	relayBytes      int64
	relayConnects   int64
	relayAuthFailed int64

	// Error stats.
	errorsTotal int64

	// Reconnect stats.
	reconnectAttempts int64
	reconnectSuccess  int64
	reconnectFailed   int64
	reconnectGaveUp   int64

	// Fast re-handshake stats.
	fastRehandshakeAttempts int64
	fastRehandshakeSuccess  int64
	fastRehandshakeFailed   int64

	startTime time.Time

	// Historical buffers reserved for rate calculations.
	mu            sync.RWMutex
	bytesSentHist []int64
	bytesRecvHist []int64
	histIndex     int
	histSize      int
}

// NewCollector creates a new Collector.
func NewCollector() *Collector {
	histSize := 60
	return &Collector{
		startTime:     time.Now(),
		bytesSentHist: make([]int64, histSize),
		bytesRecvHist: make([]int64, histSize),
		histSize:      histSize,
	}
}

func (c *Collector) IncConnectionsTotal() {
	atomic.AddInt64(&c.connectionsTotal, 1)
}

func (c *Collector) IncConnectionsFailed() {
	atomic.AddInt64(&c.connectionsFailed, 1)
}

func (c *Collector) SetConnectionsActive(n int64) {
	atomic.StoreInt64(&c.connectionsActive, n)
}

func (c *Collector) IncDisconnectsTotal() {
	atomic.AddInt64(&c.disconnectsTotal, 1)
}

func (c *Collector) IncHandshakesTotal() {
	atomic.AddInt64(&c.handshakesTotal, 1)
}

func (c *Collector) IncHandshakesFailed() {
	atomic.AddInt64(&c.handshakesFailed, 1)
}

func (c *Collector) RecordHandshakeLatency(d time.Duration) {
	atomic.AddInt64(&c.handshakeLatencyNs, int64(d))
	atomic.AddInt64(&c.handshakeCount, 1)
}

func (c *Collector) AddBytesSent(n int64) {
	atomic.AddInt64(&c.bytesSent, n)
	atomic.AddInt64(&c.packetsSent, 1)
}

func (c *Collector) AddBytesReceived(n int64) {
	atomic.AddInt64(&c.bytesReceived, n)
	atomic.AddInt64(&c.packetsRecv, 1)
}

func (c *Collector) IncPunchAttemptUDP() {
	atomic.AddInt64(&c.punchAttemptsUDP, 1)
}

func (c *Collector) IncPunchSuccessUDP() {
	atomic.AddInt64(&c.punchSuccessUDP, 1)
}

func (c *Collector) IncPunchAttemptTCP() {
	atomic.AddInt64(&c.punchAttemptsTCP, 1)
}

func (c *Collector) IncPunchSuccessTCP() {
	atomic.AddInt64(&c.punchSuccessTCP, 1)
}

func (c *Collector) IncRelayPackets() {
	atomic.AddInt64(&c.relayPackets, 1)
}

func (c *Collector) AddRelayBytes(n int64) {
	atomic.AddInt64(&c.relayBytes, n)
}

func (c *Collector) IncRelayConnects() {
	atomic.AddInt64(&c.relayConnects, 1)
}

func (c *Collector) IncRelayAuthFailed() {
	atomic.AddInt64(&c.relayAuthFailed, 1)
}

func (c *Collector) IncErrorsTotal() {
	atomic.AddInt64(&c.errorsTotal, 1)
}

func (c *Collector) IncReconnectAttempts() {
	atomic.AddInt64(&c.reconnectAttempts, 1)
}

func (c *Collector) IncReconnectSuccess() {
	atomic.AddInt64(&c.reconnectSuccess, 1)
}

func (c *Collector) IncReconnectFailed() {
	atomic.AddInt64(&c.reconnectFailed, 1)
}

func (c *Collector) IncReconnectGaveUp() {
	atomic.AddInt64(&c.reconnectGaveUp, 1)
}

func (c *Collector) IncFastRehandshakeAttempts() {
	atomic.AddInt64(&c.fastRehandshakeAttempts, 1)
}

func (c *Collector) IncFastRehandshakeSuccess() {
	atomic.AddInt64(&c.fastRehandshakeSuccess, 1)
}

func (c *Collector) IncFastRehandshakeFailed() {
	atomic.AddInt64(&c.fastRehandshakeFailed, 1)
}

// Snapshot represents a point-in-time metrics snapshot.
type Snapshot struct {
	Uptime time.Duration

	ConnectionsTotal    int64
	ConnectionsFailed   int64
	ConnectionsActive   int64
	DisconnectsTotal    int64
	HandshakesTotal     int64
	HandshakesFailed    int64
	AvgHandshakeLatency time.Duration

	BytesSent     int64
	BytesReceived int64
	PacketsSent   int64
	PacketsRecv   int64

	PunchAttemptsUDP int64
	PunchSuccessUDP  int64
	PunchAttemptsTCP int64
	PunchSuccessTCP  int64
	PunchSuccessRate float64

	RelayPackets    int64
	RelayBytes      int64
	RelayConnects   int64
	RelayAuthFailed int64

	ErrorsTotal int64

	ReconnectAttempts int64
	ReconnectSuccess  int64
	ReconnectFailed   int64
	ReconnectGaveUp   int64
	ReconnectRate     float64

	FastRehandshakeAttempts int64
	FastRehandshakeSuccess  int64
	FastRehandshakeFailed   int64

	BytesSentPerSec int64
	BytesRecvPerSec int64
}

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

		ReconnectAttempts: atomic.LoadInt64(&c.reconnectAttempts),
		ReconnectSuccess:  atomic.LoadInt64(&c.reconnectSuccess),
		ReconnectFailed:   atomic.LoadInt64(&c.reconnectFailed),
		ReconnectGaveUp:   atomic.LoadInt64(&c.reconnectGaveUp),

		FastRehandshakeAttempts: atomic.LoadInt64(&c.fastRehandshakeAttempts),
		FastRehandshakeSuccess:  atomic.LoadInt64(&c.fastRehandshakeSuccess),
		FastRehandshakeFailed:   atomic.LoadInt64(&c.fastRehandshakeFailed),
	}

	count := atomic.LoadInt64(&c.handshakeCount)
	if count > 0 {
		totalNs := atomic.LoadInt64(&c.handshakeLatencyNs)
		s.AvgHandshakeLatency = time.Duration(totalNs / count)
	}

	totalAttempts := s.PunchAttemptsUDP + s.PunchAttemptsTCP
	totalSuccess := s.PunchSuccessUDP + s.PunchSuccessTCP
	if totalAttempts > 0 {
		s.PunchSuccessRate = float64(totalSuccess) / float64(totalAttempts)
	}

	if s.ReconnectAttempts > 0 {
		s.ReconnectRate = float64(s.ReconnectSuccess) / float64(s.ReconnectAttempts)
	}

	return s
}

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

	atomic.StoreInt64(&c.reconnectAttempts, 0)
	atomic.StoreInt64(&c.reconnectSuccess, 0)
	atomic.StoreInt64(&c.reconnectFailed, 0)
	atomic.StoreInt64(&c.reconnectGaveUp, 0)

	atomic.StoreInt64(&c.fastRehandshakeAttempts, 0)
	atomic.StoreInt64(&c.fastRehandshakeSuccess, 0)
	atomic.StoreInt64(&c.fastRehandshakeFailed, 0)

	c.startTime = time.Now()
}

// Global is the process-level metrics collector.
var Global = NewCollector()
