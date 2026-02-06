package node

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/shinyes/tenet/crypto"
)

func createTestNode(t *testing.T, port int, channel string) *Node {
	t.Helper()
	id, err := crypto.NewIdentity()
	if err != nil {
		t.Fatalf("创建身份失败: %v", err)
	}

	opts := []Option{
		WithListenPort(port),
		WithIdentity(id),
		WithNetworkPassword("test-net"),
		WithEnableRelay(false),
		WithEnableHolePunch(false), // 测试本地连接，不需要打洞
	}

	if channel != "" {
		opts = append(opts, WithChannelID(channel))
	}

	node, err := NewNode(opts...)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	if err := node.Start(); err != nil {
		t.Fatalf("启动节点失败: %v", err)
	}

	return node
}

func TestNodeChannelIsolation(t *testing.T) {
	// 场景1: 相同频道 -> 连接成功
	t.Run("Same Channel", func(t *testing.T) {
		channel := "secret-channel"
		node1 := createTestNode(t, 0, channel) // let system pick port
		defer node1.Stop()
		node2 := createTestNode(t, 0, channel)
		defer node2.Stop()

		// 获取真实监听端口，并将绑定的通配符地址替换为本地回环
		_, port, _ := net.SplitHostPort(node2.LocalAddr.String())
		addr2 := fmt.Sprintf("127.0.0.1:%s", port)

		// Node1 连接 Node2
		err := node1.Connect(addr2)
		if err != nil {
			t.Fatalf("连接失败: %v", err)
		}

		// 等待握手完成 (最多 3 秒)
		checkForPeers := func() bool {
			_, ok1 := node1.Peers.Get(node2.ID())
			_, ok2 := node2.Peers.Get(node1.ID())
			return ok1 && ok2
		}

		success := false
		for i := 0; i < 30; i++ {
			if checkForPeers() {
				success = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if !success {
			t.Error("节点未能建立连接 (超时)")
		}
	})

	// 场景2: 不同频道 -> 连接也应该成功 (Global Topology)
	t.Run("Different Channel Connect", func(t *testing.T) {
		node1 := createTestNode(t, 0, "channel-A")
		defer node1.Stop()
		node2 := createTestNode(t, 0, "channel-B")
		defer node2.Stop()

		_, port, _ := net.SplitHostPort(node2.LocalAddr.String())
		addr2 := fmt.Sprintf("127.0.0.1:%s", port)

		node1.Connect(addr2)

		// 应该能够连接成功
		success := false
		for i := 0; i < 20; i++ {
			if _, ok := node1.Peers.Get(node2.ID()); ok {
				success = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if !success {
			t.Error("不同频道的节点应该能够建立物理连接 (因为是全局拓扑)")
		}
	})
}
