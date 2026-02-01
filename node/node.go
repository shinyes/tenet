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

	// 中继模式
	RelayModeForward = 0x01 // 请求转发
	RelayModeTarget  = 0x02 // 目标侧接收
)

// Node 表示一个 P2P 节点
type Node struct {
	Config   *Config
	Identity *Identity
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
}

// NewNode 创建一个新的 Node 实例
func NewNode(opts ...Option) (*Node, error) {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	id, err := NewIdentity()
	if err != nil {
		return nil, fmt.Errorf("failed to create identity: %w", err)
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
	}, nil
}

// ID 返回本地节点 ID
func (n *Node) ID() string {
	return n.Identity.ID.String()
}

// Start 启动节点监听
func (n *Node) Start() error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", n.Config.ListenPort))
	if err != nil {
		return fmt.Errorf("failed to resolve addr: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	n.conn = conn
	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		conn.Close()
		return fmt.Errorf("unexpected address type: %T", conn.LocalAddr())
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
			fmt.Printf("中继地址解析失败: %s: %v\n", addrStr, err)
			continue
		}
		n.relayManager.AddRelay(addrStr, relayAddr)
		n.relayAddrSet[relayAddr.String()] = true
	}

	// 启动 TCP 监听（用于接入连接与打洞基础）
	// 使用 transport.ListenConfig（SO_REUSEADDR）以便打洞逻辑也能绑定该端口
	lc := transport.ListenConfig()
	listener, err := lc.Listen(context.Background(), "tcp", fmt.Sprintf(":%d", n.Config.ListenPort))
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to listen tcp: %w", err)
	}
	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		listener.Close()
		conn.Close()
		return fmt.Errorf("unexpected listener type: %T", listener)
	}
	n.tcpListener = tcpListener

	n.wg.Add(2) // 1 个用于 UDP 读，1 个用于 TCP Accept
	go n.handleRead()
	go n.acceptTCP()

	return nil
}

// Stop 停止节点
func (n *Node) Stop() error {
	select {
	case <-n.closing:
		return nil
	default:
		close(n.closing)
	}

	if n.conn != nil {
		n.conn.Close()
	}
	if n.tcpListener != nil {
		n.tcpListener.Close()
	}

	n.wg.Wait()
	return nil
}

