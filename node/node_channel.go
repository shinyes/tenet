package node

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net"

	"github.com/shinyes/tenet/peer"
)

// Channel Update OpCodes
const (
	OpCodeJoin  = 0x01
	OpCodeLeave = 0x02
)

// 应用层帧类型 (Encrypted Payload 内)
const (
	AppFrameTypeUserWithChannel = 0x02 // 用户数据（携带频道标志）
	AppFrameTypeChannelUpdate   = 0x01 // 频道更新（内部使用）
)

// hashChannelName 计算频道名称的 SHA-256 哈希
func hashChannelName(name string) []byte {
	h := sha256.Sum256([]byte(name))
	return h[:]
}

// JoinChannel 加入频道并通知所有 Peer
func (n *Node) JoinChannel(name string) {
	n.mu.Lock()
	// 检查是否已存在
	for _, ch := range n.Config.Channels {
		if ch == name {
			n.mu.Unlock()
			return
		}
	}
	n.Config.Channels = append(n.Config.Channels, name)
	n.mu.Unlock()

	n.Config.Logger.Info("加入频道: %s (广播通知)", name)
	n.broadcastChannelUpdate(OpCodeJoin, hashChannelName(name))
}

// LeaveChannel 离开频道并通知所有 Peer
func (n *Node) LeaveChannel(name string) {
	n.mu.Lock()
	found := false
	for i, ch := range n.Config.Channels {
		if ch == name {
			// Remove from slice
			n.Config.Channels = append(n.Config.Channels[:i], n.Config.Channels[i+1:]...)
			found = true
			break
		}
	}
	n.mu.Unlock()

	if found {
		n.Config.Logger.Info("离开频道: %s (广播通知)", name)
		n.broadcastChannelUpdate(OpCodeLeave, hashChannelName(name))
	}
}

// broadcastChannelUpdate 广播频道更新包
func (n *Node) broadcastChannelUpdate(opcode byte, channelHash []byte) {
	n.mu.RLock()
	peerIDs := n.Peers.IDs()
	n.mu.RUnlock()

	// Payload: [OpCode] + [Hash]
	payload := make([]byte, 1+32)
	payload[0] = opcode
	copy(payload[1:], channelHash)

	for _, pid := range peerIDs {
		// 并发发送
		go func(peerID string) {
			if err := n.sendAppFrame(peerID, AppFrameTypeChannelUpdate, payload); err != nil {
				// 忽略发送错误，可能是对方已断开
			}
		}(pid)
	}
}

// processChannelUpdate 处理频道更新包
func (n *Node) processChannelUpdate(p *peer.Peer, payload []byte) {
	if len(payload) < 33 {
		return
	}
	opcode := payload[0]
	hash := payload[1:33]

	matchesLocal := false
	n.mu.RLock()
	for _, ch := range n.Config.Channels {
		localHash := hashChannelName(ch)
		if bytes.Equal(localHash, hash) {
			matchesLocal = true
			break
		}
	}
	n.mu.RUnlock()

	if matchesLocal {
		if opcode == OpCodeJoin {
			p.AddRemoteChannel(hash)
			n.Config.Logger.Debug("Peer %s 加入了共同频道 (Hash: %x)", p.ID[:8], hash[:4])
		} else if opcode == OpCodeLeave {
			p.RemoveRemoteChannel(hash)
			n.Config.Logger.Debug("Peer %s 离开了共同频道 (Hash: %x)", p.ID[:8], hash[:4])
		}
	}
}

// Broadcast 向指定频道广播数据
func (n *Node) Broadcast(channelName string, data []byte) (int, error) {
	peers := n.GetPeersInChannel(channelName)

	successCount := 0
	for _, peerID := range peers {
		if err := n.Send(channelName, peerID, data); err == nil {
			successCount++
		}
	}
	return successCount, nil
}

// GetPeersInChannel 返回指定频道的节点ID列表
func (n *Node) GetPeersInChannel(channelName string) []string {
	hash := hashChannelName(channelName)
	allIDs := n.Peers.IDs()
	var targetPeers []string
	for _, id := range allIDs {
		if p, ok := n.Peers.Get(id); ok {
			if p.HasRemoteChannel(hash) {
				targetPeers = append(targetPeers, id)
			}
		}
	}
	return targetPeers
}

// isChannelSubscribed 检查本地是否订阅了指定频道
func (n *Node) isChannelSubscribed(channelHash []byte) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, ch := range n.Config.Channels {
		if bytes.Equal(hashChannelName(ch), channelHash) {
			return true
		}
	}
	return false
}

// syncChannelsWithPeer 将本地加入的所有频道同步给指定 Peer
func (n *Node) syncChannelsWithPeer(peerID string) {
	n.mu.RLock()
	channels := make([]string, len(n.Config.Channels))
	copy(channels, n.Config.Channels)
	n.mu.RUnlock()

	for _, ch := range channels {
		hash := hashChannelName(ch)
		// Payload: [OpCodeJoin] + [Hash]
		payload := make([]byte, 1+32)
		payload[0] = OpCodeJoin
		copy(payload[1:], hash)

		if err := n.sendAppFrame(peerID, AppFrameTypeChannelUpdate, payload); err != nil {
			n.Config.Logger.Debug("同步频道 %s 给 Peer %s 失败: %v", ch, peerID[:8], err)
		}
	}
}

