package metrics

import (
	"sync"
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector()
	if c == nil {
		t.Fatal("NewCollector 返回 nil")
	}
	if c.startTime.IsZero() {
		t.Error("startTime 未初始化")
	}
	if len(c.bytesSentHist) != 60 {
		t.Errorf("bytesSentHist 长度期望 60，实际 %d", len(c.bytesSentHist))
	}
}

func TestConnectionStats(t *testing.T) {
	c := NewCollector()

	c.IncConnectionsTotal()
	c.IncConnectionsTotal()
	c.IncConnectionsFailed()

	snap := c.GetSnapshot()
	if snap.ConnectionsTotal != 2 {
		t.Errorf("ConnectionsTotal 期望 2，实际 %d", snap.ConnectionsTotal)
	}
	if snap.ConnectionsFailed != 1 {
		t.Errorf("ConnectionsFailed 期望 1，实际 %d", snap.ConnectionsFailed)
	}
}

func TestActiveConnections(t *testing.T) {
	c := NewCollector()

	c.SetConnectionsActive(5)
	snap := c.GetSnapshot()
	if snap.ConnectionsActive != 5 {
		t.Errorf("ConnectionsActive 期望 5，实际 %d", snap.ConnectionsActive)
	}

	c.SetConnectionsActive(3)
	snap = c.GetSnapshot()
	if snap.ConnectionsActive != 3 {
		t.Errorf("ConnectionsActive 期望 3，实际 %d", snap.ConnectionsActive)
	}
}

func TestDisconnectStats(t *testing.T) {
	c := NewCollector()

	c.IncDisconnectsTotal()
	c.IncDisconnectsTotal()
	c.IncDisconnectsTotal()

	snap := c.GetSnapshot()
	if snap.DisconnectsTotal != 3 {
		t.Errorf("DisconnectsTotal 期望 3，实际 %d", snap.DisconnectsTotal)
	}
}

func TestHandshakeStats(t *testing.T) {
	c := NewCollector()

	c.IncHandshakesTotal()
	c.IncHandshakesTotal()
	c.IncHandshakesFailed()
	c.RecordHandshakeLatency(100 * time.Millisecond)
	c.RecordHandshakeLatency(200 * time.Millisecond)

	snap := c.GetSnapshot()
	if snap.HandshakesTotal != 2 {
		t.Errorf("HandshakesTotal 期望 2，实际 %d", snap.HandshakesTotal)
	}
	if snap.HandshakesFailed != 1 {
		t.Errorf("HandshakesFailed 期望 1，实际 %d", snap.HandshakesFailed)
	}

	expectedAvg := 150 * time.Millisecond
	if snap.AvgHandshakeLatency != expectedAvg {
		t.Errorf("AvgHandshakeLatency 期望 %v，实际 %v", expectedAvg, snap.AvgHandshakeLatency)
	}
}

func TestTrafficStats(t *testing.T) {
	c := NewCollector()

	c.AddBytesSent(1000)
	c.AddBytesSent(500)
	c.AddBytesReceived(2000)

	snap := c.GetSnapshot()
	if snap.BytesSent != 1500 {
		t.Errorf("BytesSent 期望 1500，实际 %d", snap.BytesSent)
	}
	if snap.BytesReceived != 2000 {
		t.Errorf("BytesReceived 期望 2000，实际 %d", snap.BytesReceived)
	}
	if snap.PacketsSent != 2 {
		t.Errorf("PacketsSent 期望 2，实际 %d", snap.PacketsSent)
	}
	if snap.PacketsRecv != 1 {
		t.Errorf("PacketsRecv 期望 1，实际 %d", snap.PacketsRecv)
	}
}

func TestNATPunchStats(t *testing.T) {
	c := NewCollector()

	c.IncPunchAttemptUDP()
	c.IncPunchAttemptUDP()
	c.IncPunchSuccessUDP()
	c.IncPunchAttemptTCP()
	c.IncPunchSuccessTCP()

	snap := c.GetSnapshot()
	if snap.PunchAttemptsUDP != 2 {
		t.Errorf("PunchAttemptsUDP 期望 2，实际 %d", snap.PunchAttemptsUDP)
	}
	if snap.PunchSuccessUDP != 1 {
		t.Errorf("PunchSuccessUDP 期望 1，实际 %d", snap.PunchSuccessUDP)
	}
	if snap.PunchAttemptsTCP != 1 {
		t.Errorf("PunchAttemptsTCP 期望 1，实际 %d", snap.PunchAttemptsTCP)
	}
	if snap.PunchSuccessTCP != 1 {
		t.Errorf("PunchSuccessTCP 期望 1，实际 %d", snap.PunchSuccessTCP)
	}
}