// Connect 通过 TCP/UDP 打洞发起连接
func (n *Node) Connect(addrStr string) error {
	if n.conn == nil || n.LocalAddr == nil {
		return fmt.Errorf("node is not started")
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
	packetUDP[4] = 0x01 // Handshake Type
	copy(packetUDP[5:], msgUDP)

	// 构造握手包（TCP）
	packetTCP := make([]byte, 5+len(msgTCP))
	copy(packetTCP[0:4], []byte("TENT"))
	packetTCP[4] = 0x01 // Handshake Type
	copy(packetTCP[5:], msgTCP)

	// 注册待处理握手状态（发起方），按传输前缀区分
	n.mu.Lock()
	n.pendingHandshakes["udp://"+rUDPAddr.String()] = hsUDP
	if rTCPAddr != nil {
		key := "tcp://" + rTCPAddr.String()
		n.pendingHandshakes[key] = hsTCP
	}
	n.mu.Unlock()

	// --- 策略：TCP 与 UDP 同时尝试 ---

	type ConnectResult struct {
		Conn      net.Conn
		Transport string
		Err       error
	}
	resultChan := make(chan ConnectResult, 2)
	// 使用独立的 context 让 TCP 打洞在本函数返回后仍可继续
	// 由于需要“迟到升级”，让 TCP 打洞自行完成并超时退出
	punchCtx := context.Background()

	// 1. TCP 打洞
	go func() {
		// 为打洞创建超时上下文
		ctx, cancel := context.WithTimeout(punchCtx, 10*time.Second)
		defer cancel()

		puncher := nat.NewTCPHolePuncher()
		// 使用与 UDP 相同的本地端口
		conn, err := puncher.Punch(ctx, n.LocalAddr.Port, rTCPAddr)
		if err != nil {
			resultChan <- ConnectResult{Err: err}
			return
		}
		resultChan <- ConnectResult{Conn: conn, Transport: "tcp"}
	}()

	// 2. UDP 打洞（简单发送）
	go func() {
		// 仅发送数据包；收到响应由 handleRead 处理
		// UDP 无连接，发送即视为发起成功，作为 TCP 失败时的兜底
		// 多次发送以打洞
		// 使用本地超时控制发送循环
		timeout := time.After(2 * time.Second)
		for {
			select {
			case <-timeout:
				resultChan <- ConnectResult{Transport: "udp"}
				return
			default:
				n.conn.WriteToUDP(packetUDP, rUDPAddr)
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()

	// 等待第一个可用结果，理想情况下 TCP 优先
	// 等待第一个结果或超时
	select {
	case res := <-resultChan:
		if res.Transport == "tcp" && res.Conn != nil {
			// TCP 成功！
			// TCP 帧格式: [Len(2)] [Packet]
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
			// UDP 已发送，作为有效兜底
			// 但需要处理可能的 TCP 迟到成功
			if n.Config.EnableRelay {
				addrKey := rUDPAddr.String()
				go func() {
					time.Sleep(n.Config.DialTimeout)
					n.mu.RLock()
					_, ok := n.addrToPeer[addrKey]
					n.mu.RUnlock()
					if !ok {
						n.connectViaRelay(addrStr)
					}
				}()
			}
			go func() {
				// 等待 TCP 结果（可能已在通道中或即将到达）
				// 循环直到 TCP 成功或超时
				timeout := time.After(10 * time.Second)
				for {
					select {
					case res2 := <-resultChan:
						if res2.Transport == "tcp" && res2.Conn != nil {
							// TCP 迟到成功，执行升级逻辑
							// 通过 TCP 发送握手
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

							// 开始处理 TCP，握手成功后由 processHandshake 触发升级
							go n.handleTCP(res2.Conn)
							return // 完成
						} else if res2.Conn != nil {
							// 非 TCP 但连接不为空，关闭以避免泄漏
							res2.Conn.Close()
						}
						// 忽略其他结果（例如 UDP 迟到失败）
					case <-timeout:
						// TCP 最终超时，排空通道中可能残留的连接
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
		return fmt.Errorf("connection failed")
	case <-time.After(5 * time.Second):
		if n.relayManager != nil {
			if err := n.connectViaRelay(addrStr); err == nil {
				return nil
			}
		}
		return fmt.Errorf("connection timeout")
	}
}

// Send 向对等节点发送数据
func (n *Node) Send(peerID string, data []byte) error {
	p, ok := n.Peers.Get(peerID)
	if !ok {
		return fmt.Errorf("peer not found: %s", peerID)
	}
	if p.Session == nil {
		return fmt.Errorf("peer session not established")
	}

	encrypted, err := p.Session.Encrypt(data)
	if err != nil {
		return err
	}

	// 包格式: [Magic(4)] [Type(1)] [Verified(1)?] [Data]
	packet := make([]byte, 5+len(encrypted))
	copy(packet[0:4], []byte("TENT"))
	packet[4] = 0x02 // Data Type
	copy(packet[5:], encrypted)

	transport, addr, conn := p.GetTransportInfo()

	if transport == "tcp" && conn != nil {
		// TCP 帧格式: [Len(2)] [Packet]
		length := uint16(len(packet))
		frame := make([]byte, 2+len(packet))
		frame[0] = byte(length >> 8)
		frame[1] = byte(length)
		copy(frame[2:], packet)
		_, err := conn.Write(frame)
		return err
	}

	if p.LinkMode == "relay" {
		if relayAddr, ok := addr.(*net.UDPAddr); ok {
			if p.RelayTarget != nil {
				relayPacket, err := n.buildRelayPacket(0x01, p.RelayTarget, packet)
				if err != nil {
					return err
				}
				_, err = n.conn.WriteToUDP(relayPacket, relayAddr)
				return err
			}
			// 没有目标地址时，直接发送给中继，由中继根据映射转发
			_, err = n.conn.WriteToUDP(packet, relayAddr)
			return err
		}
	}

	// 回退到 UDP
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return fmt.Errorf("invalid udp address for peer")
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
				// Log error?
				continue
			}
		}

		// 处理新连接
		go n.handleTCP(conn)
	}
}

// handleTCP 处理 TCP 入站连接
func (n *Node) handleTCP(conn net.Conn) {
	// fmt.Printf("DEBUG: handleTCP start for %s\n", conn.RemoteAddr())
	defer func() {
		// fmt.Printf("DEBUG: handleTCP exit for %s\n", conn.RemoteAddr())
		conn.Close()
	}()

	// 按帧读取
	// [Len(2)] [Packet...]
	header := make([]byte, 2)
	const maxTCPFrameSize = 32 * 1024
	for {
		// 读取长度
		_, err := io.ReadFull(conn, header)
		if err != nil {
			if err != io.EOF {
				fmt.Printf("TCP read error from %s: %v\n", conn.RemoteAddr(), err)
			}
			return
		}
		length := uint16(header[0])<<8 | uint16(header[1])
		if length == 0 || length > maxTCPFrameSize {
			fmt.Printf("DEBUG: Invalid TCP Frame Len=%d from %s\n", length, conn.RemoteAddr())
			return
		}

		// 读取包体
		buf := make([]byte, length)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return
		}

		// 处理包
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

	buf := make([]byte, 65535)
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
			// If closed, return
			select {
			case <-n.closing:
				return
			default:
				// Log error?
			}
			continue
		}

		// 基本包校验
		if count < 5 {
			continue
		}
		if string(buf[0:4]) != "TENT" {
			continue
		}

		packetType := buf[4]
		payload := make([]byte, count-5)
		copy(payload, buf[5:count])

		if packetType == 0x03 {
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
	case 0x01: // 握手
		n.processHandshake(conn, remoteAddr, transport, payload)
	case 0x02: // 数据
		n.processData(remoteAddr, payload)
	case 0x04: // 节点发现请求
		n.processDiscoveryRequest(conn, remoteAddr, transport)
	case 0x05: // 节点发现响应
		n.processDiscoveryResponse(payload)
	}
}

// processHandshake 处理握手消息
func (n *Node) processHandshake(conn net.Conn, remoteAddr net.Addr, transport string, payload []byte) {
	n.mu.Lock()
	defer func() {
		n.mu.Unlock()
	}()

	addrStr := remoteAddr.String()
	// 使用传输相关的 key
	stateKey := fmt.Sprintf("%s://%s", transport, addrStr)
	hs, exists := n.pendingHandshakes[stateKey]

	// 若不存在则创建响应方握手
	if !exists {
		var err error
		hs, err = crypto.NewResponderHandshake(
			n.Identity.NoisePrivateKey[:],
			n.Identity.NoisePublicKey[:],
			[]byte(n.Config.NetworkPassword),
		)
		if err != nil {
			fmt.Printf("Error creating handshake: %v\n", err)
			return
		}
		n.pendingHandshakes[stateKey] = hs
	}

	// 处理消息
	response, session, err := hs.ProcessMessage(payload)
	if err != nil {
		// 智能错误处理：
		// 若握手失败（如“消息过短”或“鉴权失败”），
		// 先检查是否已连接该对端（或地址）。
		// 这可处理并发打洞时的 UDP 重传或迟到包。
		// n.mu.Lock() // 已持有锁
		_, isConnected := n.addrToPeer[addrStr]
		// n.mu.Unlock()

		if isConnected {
			// 这可能是已建立连接的冗余包。
			// 结构正确但状态无效（重放/迟到）。
			// 可安全忽略以保持输出简洁。
			return
		}

		// 真实的握手失败
		fmt.Printf("Handshake error from %s: %v\n", addrStr, err)
		delete(n.pendingHandshakes, stateKey)
		return
	}

	// 如需响应则发送
	if response != nil {
		packet := make([]byte, 5+len(response))
		copy(packet[0:4], []byte("TENT"))
		packet[4] = 0x01 // Handshake Type
		copy(packet[5:], response)

		n.sendRaw(conn, remoteAddr, transport, packet)
	}

	// 如果会话建立
	if session != nil {
		// 握手完成
		delete(n.pendingHandshakes, stateKey)

		remotePub := session.RemotePublicKey()
		// 使用一致的 ID 推导（同 identity.go）
		// ID = SHA256(PublicKey)[:16]
		idHash := sha256.Sum256(remotePub)
		peerID := fmt.Sprintf("%x", idHash[:16])

		// 传输升级逻辑：
		existingPeer, ok := n.Peers.Get(peerID)
		if ok {
			// 对端已存在，检查是否需要升级到 TCP
			if transport == "tcp" && existingPeer.Transport != "tcp" {
				fmt.Printf(">>> 升级成功: 节点 %s 已切换至 TCP 链路 <<<\n", peerID[:8])
				existingPeer.UpgradeTransport(remoteAddr, conn, transport, session)

				// n.mu is already held
				n.addrToPeer[addrStr] = peerID
				n.registerRelayCandidate(peerID, remoteAddr)
				return
			}
			if target, ok := n.relayPendingTarget[addrStr]; ok {
				existingPeer.SetLinkMode("relay", target)
				delete(n.relayPendingTarget, addrStr)
			} else if inbound := n.relayInbound[addrStr]; inbound {
				existingPeer.SetLinkMode("relay", nil)
				delete(n.relayInbound, addrStr)
			}
			// 已是 TCP 或为冗余 UDP，仅更新 LastSeen
			existingPeer.UpdateLastSeen()
			n.registerRelayCandidate(peerID, remoteAddr)
			return
		}

		linkMode := "p2p"
		var relayTarget *net.UDPAddr
		if target, ok := n.relayPendingTarget[addrStr]; ok {
			linkMode = "relay"
			relayTarget = target
			delete(n.relayPendingTarget, addrStr)
		}
		if inbound := n.relayInbound[addrStr]; inbound {
			linkMode = "relay"
			delete(n.relayInbound, addrStr)
		}

		// 检查是否超过最大连接数
		if n.Config.MaxPeers > 0 && n.Peers.Count() >= n.Config.MaxPeers {
			fmt.Printf("已达到最大连接数 %d，拒绝新连接 %s\n", n.Config.MaxPeers, peerID[:8])
			return
		}

		// 新对端
		p := &peer.Peer{
			ID:          peerID,
			Addr:        remoteAddr,
			Conn:        conn,
			Transport:   transport,
			LinkMode:    linkMode,
			RelayTarget: relayTarget,
			Session:     session,
			LastSeen:    time.Now(),
		}
		n.Peers.Add(p)
		// n.mu 已由 processHandshake 持有
		n.addrToPeer[addrStr] = peerID
		n.registerRelayCandidate(peerID, remoteAddr)

		// 向新连接的节点请求其已知的节点列表（节点发现）
		go n.sendDiscoveryRequest(p)

		if n.onPeerConnected != nil {
			go n.onPeerConnected(peerID)
		}
	}
}

// sendDiscoveryRequest 向指定节点发送节点发现请求
func (n *Node) sendDiscoveryRequest(p *peer.Peer) {
	// 构建发现请求包：TENT + 0x04 + 空payload
	packet := make([]byte, 5)
	copy(packet[0:4], []byte("TENT"))
	packet[4] = 0x04

	transport, addr, conn := p.GetTransportInfo()
	n.sendRaw(conn, addr, transport, packet)
}

// processDiscoveryRequest 处理节点发现请求，返回已知节点列表
func (n *Node) processDiscoveryRequest(conn net.Conn, remoteAddr net.Addr, transport string) {
	n.mu.RLock()
	peerIDs := n.Peers.IDs()
	n.mu.RUnlock()

	// 构建响应：收集所有已知节点的 (PeerID, Addr) 对
	// 格式: [Count(2 bytes)] [Entry...]，每个 Entry: [PeerIDLen(1)] [PeerID] [AddrLen(1)] [Addr]
	var entries []byte
	count := 0

	for _, pid := range peerIDs {
		p, ok := n.Peers.Get(pid)
		if !ok {
			continue
		}
		// 获取节点地址
		_, addr, _ := p.GetTransportInfo()
		if addr == nil {
			continue
		}
		addrStr := addr.String()

		// 编码 entry
		pidBytes := []byte(pid)
		addrBytes := []byte(addrStr)
		if len(pidBytes) > 255 || len(addrBytes) > 255 {
			continue
		}

		entry := make([]byte, 1+len(pidBytes)+1+len(addrBytes))
		entry[0] = byte(len(pidBytes))
		copy(entry[1:1+len(pidBytes)], pidBytes)
		entry[1+len(pidBytes)] = byte(len(addrBytes))
		copy(entry[2+len(pidBytes):], addrBytes)

		entries = append(entries, entry...)
		count++
	}

	// 构建完整响应包
	payload := make([]byte, 2+len(entries))
	payload[0] = byte(count >> 8)
	payload[1] = byte(count & 0xFF)
	copy(payload[2:], entries)

	packet := make([]byte, 5+len(payload))
	copy(packet[0:4], []byte("TENT"))
	packet[4] = 0x05
	copy(packet[5:], payload)

	n.sendRaw(conn, remoteAddr, transport, packet)
}

// processDiscoveryResponse 处理节点发现响应，尝试连接未知节点
func (n *Node) processDiscoveryResponse(payload []byte) {
	if len(payload) < 2 {
		return
	}

	count := int(payload[0])<<8 | int(payload[1])
	offset := 2

	for i := 0; i < count && offset < len(payload); i++ {
		// 解析 PeerID
		if offset >= len(payload) {
			break
		}
		pidLen := int(payload[offset])
		offset++
		if offset+pidLen > len(payload) {
			break
		}
		peerID := string(payload[offset : offset+pidLen])
		offset += pidLen

		// 解析 Addr
		if offset >= len(payload) {
			break
		}
		addrLen := int(payload[offset])
		offset++
		if offset+addrLen > len(payload) {
			break
		}
		addrStr := string(payload[offset : offset+addrLen])
		offset += addrLen

		// 跳过自己
		if peerID == n.localPeerID {
			continue
		}

		// 跳过已连接的节点
		if _, exists := n.Peers.Get(peerID); exists {
			continue
		}

		// 尝试连接新发现的节点
		fmt.Printf("[发现] 通过节点发现发现新节点 %s (%s)，尝试连接...\n", peerID[:8], addrStr)
		go func(addr string) {
			if err := n.Connect(addr); err != nil {
				fmt.Printf("[发现] 连接 %s 失败: %v\n", addr, err)
			}
		}(addrStr)
	}
}

// connectViaRelay 使用中继发送握手包
func (n *Node) connectViaRelay(targetAddrStr string) error {
	if n.relayManager == nil {
		return fmt.Errorf("relay manager not initialized")
	}

	targetAddr, err := net.ResolveUDPAddr("udp", targetAddrStr)
	if err != nil {
		return err
	}

	relay, err := n.relayManager.SelectBestRelay()
	if err != nil {
		return err
	}

	hs, msg, err := crypto.NewInitiatorHandshake(
		n.Identity.NoisePrivateKey[:],
		n.Identity.NoisePublicKey[:],
		[]byte(n.Config.NetworkPassword),
	)
	if err != nil {
		return err
	}

	packet := make([]byte, 5+len(msg))
	copy(packet[0:4], []byte("TENT"))
	packet[4] = 0x01
	copy(packet[5:], msg)

	relayPacket, err := n.buildRelayPacket(0x01, targetAddr, packet)
	if err != nil {
		return err
	}

	n.mu.Lock()
	n.pendingHandshakes["udp://"+relay.Addr.String()] = hs
	n.relayPendingTarget[relay.Addr.String()] = targetAddr
	n.mu.Unlock()

	_, err = n.conn.WriteToUDP(relayPacket, relay.Addr)
	return err
}

// registerRelayCandidate 将已连接节点作为潜在中继候选
// 所有节点都会注册候选，但只有开启 EnableRelay 的节点才会响应转发请求
func (n *Node) registerRelayCandidate(peerID string, remoteAddr net.Addr) {
	if n.relayManager == nil {
		return
	}
	udpAddr, ok := remoteAddr.(*net.UDPAddr)
	if !ok || udpAddr == nil {
		return
	}
	addrStr := udpAddr.String()
	if n.relayAddrSet[addrStr] {
		return
	}
	n.relayManager.AddRelay(peerID, udpAddr)
	n.relayAddrSet[addrStr] = true
}

// buildRelayPacket 构造中继封装包
func (n *Node) buildRelayPacket(mode byte, targetAddr *net.UDPAddr, inner []byte) ([]byte, error) {
	if targetAddr == nil {
		return nil, fmt.Errorf("targetAddr is nil")
	}
	addrStr := targetAddr.String()
	if len(addrStr) > 255 {
		return nil, fmt.Errorf("targetAddr too long")
	}
	payload := make([]byte, 2+len(addrStr)+len(inner))
	payload[0] = mode
	payload[1] = byte(len(addrStr))
	copy(payload[2:], []byte(addrStr))
	copy(payload[2+len(addrStr):], inner)

	packet := make([]byte, 5+len(payload))
	copy(packet[0:4], []byte("TENT"))
	packet[4] = 0x03
	copy(packet[5:], payload)
	return packet, nil
}

// handleRelayPacket 处理中继封装包
func (n *Node) handleRelayPacket(origin *net.UDPAddr, payload []byte) {
	if !n.Config.EnableRelay {
		return
	}
	if origin == nil || len(payload) < 2 {
		return
	}
	mode := payload[0]
	addrLen := int(payload[1])
	if len(payload) < 2+addrLen {
		return
	}
	addrStr := string(payload[2 : 2+addrLen])
	inner := payload[2+addrLen:]
	if len(inner) == 0 {
		return
	}
	targetAddr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		return
	}

	if mode == 0x01 {
		// 作为中继转发给目标
		n.mu.Lock()
		n.relayForward[targetAddr.String()] = origin
		n.mu.Unlock()

		forwardPacket, err := n.buildRelayPacket(0x02, targetAddr, inner)
		if err != nil {
			return
		}
		n.conn.WriteToUDP(forwardPacket, targetAddr)
		return
	}

	if mode == 0x02 {
		// 目标侧解封装并本地处理
		if len(inner) < 5 || string(inner[0:4]) != "TENT" {
			return
		}
		packetType := inner[4]
		payload := make([]byte, len(inner)-5)
		copy(payload, inner[5:])

		n.mu.Lock()
		n.relayInbound[origin.String()] = true
		n.mu.Unlock()

		n.handlePacket(nil, origin, "udp", packetType, payload)
	}
}

// processData 处理数据消息
func (n *Node) processData(remoteAddr net.Addr, payload []byte) {
	n.mu.RLock()

	peerID, ok := n.addrToPeer[remoteAddr.String()]
	n.mu.RUnlock()

	// fmt.Printf("DEBUG: processData from %s -> PeerID=%s Found=%v\n", remoteAddr, peerID, ok)
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
		return
	}

	n.mu.RLock()
	onReceive := n.onReceive
	n.mu.RUnlock()
	if onReceive != nil {
		onReceive(peerID, plaintext)
	} else {
		fmt.Println("onReceive callback is nil")
	}
}

// sendRaw 发送原始包（用于握手）
func (n *Node) sendRaw(conn net.Conn, addr net.Addr, transport string, packet []byte) {
	if transport == "tcp" && conn != nil {
		length := uint16(len(packet))
		frame := make([]byte, 2+len(packet))
		frame[0] = byte(length >> 8)
		frame[1] = byte(length)
		copy(frame[2:], packet)
		if _, err := conn.Write(frame); err != nil {
			fmt.Printf("DEBUG: sendRaw tcp write error to %s: %v\n", conn.RemoteAddr(), err)
		}
	} else if udpAddr, ok := addr.(*net.UDPAddr); ok {
		if _, err := n.conn.WriteToUDP(packet, udpAddr); err != nil {
			fmt.Printf("DEBUG: sendRaw udp write error to %s: %v\n", udpAddr.String(), err)
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
		return "udp" // 默认
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