// handleAppFrame 处理应用层分帧
func (n *Node) handleAppFrame(peerID string, p *peer.Peer, data []byte) {
	if len(data) < 1 {
		return
	}
	appFrameType := data[0]
	payload := data[1:]

	switch appFrameType {
	case AppFrameTypeUserWithChannel:
		// 频道用户数据: [ChannelHash(32)] [UserData]
		if len(payload) < 32 {
			n.Config.Logger.Warn("收到无效的频道数据帧 (来自 %s)", peerID[:8])
			return
		}
		channelHash := payload[:32]
		userData := payload[32:]

		// 严格验证频道: 检查本地是否订阅了该频道
		if !n.isChannelSubscribed(channelHash) {
			n.Config.Logger.Warn("拒绝接收来自非订阅频道的消息 (Peer: %s, Hash: %x)", peerID[:8], channelHash[:4])
			return
		}

		// 回调上层
		n.mu.RLock()
		onReceive := n.onReceive
		n.mu.RUnlock()
		if onReceive != nil {
			onReceive(peerID, userData)
		}

	case AppFrameTypeChannelUpdate:
		// 频道更新 -> 内部处理
		n.processChannelUpdate(p, payload)
	}
}

// Send 向对等节点发送数据 (指定频道)
func (n *Node) Send(channelName string, peerID string, data []byte) error {
	if !n.isChannelSubscribed(hashChannelName(channelName)) {
		return fmt.Errorf("节点未订阅频道: %s", channelName)
	}

	// 组合帧: [ChannelHash(32)] [UserData]
	channelHash := hashChannelName(channelName)
	frame := make([]byte, 32+len(data))
	copy(frame[:32], channelHash)
	copy(frame[32:], data)
	return n.sendAppFrame(peerID, AppFrameTypeUserWithChannel, frame)
}

// sendAppFrame 发送应用层帧 (封装了 AppFrameType)
func (n *Node) sendAppFrame(peerID string, appFrameType byte, data []byte) error {
	// 检查是否尝试向自己发送
	if peerID == n.ID() {
		return fmt.Errorf("不能向本节点发送数据")
	}

	p, ok := n.Peers.Get(peerID)
	if !ok {
		return fmt.Errorf("未找到对等节点: %s", peerID)
	}
	// Serialize data sending with transport/session switch.
	p.LockDataSend()
	defer p.UnlockDataSend()

	session := p.GetSession()
	if session == nil {
		return fmt.Errorf("对等节点会话未建立")
	}

	// === 1. 确定传输通道 (Writer) ===
	var sendFunc func(payload []byte) error
	transport, addr, conn := p.GetTransportInfo()

	if transport == "tcp" && conn != nil {
		sendFunc = func(payload []byte) error {
			return n.writeTCPPacket(conn, payload)
		}
	} else if n.kcpTransport != nil && n.kcpTransport.HasSession(p.ID) {
		sendFunc = func(payload []byte) error {
			return n.kcpTransport.Send(p.ID, payload)
		}
	} else if transport == "udp" {
		udpAddr, ok := addr.(*net.UDPAddr)
		if !ok {
			return fmt.Errorf("udp transport has invalid address type")
		}
		if n.kcpTransport == nil {
			return fmt.Errorf("reliable data transport unavailable: udp peer without kcp")
		}
		if err := n.kcpTransport.UpgradePeer(p.ID, udpAddr); err != nil {
			return fmt.Errorf("failed to upgrade to kcp for data transport: %w", err)
		}
		sendFunc = func(payload []byte) error {
			return n.kcpTransport.Send(p.ID, payload)
		}
	}
	if sendFunc == nil {
		return fmt.Errorf("无法建立发送通道")
	}

	// === 2. 定义 sendFrame 辅助函数 ===
	sendFrame := func(frameType byte, fragmentData []byte) error {
		// 应用层封包: [FrameType(1)] [Payload]
		plainFrame := make([]byte, 1+len(fragmentData))
		plainFrame[0] = frameType
		copy(plainFrame[1:], fragmentData)

		encrypted, err := session.Encrypt(plainFrame)
		if err != nil {
			return err
		}

		// 包格式: [Magic(4)] [Type(1)] [Data(Encrypted)]
		packet := make([]byte, 5+len(encrypted))
		copy(packet[0:4], []byte("TENT"))
		packet[4] = PacketTypeData
		copy(packet[5:], encrypted)

		// 更新流量统计
		p.AddBytesSent(int64(len(fragmentData)))
		if n.metrics != nil {
			n.metrics.AddBytesSent(int64(len(fragmentData)))
		}

		return sendFunc(packet)
	}

	// === 3. 分包发送 ===
	// Payload: [AppFrameType(1)] + [Data]
	payload := make([]byte, 1+len(data))
	payload[0] = appFrameType
	copy(payload[1:], data)

	offset := 0
	totalLen := len(payload)

	// 1. 发送 First 帧
	if totalLen > MaxPayloadSize {
		end := MaxPayloadSize
		if err := sendFrame(FrameTypeFirst, payload[:end]); err != nil {
			return err
		}
		offset = end
	} else {
		return sendFrame(FrameTypeSingle, payload)
	}

	// 2. 循环发送 Middle 帧
	for (totalLen - offset) > MaxPayloadSize {
		end := offset + MaxPayloadSize
		if err := sendFrame(FrameTypeMiddle, payload[offset:end]); err != nil {
			return err
		}
		offset = end
	}

	// 3. 发送 Last 帧
	return sendFrame(FrameTypeLast, payload[offset:])
}
