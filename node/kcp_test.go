package node

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// getConnectableAddr 将监听地址转换为可连接的地址
// 例如 [::]:1234 或 0.0.0.0:1234 转换为 127.0.0.1:1234
func getConnectableAddr(addr net.Addr) string {
	tcpAddr, ok := addr.(*net.UDPAddr)
	if ok {
		return fmt.Sprintf("127.0.0.1:%d", tcpAddr.Port)
	}
	// 解析地址字符串
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return fmt.Sprintf("127.0.0.1:%s", port)
	}
	return addr.String()
}

// TestDefaultKCPConfig 测试默认 KCP 配置
func TestDefaultKCPConfig(t *testing.T) {
	cfg := DefaultKCPConfig()

	if cfg == nil {
		t.Fatal("DefaultKCPConfig 返回 nil")
	}

	// 验证平衡模式默认值
	if cfg.NoDelay != 0 {
		t.Errorf("NoDelay 期望 0，实际 %d", cfg.NoDelay)
	}
	if cfg.Interval != 30 {
		t.Errorf("Interval 期望 30，实际 %d", cfg.Interval)
	}
	if cfg.Resend != 2 {
		t.Errorf("Resend 期望 2，实际 %d", cfg.Resend)
	}
	if cfg.NC != 1 {
		t.Errorf("NC 期望 1，实际 %d", cfg.NC)
	}
	if cfg.SndWnd != 64 {
		t.Errorf("SndWnd 期望 64，实际 %d", cfg.SndWnd)
	}
	if cfg.RcvWnd != 64 {
		t.Errorf("RcvWnd 期望 64，实际 %d", cfg.RcvWnd)
	}
	if cfg.MTU != 1350 {
		t.Errorf("MTU 期望 1350，实际 %d", cfg.MTU)
	}
	if !cfg.StreamMode {
		t.Error("StreamMode 期望 true")
	}
}

// TestKCPManager 测试 KCP 管理器基本功能
func TestKCPManager(t *testing.T) {
	cfg := DefaultKCPConfig()
	manager := NewKCPManager(cfg)

	if manager == nil {
		t.Fatal("NewKCPManager 返回 nil")
	}

	// 测试监听
	err := manager.Listen(0) // 随机端口
	if err != nil {
		t.Fatalf("KCP 监听失败: %v", err)
	}

	addr := manager.LocalAddr()
	if addr == nil {
		t.Fatal("LocalAddr 返回 nil")
	}
	t.Logf("KCP 监听地址: %s", addr.String())

	// 关闭
	err = manager.Close()
	if err != nil {
		t.Errorf("KCP 关闭失败: %v", err)
	}
}

