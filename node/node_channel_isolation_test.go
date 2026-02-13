package node

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestChannelIsolation 测试严格的频道隔离
func TestChannelIsolation(t *testing.T) {
	t.Run("同频道通信", func(t *testing.T) {
		channelA := "channel-a"

		node1, err := NewNode(
			WithNetworkPassword("test-secret"),
			WithListenPort(0),
			WithChannelID(channelA),
		)
		if err != nil {
			t.Fatalf("创建节点1失败: %v", err)
		}
		defer node1.Stop()

		node2, err := NewNode(
			WithNetworkPassword("test-secret"),
			WithListenPort(0),
			WithChannelID(channelA),
		)
		if err != nil {
			t.Fatalf("创建节点2失败: %v", err)
		}
		defer node2.Stop()

		if err := node1.Start(); err != nil {
			t.Fatalf("启动节点1失败: %v", err)
		}
		if err := node2.Start(); err != nil {
			t.Fatalf("启动节点2失败: %v", err)
		}

		// 设置接收回调
		var receivedMsg string
		var mu sync.Mutex
		node2.OnReceive(func(peerID string, data []byte) {
			mu.Lock()
			defer mu.Unlock()
			receivedMsg = string(data)
			_ = peerID // unused
		})

		// 等待连接
		connected := make(chan struct{})
		node1.OnPeerConnected(func(peerID string) {
			close(connected)
		})

		addr := fmt.Sprintf("127.0.0.1:%d", node2.LocalAddr.Port)
		if err := node1.Connect(addr); err != nil {
			t.Fatalf("连接失败: %v", err)
		}

		select {
		case <-connected:
		case <-time.After(3 * time.Second):
			t.Fatal("连接超时")
		}

		// 等待频道同步
		time.Sleep(500 * time.Millisecond)

		// 发送消息到同频道
		testMsg := "Hello from same channel!"
		peers := node1.Peers.IDs()
		if err := node1.Send(channelA, peers[0], []byte(testMsg)); err != nil {
			t.Fatalf("发送失败: %v", err)
		}

		// 等待接收
		time.Sleep(500 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()
		if receivedMsg != testMsg {
			t.Errorf("同频道消息不匹配: 期望 %q, 实际 %q", testMsg, receivedMsg)
		} else {
			t.Logf("✓ 同频道通信成功: %s", receivedMsg)
		}
	})

	t.Run("不同频道隔离", func(t *testing.T) {
		channelA := "channel-a"
		channelB := "channel-b"

		node1, err := NewNode(
			WithNetworkPassword("test-secret"),
			WithListenPort(0),
			WithChannelID(channelA),
		)
		if err != nil {
			t.Fatalf("创建节点1失败: %v", err)
		}
		defer node1.Stop()

		node2, err := NewNode(
			WithNetworkPassword("test-secret"),
			WithListenPort(0),
			WithChannelID(channelB),
		)
		if err != nil {
			t.Fatalf("创建节点2失败: %v", err)
		}
		defer node2.Stop()

		if err := node1.Start(); err != nil {
			t.Fatalf("启动节点1失败: %v", err)
		}
		if err := node2.Start(); err != nil {
			t.Fatalf("启动节点2失败: %v", err)
		}

		// 设置接收回调
		var receivedMsg string
		var mu sync.Mutex
		node2.OnReceive(func(peerID string, data []byte) {
			mu.Lock()
			defer mu.Unlock()
			receivedMsg = string(data)
		})

		// 等待连接
		connected := make(chan struct{})
		node1.OnPeerConnected(func(peerID string) {
			close(connected)
		})

		addr := fmt.Sprintf("127.0.0.1:%d", node2.LocalAddr.Port)
		if err := node1.Connect(addr); err != nil {
			t.Fatalf("连接失败: %v", err)
		}

		select {
		case <-connected:
		case <-time.After(3 * time.Second):
			t.Fatal("连接超时")
		}

		// 等待频道同步
		time.Sleep(500 * time.Millisecond)

		// Node1 尝试向 channelB 发送消息（但 node1 不在 channelB）
		peers := node1.Peers.IDs()
		testMsg := "This should be rejected!"
		err = node1.Send(channelB, peers[0], []byte(testMsg))
		if err != nil {
			t.Logf("✓ 发送到未订阅频道的消息被拒绝: %v", err)
		}

		// 等待可能的接收
		time.Sleep(500 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()
		if receivedMsg == testMsg {
			t.Errorf("❌ 频道隔离失败！不同频道的节点收到了消息: %q", receivedMsg)
		} else {
			t.Logf("✓ 频道隔离生效：不同频道的节点无法通信")
		}
	})

	t.Run("广播消息隔离", func(t *testing.T) {
		channelA := "channel-a"
		channelB := "channel-b"

		// 创建3个节点
		node1, _ := NewNode(WithNetworkPassword("test-secret"), WithListenPort(0), WithChannelID(channelA))
		node2, _ := NewNode(WithNetworkPassword("test-secret"), WithListenPort(0), WithChannelID(channelA))
		node3, _ := NewNode(WithNetworkPassword("test-secret"), WithListenPort(0), WithChannelID(channelB))

		node1.Start()
		defer node1.Stop()
		node2.Start()
		defer node2.Stop()
		node3.Start()
		defer node3.Stop()

		// 记录接收情况
		var node2Received, node3Received bool
		var mu sync.Mutex

		node2.OnReceive(func(peerID string, data []byte) {
			mu.Lock()
			node2Received = true
			mu.Unlock()
		})
		node3.OnReceive(func(peerID string, data []byte) {
			mu.Lock()
			node3Received = true
			mu.Unlock()
		})

		// 连接所有节点
		connected := make(chan struct{}, 3)
		connHandler := func(peerID string) {
			select {
			case connected <- struct{}{}:
			default:
			}
		}
		node1.OnPeerConnected(connHandler)
		node2.OnPeerConnected(connHandler)
		node3.OnPeerConnected(connHandler)

		node1.Connect(fmt.Sprintf("127.0.0.1:%d", node2.LocalAddr.Port))
		node1.Connect(fmt.Sprintf("127.0.0.1:%d", node3.LocalAddr.Port))

		time.Sleep(1 * time.Second)

		// 广播消息到 channelA
		count, err := node1.Broadcast(channelA, []byte("Broadcast test"))
		if err != nil {
			t.Fatalf("广播失败: %v", err)
		}

		time.Sleep(500 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		t.Logf("广播发送给 %d 个节点", count)

		if node2Received && !node3Received {
			t.Logf("✓ 广播隔离正确：同频道节点 %s 收到消息，不同频道节点未收到", node2.ID()[:8])
		} else {
			t.Errorf("❌ 广播隔离失败：node2Received=%v, node3Received=%v", node2Received, node3Received)
		}
	})
}
