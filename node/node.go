package node

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/cykyes/tenet/crypto"
	"github.com/cykyes/tenet/internal/pool"
	"github.com/cykyes/tenet/internal/protocol"
	"github.com/cykyes/tenet/metrics"
	"github.com/cykyes/tenet/nat"
	"github.com/cykyes/tenet/peer"
	"github.com/cykyes/tenet/transport"
)

// 协议常量
const (
	// MagicBytes 协议魔术数
	MagicBytes = "TENT"

	// 包类型常量
	PacketTypeHandshake     = 0x01 // 握手
	PacketTypeData          = 0x02 // 加密数据
	PacketTypeRelay         = 0x03 // 中继封装
	PacketTypeDiscoveryReq  = 0x04 // 节点发现请求
	PacketTypeDiscoveryResp = 0x05 // 节点发现响应
	PacketTypeHeartbeat     = 0x06 // 心跳请求
	PacketTypeHeartbeatAck  = 0x07 // 心跳响应

	// 中继模式
	RelayModeForward = 0x01 // 请求转发
	RelayModeTarget  = 0x02 // 目标侧接收
)

// Node 表示一个 P2P 节点
type Node struct {
	Config   *Config
	Identity *crypto.Identity
	Peers    *peer.PeerStore

	conn        *net.UDPConn
	tcpListener *net.TCPListener
	LocalAddr   *net.UDPAddr
	PublicAddr  *net.UDPAddr // Can be set after NAT discovery

	localPeerID       string // 本节点的 PeerID，用于节点发现时过滤
	pendingHandshakes map[string]*crypto.HandshakeState
	addrToPeer        map[string]string // Addr.String() -> PeerID

	onReceive          func(peerID string, data []byte)
	onPeerConnected    func(peerID string)
	onPeerDisconnected func(peerID string)
	mu                 sync.RWMutex

	closing chan struct{}
	wg      sync.WaitGroup

	relayManager       *nat.RelayManager
	relayAddrSet       map[string]bool
	relayForward       map[string]*net.UDPAddr
	relayPendingTarget map[string]*net.UDPAddr // relayAddr -> targetAddr
	relayInbound       map[string]bool

	metrics   *metrics.Collector      // 指标收集器
	natProber *nat.NATProber          // NAT 探测器
	natInfo   *nat.NATInfo            // 本机 NAT 信息
	relayAuth *nat.RelayAuthenticator // 中继认证器（每个 Node 实例独立）

	// KCP 可靠 UDP 传输层
	kcpTransport *KCPTransport

	// 重连管理器
	reconnectManager *ReconnectManager

	// 重连回调
	onReconnecting func(peerID string, attempt int, nextRetryIn time.Duration)
	onReconnected  func(peerID string, attempts int)
	onGaveUp       func(peerID string, attempts int, lastErr error)
}

// NewNode 创建一个新的 Node 实例
func NewNode(opts ...Option) (*Node, error) {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置无效: %w", err)
	}

	// 根据配置决定是加载还是创建身份
	var id *crypto.Identity
	var err error
	if cfg.IdentityPath != "" {
		id, err = crypto.LoadOrCreateIdentity(cfg.IdentityPath)
	} else {
		id, err = crypto.NewIdentity()
	}
	if err != nil {
		return nil, fmt.Errorf("创建身份失败: %w", err)
	}

	return &Node{
		Config:             cfg,
		Identity:           id,
		Peers:              peer.NewPeerStore(),
		pendingHandshakes:  make(map[string]*crypto.HandshakeState),
		addrToPeer:         make(map[string]string),
		closing:            make(chan struct{}),
		relayAddrSet:       make(map[string]bool),
		relayForward:       make(map[string]*net.UDPAddr),
		relayPendingTarget: make(map[string]*net.UDPAddr),
		relayInbound:       make(map[string]bool),
		metrics:            metrics.NewCollector(),
	}, nil
}

// ID 返回本地节点 ID
func (n *Node) ID() string {
	return n.Identity.ID.String()
}