// TestKCPClientServer 测试 KCP 客户端-服务端通信
func TestKCPClientServer(t *testing.T) {
	cfg := DefaultKCPConfig()

	// 启动服务端
	server := NewKCPManager(cfg)
	err := server.Listen(0)
	if err != nil {
		t.Fatalf("服务端监听失败: %v", err)
	}
	defer server.Close()

	serverAddr := getConnectableAddr(server.LocalAddr())
	t.Logf("服务端地址: %s", serverAddr)

	// 用于接收结果
	received := make(chan []byte, 1)
	testData := []byte("Hello KCP!")
	serverReady := make(chan struct{})

	// 启动服务端接收协程
	go func() {
		close(serverReady)
		session, err := server.Accept()
		if err != nil {
			t.Logf("Accept 失败: %v", err)
			return
		}
		defer session.Close()

		buf := make([]byte, 1024)
		session.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, err := session.Read(buf)
		if err != nil {
			t.Logf("读取失败: %v", err)
			return
		}
		received <- buf[:n]
	}()

	// 等待服务端准备好
	<-serverReady
	time.Sleep(100 * time.Millisecond)

	// 启动客户端
	client := NewKCPManager(cfg)
	defer client.Close()

	session, err := client.Dial(serverAddr)
	if err != nil {
		t.Fatalf("客户端连接失败: %v", err)
	}
	defer session.Close()

	// 发送数据
	_, err = session.Write(testData)
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	// 等待接收
	select {
	case data := <-received:
		if string(data) != string(testData) {
			t.Errorf("数据不匹配: 期望 %q，实际 %q", testData, data)
		} else {
			t.Logf("成功收到数据: %s", data)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("接收超时")
	}
}

// TestKCPReliability 测试 KCP 可靠性（大数据量传输）
func TestKCPReliability(t *testing.T) {
	cfg := DefaultKCPConfig()

	// 启动服务端
	server := NewKCPManager(cfg)
	err := server.Listen(0)
	if err != nil {
		t.Fatalf("服务端监听失败: %v", err)
	}
	defer server.Close()

	serverAddr := getConnectableAddr(server.LocalAddr())

	// 生成测试数据（256KB，减小以加快测试）
	dataSize := 256 * 1024
	testData := make([]byte, dataSize)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	// 用于接收结果
	received := make(chan []byte, 1)
	errChan := make(chan error, 1)
	serverReady := make(chan struct{})

	// 启动服务端接收协程
	go func() {
		close(serverReady)
		session, err := server.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer session.Close()

		buf := make([]byte, dataSize+1024)
		totalRead := 0
		session.SetReadDeadline(time.Now().Add(30 * time.Second))

		for totalRead < dataSize {
			n, err := session.Read(buf[totalRead:])
			if err != nil {
				errChan <- err
				return
			}
			totalRead += n
		}
		received <- buf[:totalRead]
	}()

	// 等待服务端准备好
	<-serverReady
	time.Sleep(100 * time.Millisecond)

	// 启动客户端
	client := NewKCPManager(cfg)
	defer client.Close()

	session, err := client.Dial(serverAddr)
	if err != nil {
		t.Fatalf("客户端连接失败: %v", err)
	}
	defer session.Close()

	// 分块发送数据
	chunkSize := 32 * 1024 // 32KB per chunk
	sent := 0
	for sent < dataSize {
		end := sent + chunkSize
		if end > dataSize {
			end = dataSize
		}
		n, err := session.Write(testData[sent:end])
		if err != nil {
			t.Fatalf("发送失败: %v", err)
		}
		sent += n
	}
	t.Logf("已发送 %d 字节", sent)

	// 等待接收
	select {
	case data := <-received:
		if len(data) != dataSize {
			t.Errorf("数据长度不匹配: 期望 %d，实际 %d", dataSize, len(data))
		} else {
			// 验证数据完整性
			for i := 0; i < dataSize; i++ {
				if data[i] != testData[i] {
					t.Errorf("数据在位置 %d 不匹配: 期望 %d，实际 %d", i, testData[i], data[i])
					break
				}
			}
			t.Logf("成功传输 %d 字节，数据完整", dataSize)
		}
	case err := <-errChan:
		t.Fatalf("服务端错误: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("接收超时")
	}
}

// TestKCPMultipleMessages 测试多条消息的可靠传输
// 注意：KCP 在流模式下会合并小消息，因此使用总字节数验证
func TestKCPMultipleMessages(t *testing.T) {
	cfg := DefaultKCPConfig()

	// 启动服务端
	server := NewKCPManager(cfg)
	err := server.Listen(0)
	if err != nil {
		t.Fatalf("服务端监听失败: %v", err)
	}
	defer server.Close()

	serverAddr := getConnectableAddr(server.LocalAddr())
	messageCount := 50
	messageSize := 100
	expectedBytes := messageCount * messageSize

	// 用于接收结果
	totalReceived := make(chan int, 1)
	errChan := make(chan error, 1)
	serverReady := make(chan struct{})

	// 启动服务端接收协程
	go func() {
		close(serverReady)
		session, err := server.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer session.Close()

		buf := make([]byte, 65536)
		total := 0
		session.SetReadDeadline(time.Now().Add(10 * time.Second))

		for total < expectedBytes {
			n, err := session.Read(buf)
			if err != nil {
				// 如果已经收到足够数据，不算错误
				if total >= expectedBytes {
					break
				}
				errChan <- err
				return
			}
			total += n
		}
		totalReceived <- total
	}()

	// 等待服务端准备好
	<-serverReady
	time.Sleep(100 * time.Millisecond)

	// 启动客户端
	client := NewKCPManager(cfg)
	defer client.Close()

	session, err := client.Dial(serverAddr)
	if err != nil {
		t.Fatalf("客户端连接失败: %v", err)
	}
	defer session.Close()

	// 发送多条消息
	sentBytes := 0
	for i := 0; i < messageCount; i++ {
		msg := make([]byte, messageSize)
		msg[0] = byte(i >> 24)
		msg[1] = byte(i >> 16)
		msg[2] = byte(i >> 8)
		msg[3] = byte(i)
		n, err := session.Write(msg)
		if err != nil {
			t.Fatalf("发送消息 %d 失败: %v", i, err)
		}
		sentBytes += n
	}
	t.Logf("已发送 %d 条消息，共 %d 字节", messageCount, sentBytes)

	// 验证接收的总字节数
	select {
	case total := <-totalReceived:
		if total != expectedBytes {
			t.Errorf("接收字节数不匹配: 期望 %d，实际 %d", expectedBytes, total)
		} else {
			t.Logf("成功接收 %d 字节", total)
		}
	case err := <-errChan:
		t.Fatalf("服务端错误: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("接收超时")
	}
}
