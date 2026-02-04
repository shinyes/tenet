package transport

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestUDPMux_Dispatch(t *testing.T) {
	// 建立一个 UDP 监听
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// 创建 Mux
	mux := NewUDPMux(conn)
	mux.Start()
	defer mux.Close()

	// 客户端连接发送数据
	clientConn, err := net.DialUDP("udp", nil, conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	// 1. 发送 KCP 数据包 (非 PNCH)
	kcpData := []byte{0x00, 0x01, 0x02, 0x03}
	if _, err := clientConn.Write(kcpData); err != nil {
		t.Fatal(err)
	}

	// 2. 发送能够识别的打洞包 (PNCH 开头)
	punchData := []byte{0x50, 0x4E, 0x43, 0x48, 0x01, 0x02}
	if _, err := clientConn.Write(punchData); err != nil {
		t.Fatal(err)
	}

	// 验证 KCP 虚拟连接接收
	kcpConn := mux.GetKCPConn()
	buf := make([]byte, 1024)
	kcpConn.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := kcpConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("KCPConn 读取失败: %v", err)
	}
	if !bytes.Equal(buf[:n], kcpData) {
		t.Errorf("KCPConn 数据不匹配: got %x, want %x", buf[:n], kcpData)
	}

	// 验证打洞虚拟连接接收
	punchConn := mux.GetPunchConn()
	punchConn.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err = punchConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("PunchConn 读取失败: %v", err)
	}
	if !bytes.Equal(buf[:n], punchData) {
		t.Errorf("PunchConn 数据不匹配: got %x, want %x", buf[:n], punchData)
	}
}

func TestUDPMux_WriteTo(t *testing.T) {
	// 建立两个 UDP 监听模拟双方
	conn1, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	conn2, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer conn1.Close()
	defer conn2.Close()

	mux := NewUDPMux(conn1)
	// WriteTo 不需要 Start loop，因为它是直接调用底层的
	kcpConn := mux.GetKCPConn()

	data := []byte("hello")
	if _, err := kcpConn.WriteTo(data, conn2.LocalAddr()); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 1024)
	n, _, err := conn2.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], data) {
		t.Errorf("数据不匹配")
	}
}