// Start 启动节点监听
func (n *Node) Start() error {
	// 确定监听地址
	listenAddr := fmt.Sprintf(":%d", n.Config.ListenPort)

	// UDP 监听
	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("解析地址失败: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("监听失败: %w", err)
	}

	n.conn = conn
	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		conn.Close()
		return fmt.Errorf("意外的地址类型: %T", conn.LocalAddr())
	}
	n.LocalAddr = udpAddr

	// 计算本节点的 PeerID（与 processHandshake 中的计算方式一致）
	idHash := sha256.Sum256(n.Identity.NoisePublicKey[:])
	n.localPeerID = fmt.Sprintf("%x", idHash[:16])

	// 所有节点都初始化 relayManager，以便在需要时使用中继
	// EnableRelay 仅控制是否作为中继服务器（响应转发请求）
	n.relayManager = nat.NewRelayManager(n.conn)
	for _, addrStr := range n.Config.RelayNodes {
		relayAddr, err := net.ResolveUDPAddr("udp", addrStr)
		if err != nil {
			n.Config.Logger.Error("中继地址解析失败: %s: %v", addrStr, err)
			continue
		}
		n.relayManager.AddRelay(addrStr, relayAddr)
		n.relayAddrSet[relayAddr.String()] = true
	}

	// 启动 TCP 监听（用于接入连接与打洞基础）
	// 使用 transport.ListenConfig（SO_REUSEADDR）以便打洞逻辑也能绑定该端口
	lc := transport.ListenConfig()
	listener, err := lc.Listen(context.Background(), "tcp", listenAddr)
	if err != nil {
		conn.Close()
		return fmt.Errorf("TCP 监听失败: %w", err)
	}
	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		listener.Close()
		conn.Close()
		return fmt.Errorf("意外的监听器类型: %T", listener)
	}
	n.tcpListener = tcpListener

	// 初始化 NAT 探测器
	n.natProber = nat.NewNATProber(n.conn)

	// 启动 KCP 可靠 UDP 传输层（如果启用）
	if n.Config.EnableKCP {
		n.kcpTransport = NewKCPTransport(n, n.Config.KCPConfig)
		if err := n.kcpTransport.Start(n.LocalAddr.Port); err != nil {
			n.Config.Logger.Warn("KCP 启动失败，将使用原始 UDP: %v", err)
			n.kcpTransport = nil
		} else {
			n.Config.Logger.Info("KCP 可靠传输层已启动，端口 %d", n.LocalAddr.Port+1)
		}
	}

	// 初始化重连管理器（如果启用）
	if n.Config.EnableReconnect {
		reconnectCfg := n.Config.ReconnectConfig
		if reconnectCfg == nil {
			reconnectCfg = DefaultReconnectConfig()
		}
		n.reconnectManager = NewReconnectManager(n, reconnectCfg)

		// 设置重连回调
		n.mu.RLock()
		if n.onReconnecting != nil {
			n.reconnectManager.SetOnReconnecting(n.onReconnecting)
		}
		if n.onReconnected != nil {
			n.reconnectManager.SetOnReconnected(n.onReconnected)
		}
		if n.onGaveUp != nil {
			n.reconnectManager.SetOnGaveUp(n.onGaveUp)
		}
		n.mu.RUnlock()

		n.Config.Logger.Info("重连管理器已启用，最大重试 %d 次", reconnectCfg.MaxRetries)
	}

	n.wg.Add(3) // 1 个用于 UDP 读，1 个用于 TCP Accept，1 个用于心跳
	go n.handleRead()
	go n.acceptTCP()
	go n.heartbeatLoop()

	n.Config.Logger.Info("节点已启动，监听 %s (ID: %s)", listenAddr, n.localPeerID[:8])
	return nil
}

// Stop 停止节点
func (n *Node) Stop() error {
	return n.GracefulStop(context.Background())
}

