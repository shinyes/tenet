package node

import (
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
func hashChannelName(name string) [32]byte {
	return sha256.Sum256([]byte(name))
}

// JoinChannel 加入频道并通知所有 Peer
func (n *Node) JoinChannel(name string) {
	n.mu.Lock()
	for _, ch := range n.Config.Channels {
		if ch == name {
			n.mu.Unlock()
			return
		}
	}
	n.Config.Channels = append(n.Config.Channels, name)
	n.localChannelSet[hashChannelName(name)] = struct{}{}
	n.mu.Unlock()

	n.Config.Logger.Info("加入频道: %s (广播通知)", name)
	hash := hashChannelName(name)
	n.broadcastChannelUpdate(OpCodeJoin, hash[:])
}

// LeaveChannel 离开频道并通知所有 Peer
func (n *Node) LeaveChannel(name string) {
	n.mu.Lock()
	found := false
	for i, ch := range n.Config.Channels {
		if ch == name {
			n.Config.Channels = append(n.Config.Channels[:i], n.Config.Channels[i+1:]...)
			delete(n.localChannelSet, hashChannelName(name))
			found = true
			break
		}
	}
	n.mu.Unlock()

	if found {
		n.Config.Logger.Info("离开频道: %s (广播通知)", name)
		hash := hashChannelName(name)
		n.broadcastChannelUpdate(OpCodeLeave, hash[:])
	}
}

// broadcastChannelUpdate 广播频道更新包
func (n *Node) broadcastChannelUpdate(opcode byte, channelHash []byte) {
	n.mu.RLock()
	peerIDs := n.Peers.IDs()
	n.mu.RUnlock()

	payload := make([]byte, 1+32)
	payload[0] = opcode
	copy(payload[1:], channelHash)

	for _, pid := range peerIDs {
		go func(peerID string) {
			if err := n.sendAppFrame(peerID, AppFrameTypeChannelUpdate, payload); err != nil {
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
	if len(hash) == 32 {
		var fixedHash [32]byte
		copy(fixedHash[:], hash)
		n.mu.RLock()
		_, matchesLocal = n.localChannelSet[fixedHash]
		n.mu.RUnlock()
	}

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
			if p.HasRemoteChannel(hash[:]) {
				targetPeers = append(targetPeers, id)
			}
		}
	}
	return targetPeers
}

// isChannelSubscribed 检查本地是否订阅了指定频道
func (n *Node) isChannelSubscribed(channelHash []byte) bool {
	if len(channelHash) != 32 {
		return false
	}
	var fixedHash [32]byte
	copy(fixedHash[:], channelHash)

	n.mu.RLock()
	defer n.mu.RUnlock()
	_, ok := n.localChannelSet[fixedHash]
	return ok
}

// syncChannelsWithPeer 将本地加入的所有频道同步给指定 Peer
func (n *Node) syncChannelsWithPeer(peerID string) {
	n.mu.RLock()
	channels := make([]string, len(n.Config.Channels))
	copy(channels, n.Config.Channels)
	n.mu.RUnlock()

	for _, ch := range channels {
		hash := hashChannelName(ch)
		payload := make([]byte, 1+32)
		payload[0] = OpCodeJoin
		copy(payload[1:], hash[:])

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
		if len(payload) < 32 {
			n.Config.Logger.Warn("收到无效的频道数据帧 (来自 %s)", peerID[:8])
			return
		}
		channelHash := payload[:32]
		userData := payload[32:]

		if !n.isChannelSubscribed(channelHash) {
			n.Config.Logger.Warn("拒绝接收来自非订阅频道的消息 (Peer: %s, Hash: %x)", peerID[:8], channelHash[:4])
			return
		}

		n.mu.RLock()
		onReceive := n.onReceive
		n.mu.RUnlock()
		if onReceive != nil {
			onReceive(peerID, userData)
		}

	case AppFrameTypeChannelUpdate:
		n.processChannelUpdate(p, payload)
	}
}

// Send 向对等节点发送数据 (指定频道)
func (n *Node) Send(channelName string, peerID string, data []byte) error {
	channelHash := hashChannelName(channelName)
	if !n.isChannelSubscribed(channelHash[:]) {
		return fmt.Errorf("节点未订阅频道: %s", channelName)
	}

	frame := make([]byte, 32+len(data))
	copy(frame[:32], channelHash[:])
	copy(frame[32:], data)
	return n.sendAppFrame(peerID, AppFrameTypeUserWithChannel, frame)
}

// sendAppFrame 发送应用层帧 (封装了 AppFrameType)
func (n *Node) sendAppFrame(peerID string, appFrameType byte, data []byte) error {
	if peerID == n.ID() {
		return fmt.Errorf("不能向本节点发送数据")
	}

	p, ok := n.Peers.Get(peerID)
	if !ok {
		return fmt.Errorf("未找到对等节点: %s", peerID)
	}
	p.LockDataSend()
	defer p.UnlockDataSend()

	session := p.GetSession()
	if session == nil {
		return fmt.Errorf("对等节点会话未建立")
	}

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

	sendFrame := func(frameType byte, fragmentData []byte) error {
		plainFrame := make([]byte, 1+len(fragmentData))
		plainFrame[0] = frameType
		copy(plainFrame[1:], fragmentData)

		encrypted, err := session.Encrypt(plainFrame)
		if err != nil {
			return err
		}

		packet := make([]byte, 5+len(encrypted))
		copy(packet[0:4], []byte("TENT"))
		packet[4] = PacketTypeData
		copy(packet[5:], encrypted)

		p.AddBytesSent(int64(len(fragmentData)))
		if n.metrics != nil {
			n.metrics.AddBytesSent(int64(len(fragmentData)))
		}

		return sendFunc(packet)
	}

	payload := make([]byte, 1+len(data))
	payload[0] = appFrameType
	copy(payload[1:], data)

	offset := 0
	totalLen := len(payload)

	if totalLen > MaxPayloadSize {
		end := MaxPayloadSize
		if err := sendFrame(FrameTypeFirst, payload[:end]); err != nil {
			return err
		}
		offset = end
	} else {
		return sendFrame(FrameTypeSingle, payload)
	}

	for (totalLen - offset) > MaxPayloadSize {
		end := offset + MaxPayloadSize
		if err := sendFrame(FrameTypeMiddle, payload[offset:end]); err != nil {
			return err
		}
		offset = end
	}

	return sendFrame(FrameTypeLast, payload[offset:])
}
