package node

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net"

	"github.com/cykyes/tenet/peer"
)

// Channel Update OpCodes
const (
	OpCodeJoin  = 0x01
	OpCodeLeave = 0x02
)

// 应用层帧类型 (Encrypted Payload 内)
const (
	AppFrameTypeUser          = 0x00
	AppFrameTypeChannelUpdate = 0x01
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
	case AppFrameTypeUser:
		// 用户数据 -> 回调上层
		if n.onReceive != nil {
			n.onReceive(peerID, payload)
		}
	case AppFrameTypeChannelUpdate:
		// 频道更新 -> 内部处理
		n.processChannelUpdate(p, payload)
	}
}

// Send 向对等节点发送数据
func (n *Node) Send(peerID string, data []byte) error {
	return n.sendAppFrame(peerID, AppFrameTypeUser, data)
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
	if p.Session == nil {
		return fmt.Errorf("对等节点会话未建立")
	}

	// === 1. 确定传输通道 (Writer) ===
	var sendFunc func(payload []byte) error
	transport, _, conn := p.GetTransportInfo()

	if transport == "tcp" && conn != nil {
		sendFunc = func(payload []byte) error {
			length := uint16(len(payload))
			frame := make([]byte, 2+len(payload))
			frame[0] = byte(length >> 8)
			frame[1] = byte(length)
			copy(frame[2:], payload)
			_, err := conn.Write(frame)
			return err
		}
	} else if n.kcpTransport != nil && n.kcpTransport.HasSession(p.ID) {
		sendFunc = func(payload []byte) error {
			return n.kcpTransport.Send(p.ID, payload)
		}
	} else if transport == "udp" {
		addr := p.Addr
		if udpAddr, ok := addr.(*net.UDPAddr); ok && n.kcpTransport != nil {
			if err := n.kcpTransport.UpgradePeer(p.ID, udpAddr); err == nil {
				sendFunc = func(payload []byte) error {
					return n.kcpTransport.Send(p.ID, payload)
				}
			} else {
				sendFunc = func(payload []byte) error {
					n.tentConn.WriteTo(payload, udpAddr)
					return nil
				}
			}
		} else if udpAddr, ok := addr.(*net.UDPAddr); ok {
			sendFunc = func(payload []byte) error {
				n.tentConn.WriteTo(payload, udpAddr)
				return nil
			}
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

		encrypted, err := p.Session.Encrypt(plainFrame)
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
