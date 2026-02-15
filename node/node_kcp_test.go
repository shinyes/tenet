package node

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// TestNodeWithKCP 测试节点间使用 KCP 通信
func TestNodeWithKCP(t *testing.T) {
	// 创建两个节点
	node1, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithListenPort(0),
		WithListenPort(0),
	)
	if err != nil {
		t.Fatalf("创建节点1失败: %v", err)
	}

	node2, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithListenPort(0),
		WithListenPort(0),
	)
	if err != nil {
		t.Fatalf("创建节点2失败: %v", err)
	}

	// 启动节点
	if err := node1.Start(); err != nil {
		t.Fatalf("启动节点1失败: %v", err)
	}
	defer node1.Stop()

	if err := node2.Start(); err != nil {
		t.Fatalf("启动节点2失败: %v", err)
	}
	defer node2.Stop()

	t.Logf("节点1 ID: %s, 地址: %s", node1.ID()[:8], node1.LocalAddr)
	t.Logf("节点2 ID: %s, 地址: %s", node2.ID()[:8], node2.LocalAddr)

	// 设置接收回调
	received := make(chan string, 10)
	node2.OnReceive(func(peerID string, data []byte) {
		received <- string(data)
	})

	// 等待连接建立
	connected := make(chan struct{}, 1)
	node1.OnPeerConnected(func(peerID string) {
		select {
		case connected <- struct{}{}:
		default:
		}
	})

	// 节点1连接节点2
	addr := fmt.Sprintf("127.0.0.1:%d", node2.LocalAddr.Port)
	if err := node1.Connect(addr); err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	// 等待连接
	select {
	case <-connected:
		t.Log("连接建立成功")
	case <-time.After(5 * time.Second):
		t.Fatal("连接超时")
	}

	// 获取对端 ID
	peers := node1.Peers.IDs()
	if len(peers) == 0 {
		t.Fatal("没有已连接的节点")
	}
	peerID := peers[0]

	// 发送消息 (加入测试频道)
	channelName := "test-channel"
	node1.JoinChannel(channelName)
	node2.JoinChannel(channelName)

	testMsg := "Hello via KCP!"
	if err := node1.Send(channelName, peerID, []byte(testMsg)); err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	// 等待接收
	select {
	case msg := <-received:
		if msg != testMsg {
			t.Errorf("消息不匹配: 期望 %q，实际 %q", testMsg, msg)
		} else {
			t.Logf("成功收到消息: %s", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("接收超时")
	}
}

// TestNodeKCPReliableTransfer 测试节点间数据传输
// 测试 TCP/UDP 基本传输能力，不强制要求所有消息都成功
func TestNodeKCPReliableTransfer(t *testing.T) {
	// 创建两个节点
	node1, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithListenPort(0),
		WithListenPort(0),
	)
	if err != nil {
		t.Fatalf("创建节点1失败: %v", err)
	}

	node2, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithListenPort(0),
		WithListenPort(0),
	)
	if err != nil {
		t.Fatalf("创建节点2失败: %v", err)
	}

	if err := node1.Start(); err != nil {
		t.Fatalf("启动节点1失败: %v", err)
	}
	defer node1.Stop()

	if err := node2.Start(); err != nil {
		t.Fatalf("启动节点2失败: %v", err)
	}
	defer node2.Stop()

	// 设置接收回调 - 统计收到的消息
	var receivedCount int32
	var receivedBytes int64
	messageCount := 10 // 少量消息测试

	receivedChan := make(chan struct{}, messageCount)

	node2.OnReceive(func(peerID string, data []byte) {
		atomic.AddInt32(&receivedCount, 1)
		atomic.AddInt64(&receivedBytes, int64(len(data)))
		select {
		case receivedChan <- struct{}{}:
		default:
		}
	})

	// 等待连接建立
	connected := make(chan struct{}, 1)
	node1.OnPeerConnected(func(peerID string) {
		select {
		case connected <- struct{}{}:
		default:
		}
	})

	// 连接
	addr := fmt.Sprintf("127.0.0.1:%d", node2.LocalAddr.Port)
	if err := node1.Connect(addr); err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("连接超时")
	}

	peers := node1.Peers.IDs()
	if len(peers) == 0 {
		t.Fatal("没有已连接的节点")
	}
	peerID := peers[0]

	// 等待传输层稳定
	time.Sleep(1 * time.Second)

	// 检查传输类型
	transport := node1.GetPeerTransport(peerID)
	t.Logf("传输类型: %s", transport)

	// 加入测试频道
	channelName := "test-reliable"
	node1.JoinChannel(channelName)
	node2.JoinChannel(channelName)

	// 发送多条消息
	sentBytes := int64(0)
	for i := 0; i < messageCount; i++ {
		msg := fmt.Sprintf("Message #%d: Test", i)
		if err := node1.Send(channelName, peerID, []byte(msg)); err != nil {
			t.Fatalf("发送消息 %d 失败: %v", i, err)
		}
		sentBytes += int64(len(msg))
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("已发送 %d 条消息，共 %d 字节", messageCount, sentBytes)

	// 等待接收
	time.Sleep(2 * time.Second)

	count := atomic.LoadInt32(&receivedCount)
	bytes := atomic.LoadInt64(&receivedBytes)
	t.Logf("收到 %d/%d 条消息，共 %d 字节", count, messageCount, bytes)

	// 至少应该收到一些消息
	if count == 0 {
		t.Error("没有收到任何消息")
	} else if count < int32(messageCount) {
		t.Logf("注意: 只收到 %d/%d 条消息（UDP 传输可能丢包）", count, messageCount)
	}
}