// GracefulStop 优雅关闭节点
// 向所有对端发送关闭通知，等待现有数据发送完成，然后关闭连接
func (n *Node) GracefulStop(ctx context.Context) error {
	select {
	case <-n.closing:
		return nil
	default:
	}

	n.Config.Logger.Info("正在优雅关闭节点...")

	// 通知所有对端我们即将关闭
	peerIDs := n.Peers.IDs()
	for _, peerID := range peerIDs {
		p, ok := n.Peers.Get(peerID)
		if !ok {
			continue
		}
		p.SetState(peer.StateDisconnecting)
		// 发送关闭通知（使用特殊的心跳包）
		transport, addr, conn := p.GetTransportInfo()
		goodbyePacket := n.buildGoodbyePacket()
		n.sendRaw(conn, addr, transport, goodbyePacket)
	}

	// 等待一小段时间让关闭通知发送出去
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
	}

	// 关闭节点
	close(n.closing)

	if n.conn != nil {
		n.conn.Close()
	}
	if n.tcpListener != nil {
		n.tcpListener.Close()
	}

	// 关闭 NAT 探测器，停止清理协程
	if n.natProber != nil {
		n.natProber.Close()
	}

	// 关闭 KCP 传输层
	if n.kcpTransport != nil {
		n.kcpTransport.Close()
	}

	// 关闭重连管理器
	if n.reconnectManager != nil {
		n.reconnectManager.Close()
	}

	// 关闭所有对端连接
	for _, peerID := range peerIDs {
		if p, ok := n.Peers.Get(peerID); ok {
			p.Close()
		}
	}

	n.wg.Wait()
	n.Config.Logger.Info("节点已关闭")
	return nil
}

// buildGoodbyePacket 构建关闭通知包
func (n *Node) buildGoodbyePacket() []byte {
	// 使用特殊的心跳包类型作为关闭通知
	packet := make([]byte, 6)
	copy(packet[0:4], []byte(MagicBytes))
	packet[4] = PacketTypeHeartbeat
	packet[5] = 0xFF // 特殊标记表示即将关闭
	return packet
}

