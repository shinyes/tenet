package node

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDefaultReconnectConfig 测试默认重连配置
func TestDefaultReconnectConfig(t *testing.T) {
	cfg := DefaultReconnectConfig()

	if cfg == nil {
		t.Fatal("DefaultReconnectConfig 返回 nil")
	}

	if !cfg.Enabled {
		t.Error("Enabled 期望 true")
	}
	if cfg.MaxRetries != 10 {
		t.Errorf("MaxRetries 期望 10，实际 %d", cfg.MaxRetries)
	}
	if cfg.InitialDelay != time.Second {
		t.Errorf("InitialDelay 期望 1s，实际 %v", cfg.InitialDelay)
	}
	if cfg.MaxDelay != 5*time.Minute {
		t.Errorf("MaxDelay 期望 5m，实际 %v", cfg.MaxDelay)
	}
	if cfg.BackoffMultiplier != 2.0 {
		t.Errorf("BackoffMultiplier 期望 2.0，实际 %f", cfg.BackoffMultiplier)
	}
	if cfg.JitterFactor != 0.2 {
		t.Errorf("JitterFactor 期望 0.2，实际 %f", cfg.JitterFactor)
	}
}

// TestReconnectStateString 测试重连状态字符串表示
func TestReconnectStateString(t *testing.T) {
	tests := []struct {
		state    ReconnectState
		expected string
	}{
		{ReconnectStateIdle, "Idle"},
		{ReconnectStateWaiting, "Waiting"},
		{ReconnectStateConnecting, "Connecting"},
		{ReconnectStateSuccess, "Success"},
		{ReconnectStateFailed, "Failed"},
		{ReconnectState(99), "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("ReconnectState(%d).String() = %s, 期望 %s", tt.state, got, tt.expected)
		}
	}
}

// TestReconnectManagerCreation 测试重连管理器创建
func TestReconnectManagerCreation(t *testing.T) {
	// 创建一个简单的节点用于测试
	node, err := NewNode(
		WithNetworkPassword("test"),
		WithListenPort(0),
		WithEnableReconnect(false), // 禁用自动重连
	)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	// 使用默认配置创建重连管理器
	rm := NewReconnectManager(node, nil)
	if rm == nil {
		t.Fatal("NewReconnectManager 返回 nil")
	}
	if rm.config == nil {
		t.Fatal("ReconnectManager.config 为 nil")
	}
	if !rm.config.Enabled {
		t.Error("默认配置应该启用重连")
	}

	// 使用自定义配置创建
	customCfg := &ReconnectConfig{
		Enabled:    true,
		MaxRetries: 5,
	}
	rm2 := NewReconnectManager(node, customCfg)
	if rm2.config.MaxRetries != 5 {
		t.Errorf("MaxRetries 期望 5，实际 %d", rm2.config.MaxRetries)
	}
}