func TestRelayStats(t *testing.T) {
	c := NewCollector()

	c.IncRelayPackets()
	c.IncRelayPackets()
	c.AddRelayBytes(1024)
	c.IncRelayConnects()
	c.IncRelayAuthFailed()

	snap := c.GetSnapshot()
	if snap.RelayPackets != 2 {
		t.Errorf("RelayPackets 期望 2，实际 %d", snap.RelayPackets)
	}
	if snap.RelayBytes != 1024 {
		t.Errorf("RelayBytes 期望 1024，实际 %d", snap.RelayBytes)
	}
	if snap.RelayConnects != 1 {
		t.Errorf("RelayConnects 期望 1，实际 %d", snap.RelayConnects)
	}
	if snap.RelayAuthFailed != 1 {
		t.Errorf("RelayAuthFailed 期望 1，实际 %d", snap.RelayAuthFailed)
	}
}

func TestReconnectStats(t *testing.T) {
	c := NewCollector()

	c.IncReconnectAttempts()
	c.IncReconnectAttempts()
	c.IncReconnectSuccess()
	c.IncReconnectFailed()
	c.IncReconnectGaveUp()

	snap := c.GetSnapshot()
	if snap.ReconnectAttempts != 2 {
		t.Errorf("ReconnectAttempts 期望 2，实际 %d", snap.ReconnectAttempts)
	}
	if snap.ReconnectSuccess != 1 {
		t.Errorf("ReconnectSuccess 期望 1，实际 %d", snap.ReconnectSuccess)
	}
	if snap.ReconnectFailed != 1 {
		t.Errorf("ReconnectFailed 期望 1，实际 %d", snap.ReconnectFailed)
	}
	if snap.ReconnectGaveUp != 1 {
		t.Errorf("ReconnectGaveUp 期望 1，实际 %d", snap.ReconnectGaveUp)
	}
}

func TestErrorStats(t *testing.T) {
	c := NewCollector()

	c.IncErrorsTotal()
	c.IncErrorsTotal()

	snap := c.GetSnapshot()
	if snap.ErrorsTotal != 2 {
		t.Errorf("ErrorsTotal 期望 2，实际 %d", snap.ErrorsTotal)
	}
}

func TestReset(t *testing.T) {
	c := NewCollector()

	c.IncConnectionsTotal()
	c.AddBytesSent(1000)
	c.IncPunchAttemptUDP()

	c.Reset()

	snap := c.GetSnapshot()
	if snap.ConnectionsTotal != 0 {
		t.Errorf("Reset 后 ConnectionsTotal 应为 0，实际 %d", snap.ConnectionsTotal)
	}
	if snap.BytesSent != 0 {
		t.Errorf("Reset 后 BytesSent 应为 0，实际 %d", snap.BytesSent)
	}
}

func TestUptime(t *testing.T) {
	c := NewCollector()
	time.Sleep(10 * time.Millisecond)

	snap := c.GetSnapshot()
	if snap.Uptime < 10*time.Millisecond {
		t.Errorf("Uptime 应该 >= 10ms，实际 %v", snap.Uptime)
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := NewCollector()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.IncConnectionsTotal()
			c.AddBytesSent(100)
			c.AddBytesReceived(200)
			c.IncPunchAttemptUDP()
		}()
	}

	wg.Wait()

	snap := c.GetSnapshot()
	if snap.ConnectionsTotal != 100 {
		t.Errorf("并发后 ConnectionsTotal 期望 100，实际 %d", snap.ConnectionsTotal)
	}
	if snap.BytesSent != 10000 {
		t.Errorf("并发后 BytesSent 期望 10000，实际 %d", snap.BytesSent)
	}
}

func TestGlobalCollector(t *testing.T) {
	if Global == nil {
		t.Error("Global 收集器为 nil")
	}

	initialCount := Global.GetSnapshot().ConnectionsTotal
	Global.IncConnectionsTotal()
	snap := Global.GetSnapshot()
	if snap.ConnectionsTotal != initialCount+1 {
		t.Error("Global 收集器无法正常工作")
	}
}

func TestFastRehandshakeStats(t *testing.T) {
	c := NewCollector()

	c.IncFastRehandshakeAttempts()
	c.IncFastRehandshakeAttempts()
	c.IncFastRehandshakeSuccess()
	c.IncFastRehandshakeFailed()

	snap := c.GetSnapshot()
	if snap.FastRehandshakeAttempts != 2 {
		t.Errorf("FastRehandshakeAttempts expected 2, got %d", snap.FastRehandshakeAttempts)
	}
	if snap.FastRehandshakeSuccess != 1 {
		t.Errorf("FastRehandshakeSuccess expected 1, got %d", snap.FastRehandshakeSuccess)
	}
	if snap.FastRehandshakeFailed != 1 {
		t.Errorf("FastRehandshakeFailed expected 1, got %d", snap.FastRehandshakeFailed)
	}
}