// Connect 通过 TCP/UDP 打洞发起连接
func (n *Node) Connect(addrStr string) error {
	if n.conn == nil || n.LocalAddr == nil {
		return fmt.Errorf("节点未启动")
	}
	rUDPAddr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		return err
	}
	rTCPAddr, err := net.ResolveTCPAddr("tcp", addrStr)
	if err != nil {
		return err
	}

	// 准备握手数据（UDP）
	hsUDP, msgUDP, err := crypto.NewInitiatorHandshake(
		n.Identity.NoisePrivateKey[:],
		n.Identity.NoisePublicKey[:],
		[]byte(n.Config.NetworkPassword),
	)
	if err != nil {
		return err
	}

	// 准备握手数据（TCP）
	hsTCP, msgTCP, err := crypto.NewInitiatorHandshake(
		n.Identity.NoisePrivateKey[:],
		n.Identity.NoisePublicKey[:],
		[]byte(n.Config.NetworkPassword),
	)
	if err != nil {
		return err
	}

	// 构造握手包（UDP）
	packetUDP := make([]byte, 5+len(msgUDP))
	copy(packetUDP[0:4], []byte("TENT"))
	packetUDP[4] = PacketTypeHandshake
	copy(packetUDP[5:], msgUDP)

	// 构造握手包（TCP）
	packetTCP := make([]byte, 5+len(msgTCP))
	copy(packetTCP[0:4], []byte("TENT"))
	packetTCP[4] = PacketTypeHandshake
	copy(packetTCP[5:], msgTCP)

	// 注册待处理握手状态（发起方），按传输前缀区分
	udpStateKey := "udp://" + rUDPAddr.String()
	tcpStateKey := "tcp://" + rTCPAddr.String()

	n.mu.Lock()
	n.pendingHandshakes[udpStateKey] = hsUDP
	if rTCPAddr != nil {
		n.pendingHandshakes[tcpStateKey] = hsTCP
	}
	n.mu.Unlock()

	// 设置握手超时清理（30秒后自动清理未完成的握手状态）
	go func() {
		time.Sleep(30 * time.Second)
		n.mu.Lock()
		delete(n.pendingHandshakes, udpStateKey)
		delete(n.pendingHandshakes, tcpStateKey)
		n.mu.Unlock()
	}()

	// --- 策略：TCP 与 UDP 同时尝试 ---

	type ConnectResult struct {
		Conn      net.Conn
		Transport string
		Err       error
	}
	resultChan := make(chan ConnectResult, 2)

	// 使用可取消的 context，在节点关闭时取消打洞
	punchCtx, punchCancel := context.WithCancel(context.Background())
	defer punchCancel() // 确保在函数返回时取消 context

	go func() {
		select {
		case <-n.closing:
			punchCancel()
		case <-punchCtx.Done():
			// context 已取消，退出
		case <-time.After(15 * time.Second):
			punchCancel()
		}
	}()

	// 1. TCP 打洞
	go func() {
		ctx, cancel := context.WithTimeout(punchCtx, 10*time.Second)
		defer cancel()

		puncher := nat.NewTCPHolePuncher()
		conn, err := puncher.Punch(ctx, n.LocalAddr.Port, rTCPAddr)
		if err != nil {
			resultChan <- ConnectResult{Err: err}
			return
		}
		resultChan <- ConnectResult{Conn: conn, Transport: "tcp"}
	}()

	// 2. UDP 打洞（简单发送）
	go func() {
		timeout := time.After(2 * time.Second)
		for {
			select {
			case <-punchCtx.Done():
				return
			case <-timeout:
				resultChan <- ConnectResult{Transport: "udp"}
				return
			default:
				n.conn.WriteToUDP(packetUDP, rUDPAddr)
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()

	// 等待第一个可用结果
	select {
	case res := <-resultChan:
		if res.Transport == "tcp" && res.Conn != nil {
			// TCP 成功
			length := uint16(len(packetTCP))
			frame := make([]byte, 2+len(packetTCP))
			frame[0] = byte(length >> 8)
			frame[1] = byte(length)
			copy(frame[2:], packetTCP)

			_, err := res.Conn.Write(frame)
			if err != nil {
				res.Conn.Close()
				return err
			}

			go n.handleTCP(res.Conn)
			return nil
		} else if res.Transport == "udp" {
			// UDP 已发送，处理可能的 TCP 迟到成功
			if n.Config.EnableRelay {
				addrKey := rUDPAddr.String()
				go func() {
					select {
					case <-n.closing:
						return
					case <-time.After(n.Config.DialTimeout):
					}
					n.mu.RLock()
					_, ok := n.addrToPeer[addrKey]
					n.mu.RUnlock()
					if !ok {
						n.connectViaRelay(addrStr)
					}
				}()
			}
			go func() {
				timeout := time.After(10 * time.Second)
				for {
					select {
					case <-n.closing:
						// 节点关闭，清理资源
						select {
						case res2 := <-resultChan:
							if res2.Conn != nil {
								res2.Conn.Close()
							}
						default:
						}
						return
					case res2 := <-resultChan:
						if res2.Transport == "tcp" && res2.Conn != nil {
							length := uint16(len(packetTCP))
							frame := make([]byte, 2+len(packetTCP))
							frame[0] = byte(length >> 8)
							frame[1] = byte(length)
							copy(frame[2:], packetTCP)

							_, err := res2.Conn.Write(frame)
							if err != nil {
								res2.Conn.Close()
								return
							}
							go n.handleTCP(res2.Conn)
							return
						} else if res2.Conn != nil {
							res2.Conn.Close()
						}
					case <-timeout:
						select {
						case res2 := <-resultChan:
							if res2.Conn != nil {
								res2.Conn.Close()
							}
						default:
						}
						return
					}
				}
			}()
			return nil
		}
		if res.Err != nil {
			if n.relayManager != nil {
				if err := n.connectViaRelay(addrStr); err == nil {
					return nil
				}
			}
			return res.Err
		}
		return fmt.Errorf("连接失败")
	case <-time.After(5 * time.Second):
		if n.relayManager != nil {
			if err := n.connectViaRelay(addrStr); err == nil {
				return nil
			}
		}
		return fmt.Errorf("连接超时")
	}
}

// Send 向对等节点发送数据
func (n *Node) Send(peerID string, data []byte) error {
	// 检查是否尝试向自己发送
	if peerID == n.ID() {
		return fmt.Errorf("不能向本节点发送数据")
	}

	p, ok := n.Peers.Get(peerID)
	if !ok {
		return fmt.Errorf("未找到对等节点: %s", peerID)
	}
	if p.Session == nil {
		return fmt.Errorf("对等节点会话未建立")
	}

	encrypted, err := p.Session.Encrypt(data)
	if err != nil {
		return err
	}

	// 包格式: [Magic(4)] [Type(1)] [Data]
	packet := make([]byte, 5+len(encrypted))
	copy(packet[0:4], []byte("TENT"))
	packet[4] = PacketTypeData
	copy(packet[5:], encrypted)

	// 更新流量统计
	p.AddBytesSent(int64(len(data)))
	if n.metrics != nil {
		n.metrics.AddBytesSent(int64(len(data)))
	}

	transport, addr, conn := p.GetTransportInfo()

	// TCP 传输（可靠）
	if transport == "tcp" && conn != nil {
		length := uint16(len(packet))
		frame := make([]byte, 2+len(packet))
		frame[0] = byte(length >> 8)
		frame[1] = byte(length)
		copy(frame[2:], packet)
		_, err := conn.Write(frame)
		return err
	}

	// KCP 传输（可靠 UDP）
	if transport == "kcp" && n.kcpTransport != nil && n.kcpTransport.HasSession(peerID) {
		return n.kcpTransport.Send(peerID, packet)
	}

	// 尝试升级到 KCP（如果启用且是 UDP 模式）
	if transport == "udp" && n.kcpTransport != nil && !n.kcpTransport.HasSession(peerID) {
		if udpAddr, ok := addr.(*net.UDPAddr); ok {
			// 异步升级到 KCP，当前数据仍通过 UDP 发送
			go func() {
				if err := n.kcpTransport.UpgradePeer(peerID, udpAddr); err != nil {
					n.Config.Logger.Debug("KCP 升级失败: %v", err)
				} else {
					// 升级成功，更新 peer 传输类型
					if peer, ok := n.Peers.Get(peerID); ok {
						peer.UpgradeTransport(addr, nil, "kcp", peer.Session)
					}
				}
			}()
		}
	}

	if p.LinkMode == "relay" {
		if relayAddr, ok := addr.(*net.UDPAddr); ok {
			if p.RelayTarget != nil {
				relayPacket, err := n.buildRelayPacket(RelayModeForward, p.RelayTarget, packet)
				if err != nil {
					return err
				}
				_, err = n.conn.WriteToUDP(relayPacket, relayAddr)
				return err
			}
			_, err = n.conn.WriteToUDP(packet, relayAddr)
			return err
		}
	}

	// 回退到 UDP
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return fmt.Errorf("对等节点的 UDP 地址无效")
	}
	_, err = n.conn.WriteToUDP(packet, udpAddr)
	return err
}

// acceptTCP 接收 TCP 入站连接
func (n *Node) acceptTCP() {
	defer n.wg.Done()
	if n.tcpListener == nil {
		return
	}

	for {
		conn, err := n.tcpListener.Accept()
		if err != nil {
			select {
			case <-n.closing:
				return
			default:
				continue
			}
		}
		go n.handleTCP(conn)
	}
}

// handleTCP 处理 TCP 入站连接
func (n *Node) handleTCP(conn net.Conn) {
	defer conn.Close()

	// 设置 TCP KeepAlive
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	// 使用缓冲池
	header := make([]byte, 2)
	frameBuf := pool.GetLargeBuffer()
	defer pool.PutLargeBuffer(frameBuf)

	for {
		_, err := io.ReadFull(conn, header)
		if err != nil {
			if err != io.EOF {
				n.Config.Logger.Error("从 %s 读取 TCP 数据出错: %v", conn.RemoteAddr(), err)
			}
			return
		}

		length := uint16(header[0])<<8 | uint16(header[1])
		if length == 0 {
			return
		}

		// 动态分配缓冲区（如果超过预分配大小）
		var buf []byte
		if int(length) <= len(*frameBuf) {
			buf = (*frameBuf)[:length]
		} else {
			buf = make([]byte, length)
		}

		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return
		}

		if length < 5 || string(buf[0:4]) != "TENT" {
			continue
		}

		packetType := buf[4]
		payload := make([]byte, length-5)
		copy(payload, buf[5:])

		n.handlePacket(conn, conn.RemoteAddr(), "tcp", packetType, payload)
	}
}

