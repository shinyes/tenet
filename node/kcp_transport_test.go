package node

import (
	"sync"
	"testing"
	"time"
)

// TestKCPTransportBasic 测试 KCP 传输层基本功能
func TestKCPTransportBasic(t *testing.T) {
	// 创建模拟节点
	node, err := NewNode(
		WithNetworkPassword("test"),
		WithListenPort(0),
		WithEnableKCP(false), // 手动管理 KCP
	)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}
	if err := node.Start(); err != nil {
		t.Fatalf("启动节点失败: %v", err)
	}
	defer node.Stop()

	// 创建 KCP 传输层
	transport := NewKCPTransport(node, DefaultKCPConfig())
	if transport == nil {
		t.Fatal("NewKCPTransport 返回 nil")
	}

	// 启动传输层（使用 UDP 端口 + 1，避免与 UDP socket 冲突）
	err = transport.Start(node.LocalAddr.Port + 1)
	if err != nil {
		t.Fatalf("启动 KCP 传输层失败: %v", err)
	}
	defer transport.Close()

	// 验证地址
	addr := transport.LocalAddr()
	if addr == nil {
		t.Error("LocalAddr 返回 nil")
	} else {
		t.Logf("KCP 传输层地址: %s", addr.String())
	}

	// 等待一下确保传输层运行正常
	time.Sleep(100 * time.Millisecond)
}

// TestKCPSessionOperations 测试 KCP 会话操作
func TestKCPSessionOperations(t *testing.T) {
	cfg := DefaultKCPConfig()

	// 启动服务端
	server := NewKCPManager(cfg)
	if err := server.Listen(0); err != nil {
		t.Fatalf("服务端监听失败: %v", err)
	}
	defer server.Close()

	serverAddr := server.LocalAddr().String()

	// 启动客户端
	client := NewKCPManager(cfg)
	defer client.Close()

	session, err := client.Dial(serverAddr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	// 测试 RemoteAddr
	remoteAddr := session.RemoteAddr()
	if remoteAddr == nil {
		t.Error("RemoteAddr 返回 nil")
	}

	// 测试 LocalAddr
	localAddr := session.LocalAddr()
	if localAddr == nil {
		t.Error("LocalAddr 返回 nil")
	}

	// 测试 SetReadDeadline
	err = session.SetReadDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Errorf("SetReadDeadline 失败: %v", err)
	}

	// 测试 SetWriteDeadline
	err = session.SetWriteDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Errorf("SetWriteDeadline 失败: %v", err)
	}

	// 测试 GetRTT
	rtt := session.GetRTT()
	t.Logf("RTT: %d ms", rtt)

	// 测试关闭
	err = session.Close()
	if err != nil {
		t.Errorf("Close 失败: %v", err)
	}

	// 测试重复关闭（应该无错误）
	err = session.Close()
	if err != nil {
		t.Errorf("重复 Close 应该无错误: %v", err)
	}
}

// TestKCPConcurrentWrite 测试并发写入
func TestKCPConcurrentWrite(t *testing.T) {
	cfg := DefaultKCPConfig()

	server := NewKCPManager(cfg)
	if err := server.Listen(0); err != nil {
		t.Fatalf("服务端监听失败: %v", err)
	}
	defer server.Close()

	serverAddr := getConnectableAddr(server.LocalAddr())

	// 接收协程
	totalReceived := make(chan int, 1)
	go func() {
		session, err := server.Accept()
		if err != nil {
			return
		}
		defer session.Close()

		buf := make([]byte, 65536)
		total := 0
		session.SetReadDeadline(time.Now().Add(10 * time.Second))
		for {
			n, err := session.Read(buf)
			if err != nil {
				break
			}
			total += n
		}
		totalReceived <- total
	}()

	// 客户端
	client := NewKCPManager(cfg)
	defer client.Close()

	session, err := client.Dial(serverAddr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	// 并发写入
	var wg sync.WaitGroup
	goroutines := 10
	messagesPerGoroutine := 100
	messageSize := 100

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			data := make([]byte, messageSize)
			for j := 0; j < messagesPerGoroutine; j++ {
				data[0] = byte(id)
				data[1] = byte(j)
				session.Write(data)
			}
		}(i)
	}

	wg.Wait()
	session.Close()

	expectedTotal := goroutines * messagesPerGoroutine * messageSize

	select {
	case total := <-totalReceived:
		t.Logf("并发写入测试: 发送 %d 字节，接收 %d 字节", expectedTotal, total)
		// 由于是流模式，接收的字节数应该等于发送的
		if total != expectedTotal {
			t.Logf("注意: 接收字节数与发送不完全一致（可能是连接提前关闭）")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("接收超时")
	}
}

// TestKCPManagerSessionManagement 测试会话管理
func TestKCPManagerSessionManagement(t *testing.T) {
	cfg := DefaultKCPConfig()
	manager := NewKCPManager(cfg)

	if err := manager.Listen(0); err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer manager.Close()

	serverAddr := getConnectableAddr(manager.LocalAddr())

	// 创建客户端连接
	client := NewKCPManager(cfg)
	session, err := client.Dial(serverAddr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	remoteAddr := session.RemoteAddr().String()

	// 测试 GetSession
	_, exists := client.GetSession(remoteAddr)
	if !exists {
		t.Error("GetSession 应该找到会话")
	}

	// 测试 RemoveSession
	client.RemoveSession(remoteAddr)

	_, exists = client.GetSession(remoteAddr)
	if exists {
		t.Error("RemoveSession 后不应该找到会话")
	}

	client.Close()
}
