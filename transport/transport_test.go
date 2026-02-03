package transport

import (
	"context"
	"net"
	"testing"
)

func TestListenConfig(t *testing.T) {
	lc := ListenConfig()
	if lc == nil {
		t.Fatal("ListenConfig 返回 nil")
	}
	if lc.Control == nil {
		t.Error("Control 函数应该被设置")
	}
}

func TestDialConfig(t *testing.T) {
	localAddr := &net.TCPAddr{IP: net.IPv4zero, Port: 0}
	dc := DialConfig(localAddr)
	if dc == nil {
		t.Fatal("DialConfig 返回 nil")
	}
	if dc.LocalAddr != localAddr {
		t.Error("LocalAddr 未正确设置")
	}
	if dc.Control == nil {
		t.Error("Control 函数应该被设置")
	}
}

func TestListenConfigRealUse(t *testing.T) {
	lc := ListenConfig()

	// 使用 ListenConfig 创建真实的监听器
	listener, err := lc.Listen(nil, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen 失败: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr()
	if addr == nil {
		t.Error("监听器地址为 nil")
	}
}

func TestDialConfigRealUse(t *testing.T) {
	// 首先创建一个监听器
	lc := ListenConfig()
	listener, err := lc.Listen(nil, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	// 获取监听地址
	serverAddr := listener.Addr().String()

	// 使用 DialConfig 连接
	dc := DialConfig(nil)
	conn, err := dc.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer conn.Close()

	if conn.RemoteAddr().String() != serverAddr {
		t.Error("连接的远程地址不匹配")
	}
}

func TestTransportInterface(t *testing.T) {
	// 确保接口定义正确
	var _ Transport = (*mockTransport)(nil)
	var _ Listener = (*mockListener)(nil)
	var _ Conn = (*mockConn)(nil)
}

// Mock implementations for interface testing
type mockTransport struct{}

func (m *mockTransport) Listen(ctx context.Context, port int) (Listener, error) {
	return nil, nil
}
func (m *mockTransport) Dial(ctx context.Context, addr string) (Conn, error) {
	return nil, nil
}
func (m *mockTransport) Protocol() string { return "mock" }

type mockListener struct{ net.Listener }

func (m *mockListener) Accept() (Conn, error) { return nil, nil }

type mockConn struct{ net.Conn }