// 回调设置
func (n *Node) OnReceive(f func(string, []byte)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onReceive = f
}

func (n *Node) OnPeerConnected(f func(string)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onPeerConnected = f
}

func (n *Node) OnPeerDisconnected(f func(string)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onPeerDisconnected = f
}

// handleRead 处理 UDP 入站数据包
func (n *Node) handleRead() {
	defer n.wg.Done()

	// 使用缓冲池
	bufPtr := pool.GetLargeBuffer()
	defer pool.PutLargeBuffer(bufPtr)
	buf := *bufPtr

	for {
		select {
		case <-n.closing:
			return
		default:
		}

		n.conn.SetReadDeadline(time.Now().Add(time.Second))
		count, addr, err := n.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-n.closing:
				return
			default:
			}
			continue
		}

		if count < 5 {
			continue
		}
		if string(buf[0:4]) != "TENT" {
			continue
		}

		packetType := buf[4]
		payload := make([]byte, count-5)
		copy(payload, buf[5:count])

		if packetType == PacketTypeRelay {
			n.handleRelayPacket(addr, payload)
			continue
		}

		if n.Config.EnableRelay {
			n.mu.RLock()
			originAddr, forward := n.relayForward[addr.String()]
			n.mu.RUnlock()
			if forward && originAddr != nil {
				n.conn.WriteToUDP(buf[:count], originAddr)
				continue
			}
		}

		n.handlePacket(nil, addr, "udp", packetType, payload)
	}
}

