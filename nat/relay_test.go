package nat

import (
	"net"
	"testing"
	"time"
)

func TestRelayMetricsScore(t *testing.T) {
	// 高质量中继
	high := &RelayMetrics{
		Latency:     10 * time.Millisecond,
		Bandwidth:   10000000, // 10 Mbps
		Reliability: 0.99,
		PacketLoss:  0.01,
	}
	highScore := high.Score()
	if highScore <= 0 {
		t.Errorf("高质量中继 Score 应该 > 0，实际 %f", highScore)
	}

	// 低质量中继
	low := &RelayMetrics{
		Latency:     500 * time.Millisecond,
		Bandwidth:   100000, // 100 Kbps
		Reliability: 0.5,
		PacketLoss:  0.5,
	}
	lowScore := low.Score()

	if lowScore >= highScore {
		t.Errorf("低质量中继评分 (%f) 不应该 >= 高质量中继评分 (%f)", lowScore, highScore)
	}
}

func TestRelayMetricsScoreZeroLatency(t *testing.T) {
	m := &RelayMetrics{
		Latency:     0,
		Bandwidth:   1000000,
		Reliability: 1.0,
	}
	score := m.Score()
	// 零延迟应该得到很高的分数
	if score <= 0 {
		t.Errorf("零延迟 Score 应该 > 0，实际 %f", score)
	}
}

func TestNewRelayManager(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatalf("创建 UDP 连接失败: %v", err)
	}
	defer conn.Close()

	rm := NewRelayManager(conn)
	if rm == nil {
		t.Fatal("NewRelayManager 返回 nil")
	}
	if rm.conn != conn {
		t.Error("conn 未正确设置")
	}
	if rm.relays == nil || rm.relayMetrics == nil || rm.activeRelays == nil {
		t.Error("内部 map 未初始化")
	}
}

func TestRelayManagerAddRemove(t *testing.T) {
	conn, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	defer conn.Close()
	rm := NewRelayManager(conn)

	addr1, _ := net.ResolveUDPAddr("udp", "192.168.1.1:8000")
	addr2, _ := net.ResolveUDPAddr("udp", "192.168.1.2:8000")

	// 添加
	rm.AddRelay("relay1", addr1)
	rm.AddRelay("relay2", addr2)

	rm.mu.RLock()
	if len(rm.relays) != 2 {
		t.Errorf("添加后应有 2 个中继，实际 %d", len(rm.relays))
	}
	rm.mu.RUnlock()

	// 删除
	rm.RemoveRelay("relay1")

	rm.mu.RLock()
	if len(rm.relays) != 1 {
		t.Errorf("删除后应有 1 个中继，实际 %d", len(rm.relays))
	}
	if _, exists := rm.relays["relay1"]; exists {
		t.Error("relay1 应该被删除")
	}
	rm.mu.RUnlock()
}

func TestSelectBestRelayEmpty(t *testing.T) {
	conn, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	defer conn.Close()
	rm := NewRelayManager(conn)

	_, err := rm.SelectBestRelay()
	if err == nil {
		t.Error("空中继列表应返回错误")
	}
}

func TestSelectBestRelaySingle(t *testing.T) {
	conn, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	defer conn.Close()
	rm := NewRelayManager(conn)

	addr, _ := net.ResolveUDPAddr("udp", "192.168.1.1:8000")
	rm.AddRelay("only-relay", addr)

	best, err := rm.SelectBestRelay()
	if err != nil {
		t.Fatalf("SelectBestRelay 失败: %v", err)
	}
	if best.ID != "only-relay" {
		t.Errorf("期望 only-relay，实际 %s", best.ID)
	}
}

func TestSelectBestRelayWithMetrics(t *testing.T) {
	conn, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	defer conn.Close()
	rm := NewRelayManager(conn)

	addr1, _ := net.ResolveUDPAddr("udp", "192.168.1.1:8000")
	addr2, _ := net.ResolveUDPAddr("udp", "192.168.1.2:8000")
	addr3, _ := net.ResolveUDPAddr("udp", "192.168.1.3:8000")

	rm.AddRelay("slow", addr1)
	rm.AddRelay("fast", addr2)
	rm.AddRelay("medium", addr3)

	// 手动设置指标
	rm.mu.Lock()
	rm.relayMetrics["slow"] = &RelayMetrics{
		Latency:     500 * time.Millisecond,
		Bandwidth:   100000,
		Reliability: 0.5,
	}
	rm.relayMetrics["fast"] = &RelayMetrics{
		Latency:     10 * time.Millisecond,
		Bandwidth:   10000000,
		Reliability: 0.99,
	}
	rm.relayMetrics["medium"] = &RelayMetrics{
		Latency:     100 * time.Millisecond,
		Bandwidth:   1000000,
		Reliability: 0.8,
	}
	rm.mu.Unlock()

	best, err := rm.SelectBestRelay()
	if err != nil {
		t.Fatalf("SelectBestRelay 失败: %v", err)
	}
	if best.ID != "fast" {
		t.Errorf("应选择 fast，实际选择 %s", best.ID)
	}
}

func TestRelayInfoFields(t *testing.T) {
	addr, _ := net.ResolveUDPAddr("udp", "10.0.0.1:9000")
	info := &RelayInfo{
		ID:        "test-relay",
		Addr:      addr,
		Latency:   50 * time.Millisecond,
		Bandwidth: 5000000,
		LastCheck: time.Now(),
	}

	if info.ID != "test-relay" {
		t.Error("ID 不匹配")
	}
	if info.Addr.Port != 9000 {
		t.Error("Addr 端口不匹配")
	}
	if info.Latency != 50*time.Millisecond {
		t.Error("Latency 不匹配")
	}
}
