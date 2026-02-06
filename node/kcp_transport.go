package node

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/shinyes/tenet/transport"
)

// KCPTransport 封装 KCP 作为可靠传输层
// 在 Noise 握手完成后，可选择升级到 KCP 传输
type KCPTransport struct {
	manager  *KCPManager
	node     *Node
	mux      *transport.UDPMux
	sessions sync.Map // peerID -> *KCPSession
	closing  chan struct{}
	wg       sync.WaitGroup
}

// NewKCPTransport 创建 KCP 传输层
func NewKCPTransport(node *Node, config *KCPConfig, mux *transport.UDPMux) *KCPTransport {
	if config == nil {
		config = DefaultKCPConfig()
	}
	return &KCPTransport{
		manager: NewKCPManager(config),
		node:    node,
		mux:     mux,
		closing: make(chan struct{}),
	}
}

// Start 启动 KCP 传输层
// 注意：不再需要传递端口，因为复用了 Node 的 UDP 连接
func (t *KCPTransport) Start() error {
	if err := t.manager.ListenOnConn(t.mux.GetKCPConn()); err != nil {
		return fmt.Errorf("KCP 监听失败: %w", err)
	}

	t.wg.Add(1)
	go t.acceptLoop()

	return nil
}

// acceptLoop 接受新的 KCP 连接
func (t *KCPTransport) acceptLoop() {
	defer t.wg.Done()

	for {
		select {
		case <-t.closing:
			return
		default:
		}

		session, err := t.manager.Accept()
		if err != nil {
			select {
			case <-t.closing:
				return
			default:
				continue
			}
		}

		go t.handleSession(session)
	}
}

// handleSession 处理 KCP 会话
func (t *KCPTransport) handleSession(session *KCPSession) {
	defer session.Close()

	remoteAddr := session.RemoteAddr()
	t.node.Config.Logger.Debug("KCP 连接来自: %s", remoteAddr.String())

	// 读取循环
	header := make([]byte, 6) // 2字节长度 + 4字节 Magic
	buf := make([]byte, 65536)

	for {
		select {
		case <-t.closing:
			return
		default:
		}

		// 读取帧头
		if err := session.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			return
		}

		_, err := io.ReadFull(session, header[:2])
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}

		length := binary.BigEndian.Uint16(header[:2])
		if length == 0 {
			continue
		}

		// 读取完整帧
		if err := session.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return
		}

		_, err = io.ReadFull(session, buf[:length])
		if err != nil {
			return
		}

		// 验证协议魔术数
		if length < 5 || string(buf[0:4]) != MagicBytes {
			continue
		}

		packetType := buf[4]
		payload := make([]byte, length-5)
		copy(payload, buf[5:length])

		// 处理数据包
		t.node.handlePacket(nil, remoteAddr, "kcp", packetType, payload)
	}
}

// UpgradePeer 将对等节点升级到 KCP 传输
func (t *KCPTransport) UpgradePeer(peerID string, remoteAddr *net.UDPAddr) error {
	remoteAddrStr := remoteAddr.String()

	// 注册特定地址处理器，确保 Dial 的响应能回到这里
	handler := t.mux.RegisterKCPHandler(remoteAddrStr)

	session, err := t.manager.DialWithConn(remoteAddrStr, handler)
	if err != nil {
		t.mux.UnregisterKCPHandler(remoteAddrStr)
		return fmt.Errorf("KCP 连接失败: %w", err)
	}

	t.sessions.Store(peerID, session)
	t.node.Config.Logger.Info("节点 %s 已升级到 KCP 传输", peerID[:8])

	// 启动读取循环
	go t.handleSession(session)

	return nil
}

// Send 通过 KCP 发送数据
func (t *KCPTransport) Send(peerID string, data []byte) error {
	sessionI, ok := t.sessions.Load(peerID)
	if !ok {
		return fmt.Errorf("KCP 会话不存在: %s", peerID)
	}
	session := sessionI.(*KCPSession)

	// 帧格式: [长度(2)] [数据]
	frame := make([]byte, 2+len(data))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(data)))
	copy(frame[2:], data)

	_, err := session.Write(frame)
	return err
}

// HasSession 检查是否有 KCP 会话
func (t *KCPTransport) HasSession(peerID string) bool {
	_, ok := t.sessions.Load(peerID)
	return ok
}

// RemoveSession 移除 KCP 会话
func (t *KCPTransport) RemoveSession(peerID string) {
	if sessionI, ok := t.sessions.LoadAndDelete(peerID); ok {
		session := sessionI.(*KCPSession)
		raddr := session.RemoteAddr().String()
		session.Close()

		// 注销 Mux 上的处理器
		t.mux.UnregisterKCPHandler(raddr)
	}
}

// GetRTT 获取指定对等节点的 RTT
func (t *KCPTransport) GetRTT(peerID string) int32 {
	if sessionI, ok := t.sessions.Load(peerID); ok {
		session := sessionI.(*KCPSession)
		return session.GetRTT()
	}
	return 0
}

// Close 关闭 KCP 传输层
func (t *KCPTransport) Close() error {
	close(t.closing)

	// 先关闭 manager 以解除 Accept 阻塞
	err := t.manager.Close()

	// 关闭所有会话
	t.sessions.Range(func(key, value interface{}) bool {
		session := value.(*KCPSession)
		raddr := session.RemoteAddr().String()
		session.Close()
		t.mux.UnregisterKCPHandler(raddr)
		t.sessions.Delete(key)
		return true
	})

	t.wg.Wait()
	return err
}

// LocalAddr 返回 KCP 监听地址
func (t *KCPTransport) LocalAddr() net.Addr {
	return t.manager.LocalAddr()
}