// handlePacket 处理通用包
func (n *Node) handlePacket(conn net.Conn, remoteAddr net.Addr, transport string, packetType byte, payload []byte) {
	switch packetType {
	case PacketTypeHandshake:
		n.processHandshake(conn, remoteAddr, transport, payload)
	case PacketTypeData:
		n.processData(remoteAddr, payload)
	case PacketTypeDiscoveryReq:
		n.processDiscoveryRequest(conn, remoteAddr, transport)
	case PacketTypeDiscoveryResp:
		n.processDiscoveryResponse(payload)
	case PacketTypeHeartbeat:
		n.processHeartbeat(conn, remoteAddr, transport)
	case PacketTypeHeartbeatAck:
		n.processHeartbeatAck(remoteAddr)
	}
}

// processData 处理数据消息
func (n *Node) processData(remoteAddr net.Addr, payload []byte) {
	n.mu.RLock()
	peerID, ok := n.addrToPeer[remoteAddr.String()]
	n.mu.RUnlock()

	if !ok {
		return
	}

	p, ok := n.Peers.Get(peerID)
	if !ok {
		return
	}

	if p.Session == nil {
		return
	}

	plaintext, err := p.Session.Decrypt(payload)
	if err != nil {
		n.Config.Logger.Warn("来自 %s 的解密错误: %v", peerID[:8], err)
		if n.metrics != nil {
			n.metrics.IncErrorsTotal()
		}
		return
	}

	// 更新接收流量统计
	p.AddBytesReceived(int64(len(plaintext)))
	if n.metrics != nil {
		n.metrics.AddBytesReceived(int64(len(plaintext)))
	}

	n.mu.RLock()
	onReceive := n.onReceive
	n.mu.RUnlock()
	if onReceive != nil {
		onReceive(peerID, plaintext)
	} else {
		n.Config.Logger.Warn("onReceive 回调为空")
	}
}

// sendRaw 发送原始包（用于握手）
func (n *Node) sendRaw(conn net.Conn, addr net.Addr, transport string, packet []byte) {
	if transport == "tcp" && conn != nil {
		frame := protocol.EncodeTCPFrame(packet)
		if _, err := conn.Write(frame); err != nil {
			n.Config.Logger.Error("TCP write error: %v", err)
		}
	} else if udpAddr, ok := addr.(*net.UDPAddr); ok {
		if _, err := n.conn.WriteToUDP(packet, udpAddr); err != nil {
			n.Config.Logger.Error("UDP write error: %v", err)
		}
	}
}

// GetPeerTransport 返回对端使用的传输协议
func (n *Node) GetPeerTransport(peerID string) string {
	p, ok := n.Peers.Get(peerID)
	if !ok {
		return ""
	}
	if p.Transport == "" {
		return "udp"
	}
	return p.Transport
}

// GetPeerLinkMode 返回与对端的链路模式（p2p/relay）
func (n *Node) GetPeerLinkMode(peerID string) string {
	p, ok := n.Peers.Get(peerID)
	if !ok {
		return ""
	}
	if p.LinkMode == "" {
		return "p2p"
	}
	return p.LinkMode
}