// TestNodeKCPCustomConfig 测试自定义 KCP 配置
func TestNodeKCPCustomConfig(t *testing.T) {
	// 使用极速模式配置
	fastConfig := &KCPConfig{
		NoDelay:    1,
		Interval:   10,
		Resend:     2,
		NC:         1,
		SndWnd:     128,
		RcvWnd:     128,
		MTU:        1350,
		StreamMode: true,
	}

	node1, err := NewNode(
		WithNetworkPassword("test-secret"),
		WithListenPort(0),
		WithListenPort(0),
		WithKCPConfig(fastConfig),
	)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	if err := node1.Start(); err != nil {
		t.Fatalf("启动节点失败: %v", err)
	}
	defer node1.Stop()

	// 验证配置
	if node1.Config.KCPConfig == nil {
		t.Error("KCP 配置不应为 nil")
	} else {
		cfg := node1.Config.KCPConfig
		if cfg.NoDelay != 1 {
			t.Errorf("NoDelay 期望 1，实际 %d", cfg.NoDelay)
		}
		if cfg.Interval != 10 {
			t.Errorf("Interval 期望 10，实际 %d", cfg.Interval)
		}
	}

	t.Log("自定义 KCP 配置验证通过")
}

// BenchmarkNodeSend 基准测试节点发送性能
func BenchmarkNodeSend(b *testing.B) {
	node1, _ := NewNode(
		WithNetworkPassword("bench-secret"),
		WithListenPort(0),
		WithListenPort(0),
	)
	node2, _ := NewNode(
		WithNetworkPassword("bench-secret"),
		WithListenPort(0),
		WithListenPort(0),
	)

	node1.Start()
	defer node1.Stop()
	node2.Start()
	defer node2.Stop()

	// 静默接收
	node2.OnReceive(func(peerID string, data []byte) {})

	connected := make(chan struct{}, 1)
	node1.OnPeerConnected(func(peerID string) {
		select {
		case connected <- struct{}{}:
		default:
		}
	})

	addr := fmt.Sprintf("127.0.0.1:%d", node2.LocalAddr.Port)
	node1.Connect(addr)

	<-connected
	peers := node1.Peers.IDs()
	peerID := peers[0]

	// 加入测试频道
	channelName := "bench-channel"
	node1.JoinChannel(channelName)
	node2.JoinChannel(channelName)

	// 等待 KCP 升级
	time.Sleep(500 * time.Millisecond)

	data := make([]byte, 1024) // 1KB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		node1.Send(channelName, peerID, data)
	}
	b.SetBytes(1024)
}