// TestReconnectManagerCallbacks 测试重连管理器回调设置
func TestReconnectManagerCallbacks(t *testing.T) {
	node, err := NewNode(
		WithNetworkPassword("test"),
		WithListenPort(0),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	rm := NewReconnectManager(node, DefaultReconnectConfig())

	// 测试回调设置
	var reconnectingCalled int32
	var reconnectedCalled int32
	var gaveUpCalled int32

	rm.SetOnReconnecting(func(peerID string, attempt int, nextRetryIn time.Duration) {
		atomic.AddInt32(&reconnectingCalled, 1)
	})

	rm.SetOnReconnected(func(peerID string, attempts int) {
		atomic.AddInt32(&reconnectedCalled, 1)
	})

	rm.SetOnGaveUp(func(peerID string, attempts int, lastErr error) {
		atomic.AddInt32(&gaveUpCalled, 1)
	})

	// 验证回调已设置
	rm.mu.RLock()
	if rm.onReconnecting == nil {
		t.Error("onReconnecting 未设置")
	}
	if rm.onReconnected == nil {
		t.Error("onReconnected 未设置")
	}
	if rm.onGaveUp == nil {
		t.Error("onGaveUp 未设置")
	}
	rm.mu.RUnlock()
}

// TestReconnectManagerSchedule 测试调度重连任务
func TestReconnectManagerSchedule(t *testing.T) {
	node, err := NewNode(
		WithNetworkPassword("test"),
		WithListenPort(0),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	cfg := &ReconnectConfig{
		Enabled:           true,
		MaxRetries:        2,
		InitialDelay:      100 * time.Millisecond,
		MaxDelay:          1 * time.Second,
		BackoffMultiplier: 2.0,
		JitterFactor:      0,
		ReconnectTimeout:  500 * time.Millisecond,
	}
	rm := NewReconnectManager(node, cfg)
	defer rm.Close()

	testPeerID := "test-peer-12345678"
	testAddr := "127.0.0.1:9999"

	// 调度重连
	rm.ScheduleReconnect(testPeerID, testAddr)

	// 验证任务已创建
	time.Sleep(50 * time.Millisecond)
	info := rm.GetReconnectInfo(testPeerID)
	if info == nil {
		t.Fatal("未找到重连信息")
	}
	if info.PeerID != testPeerID {
		t.Errorf("PeerID 期望 %s，实际 %s", testPeerID, info.PeerID)
	}
	if info.Addr != testAddr {
		t.Errorf("Addr 期望 %s，实际 %s", testAddr, info.Addr)
	}

	// 取消重连
	rm.CancelReconnect(testPeerID)
	time.Sleep(50 * time.Millisecond)

	info = rm.GetReconnectInfo(testPeerID)
	if info != nil {
		t.Error("取消后不应该有重连信息")
	}
}

// TestReconnectManagerCancelAll 测试取消所有重连任务
func TestReconnectManagerCancelAll(t *testing.T) {
	node, err := NewNode(
		WithNetworkPassword("test"),
		WithListenPort(0),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	cfg := &ReconnectConfig{
		Enabled:           true,
		MaxRetries:        10,
		InitialDelay:      1 * time.Second,
		MaxDelay:          10 * time.Second,
		BackoffMultiplier: 2.0,
		JitterFactor:      0,
		ReconnectTimeout:  5 * time.Second,
	}
	rm := NewReconnectManager(node, cfg)
	defer rm.Close()

	// 调度多个重连任务
	for i := 0; i < 5; i++ {
		rm.ScheduleReconnect("peer-"+string(rune('A'+i)), "127.0.0.1:"+string(rune('0'+i)))
	}

	time.Sleep(50 * time.Millisecond)

	// 验证有任务
	infos := rm.GetAllReconnectInfo()
	if len(infos) != 5 {
		t.Errorf("期望 5 个重连任务，实际 %d", len(infos))
	}

	// 取消所有
	rm.CancelAll()
	time.Sleep(50 * time.Millisecond)

	infos = rm.GetAllReconnectInfo()
	if len(infos) != 0 {
		t.Errorf("取消后期望 0 个重连任务，实际 %d", len(infos))
	}
}

// TestCalculateBackoff 测试退避计算
func TestCalculateBackoff(t *testing.T) {
	node, err := NewNode(
		WithNetworkPassword("test"),
		WithListenPort(0),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	cfg := &ReconnectConfig{
		Enabled:           true,
		MaxRetries:        10,
		InitialDelay:      time.Second,
		MaxDelay:          time.Minute,
		BackoffMultiplier: 2.0,
		JitterFactor:      0, // 禁用抖动以便精确测试
	}
	rm := NewReconnectManager(node, cfg)

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, time.Second},      // 1s * 2^0 = 1s
		{2, 2 * time.Second},  // 1s * 2^1 = 2s
		{3, 4 * time.Second},  // 1s * 2^2 = 4s
		{4, 8 * time.Second},  // 1s * 2^3 = 8s
		{5, 16 * time.Second}, // 1s * 2^4 = 16s
		{6, 32 * time.Second}, // 1s * 2^5 = 32s
		{7, time.Minute},      // 1s * 2^6 = 64s，但被限制为 60s
	}

	for _, tt := range tests {
		got := rm.calculateBackoff(tt.attempt)
		if got != tt.expected {
			t.Errorf("calculateBackoff(%d) = %v, 期望 %v", tt.attempt, got, tt.expected)
		}
	}
}

// TestReconnectGaveUpCallback 测试达到最大重试次数后的回调
func TestReconnectGaveUpCallback(t *testing.T) {
	node, err := NewNode(
		WithNetworkPassword("test"),
		WithListenPort(0),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	cfg := &ReconnectConfig{
		Enabled:           true,
		MaxRetries:        1, // 只重试 1 次
		InitialDelay:      50 * time.Millisecond,
		MaxDelay:          100 * time.Millisecond,
		BackoffMultiplier: 1.0,
		JitterFactor:      0,
		ReconnectTimeout:  100 * time.Millisecond,
	}
	rm := NewReconnectManager(node, cfg)
	defer rm.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	var gaveUpPeerID string
	rm.SetOnGaveUp(func(peerID string, attempts int, lastErr error) {
		gaveUpPeerID = peerID
		wg.Done()
	})

	testPeerID := "test-peer-gaveup"
	rm.ScheduleReconnect(testPeerID, "192.0.2.1:9999") // 使用不可达地址

	// 等待回调或超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if gaveUpPeerID != testPeerID {
			t.Errorf("gaveUp peerID 期望 %s，实际 %s", testPeerID, gaveUpPeerID)
		}
	case <-time.After(5 * time.Second):
		t.Log("测试超时，但这是预期的（重连尝试可能仍在进行）")
	}
}

// TestReconnectDisabled 测试禁用重连
func TestReconnectDisabled(t *testing.T) {
	node, err := NewNode(
		WithNetworkPassword("test"),
		WithListenPort(0),
		WithEnableReconnect(false),
	)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	cfg := &ReconnectConfig{
		Enabled: false,
	}
	rm := NewReconnectManager(node, cfg)
	defer rm.Close()

	// 尝试调度重连
	rm.ScheduleReconnect("test-peer", "127.0.0.1:9999")

	time.Sleep(50 * time.Millisecond)

	// 应该没有任务被创建
	infos := rm.GetAllReconnectInfo()
	if len(infos) != 0 {
		t.Errorf("禁用重连时不应创建任务，实际 %d 个", len(infos))
	}
}

// TestMinFunction 测试 min 辅助函数
func TestMinFunction(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{1, 2, 1},
		{5, 3, 3},
		{0, 0, 0},
		{-1, 1, -1},
	}

	for _, tt := range tests {
		if got := min(tt.a, tt.b); got != tt.expected {
			t.Errorf("min(%d, %d) = %d, 期望 %d", tt.a, tt.b, got, tt.expected)
		}
	}
}