// GetMetrics 获取节点指标快照
func (n *Node) GetMetrics() metrics.Snapshot {
	if n.metrics == nil {
		return metrics.Snapshot{}
	}
	n.metrics.SetConnectionsActive(int64(n.Peers.Count()))
	return n.metrics.GetSnapshot()
}

// GetNATInfo 获取本机 NAT 信息
func (n *Node) GetNATInfo() *nat.NATInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.natInfo
}

// ProbeNAT 通过已连接的节点探测本机 NAT 类型
func (n *Node) ProbeNAT() (*nat.ProbeResult, error) {
	if n.natProber == nil {
		return nil, fmt.Errorf("NAT 探测器未初始化")
	}

	peerIDs := n.Peers.IDs()
	if len(peerIDs) == 0 {
		return nil, fmt.Errorf("没有已连接的节点用于 NAT 探测")
	}

	var helperAddrs []*net.UDPAddr
	for _, peerID := range peerIDs {
		p, ok := n.Peers.Get(peerID)
		if !ok {
			continue
		}
		_, addr, _ := p.GetTransportInfo()
		if udpAddr, ok := addr.(*net.UDPAddr); ok {
			helperAddrs = append(helperAddrs, udpAddr)
		}
	}

	if len(helperAddrs) == 0 {
		return nil, fmt.Errorf("没有用于 NAT 探测的 UDP 对等节点")
	}

	result, err := n.natProber.ProbeViaHelper(helperAddrs)
	if err != nil {
		return nil, err
	}

	n.mu.Lock()
	n.natInfo = &nat.NATInfo{
		Type:            result.NATType,
		PublicAddr:      result.PublicAddr,
		PortPredictable: result.PortPredictable,
		PortDelta:       result.PortDelta,
		LastProbe:       result.ProbeTime,
	}
	n.mu.Unlock()

	return result, nil
}

// GetPeerStats 获取指定节点的统计信息
func (n *Node) GetPeerStats(peerID string) (peer.PeerStats, bool) {
	p, ok := n.Peers.Get(peerID)
	if !ok {
		return peer.PeerStats{}, false
	}
	return p.GetStats(), true
}

// --- 重连相关方法 ---

// OnReconnecting 设置重连中回调
// 当节点开始尝试重连时触发，提供当前尝试次数和下次重试延迟
func (n *Node) OnReconnecting(handler func(peerID string, attempt int, nextRetryIn time.Duration)) {
	n.mu.Lock()
	n.onReconnecting = handler
	if n.reconnectManager != nil {
		n.reconnectManager.SetOnReconnecting(handler)
	}
	n.mu.Unlock()
}

// OnReconnected 设置重连成功回调
// 当节点成功重连时触发，提供总尝试次数
func (n *Node) OnReconnected(handler func(peerID string, attempts int)) {
	n.mu.Lock()
	n.onReconnected = handler
	if n.reconnectManager != nil {
		n.reconnectManager.SetOnReconnected(handler)
	}
	n.mu.Unlock()
}

// OnGaveUp 设置放弃重连回调
// 当达到最大重试次数后触发，提供总尝试次数和最后一次错误
func (n *Node) OnGaveUp(handler func(peerID string, attempts int, lastErr error)) {
	n.mu.Lock()
	n.onGaveUp = handler
	if n.reconnectManager != nil {
		n.reconnectManager.SetOnGaveUp(handler)
	}
	n.mu.Unlock()
}

// GetReconnectInfo 获取指定节点的重连信息
func (n *Node) GetReconnectInfo(peerID string) *ReconnectInfo {
	if n.reconnectManager == nil {
		return nil
	}
	return n.reconnectManager.GetReconnectInfo(peerID)
}

// GetAllReconnectInfo 获取所有正在重连的节点信息
func (n *Node) GetAllReconnectInfo() []*ReconnectInfo {
	if n.reconnectManager == nil {
		return nil
	}
	return n.reconnectManager.GetAllReconnectInfo()
}

// CancelReconnect 取消指定节点的重连
func (n *Node) CancelReconnect(peerID string) {
	if n.reconnectManager != nil {
		n.reconnectManager.CancelReconnect(peerID)
	}
}

// CancelAllReconnects 取消所有重连任务
func (n *Node) CancelAllReconnects() {
	if n.reconnectManager != nil {
		n.reconnectManager.CancelAll()
	}
}
