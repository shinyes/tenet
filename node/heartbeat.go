package node

import (
	"net"
	"time"
)

// heartbeatLoop 心跳循环，定期向所有节点发送心跳并检测超时
func (n *Node) heartbeatLoop() {
	defer n.wg.Done()

	ticker := time.NewTicker(n.Config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.closing:
			return
		case <-ticker.C:
			n.sendHeartbeats()
			n.checkHeartbeatTimeouts()
		}
	}
}

// sendHeartbeats 向所有已连接节点发送心跳
func (n *Node) sendHeartbeats() {
	peerIDs := n.Peers.IDs()
	for _, peerID := range peerIDs {
		p, ok := n.Peers.Get(peerID)
		if !ok {
			continue
		}

		// 构造心跳包: TENT + 0x06 + 时间戳(8字节)
		packet := make([]byte, 13)
		copy(packet[0:4], []byte(MagicBytes))
		packet[4] = PacketTypeHeartbeat
		PutTimestamp(packet[5:], time.Now().UnixNano())

		transport, addr, conn := p.GetTransportInfo()
		n.sendRaw(conn, addr, transport, packet)
	}
}

// checkHeartbeatTimeouts 检查心跳超时的节点
func (n *Node) checkHeartbeatTimeouts() {
	peerIDs := n.Peers.IDs()
	now := time.Now()

	for _, peerID := range peerIDs {
		p, ok := n.Peers.Get(peerID)
		if !ok {
			continue
		}

		lastSeen := p.GetLastSeen()

		if now.Sub(lastSeen) > n.Config.HeartbeatTimeout {
			// 节点超时，移除并通知
			n.Config.Logger.Info("节点 %s 心跳超时断开", peerID[:8])
			n.removePeer(peerID)
		}
	}
}

// removePeer 移除节点并触发回调
func (n *Node) removePeer(peerID string) {
	p, ok := n.Peers.Get(peerID)
	if !ok {
		return
	}

	// 保存原始地址用于重连
	originalAddr := p.GetOriginalAddr()

	// 关闭 TCP 连接（如果有）
	p.Close()

	// 从 addrToPeer 中移除
	n.mu.Lock()
	for addr, pid := range n.addrToPeer {
		if pid == peerID {
			delete(n.addrToPeer, addr)
		}
	}
	n.mu.Unlock()

	// 从 PeerStore 中移除
	n.Peers.Remove(peerID)

	// 触发断开回调
	n.mu.RLock()
	callback := n.onPeerDisconnected
	n.mu.RUnlock()
	if callback != nil {
		go callback(peerID)
	}

	// 尝试重连（在后台进行）
	if originalAddr != "" {
		go func(addr string, targetPeerID string) {
			// 等待一段时间再尝试重连
			select {
			case <-n.closing:
				return
			case <-time.After(5 * time.Second):
			}

			// 再次检查节点是否仍在运行
			select {
			case <-n.closing:
				return
			default:
			}

			// 检查是否已经重新连接
			for _, pid := range n.Peers.IDs() {
				if pid == targetPeerID {
					return // 已重连
				}
			}

			n.Config.Logger.Info("尝试重新连接到 %s", addr)
			if err := n.Connect(addr); err != nil {
				n.Config.Logger.Error("重连 %s 失败: %v", addr, err)
			}
		}(originalAddr, peerID)
	}
}

// processHeartbeat 处理心跳请求，返回心跳响应
func (n *Node) processHeartbeat(conn net.Conn, remoteAddr net.Addr, transport string) {
	// 更新节点最后活动时间
	n.mu.RLock()
	peerID, ok := n.addrToPeer[remoteAddr.String()]
	n.mu.RUnlock()
	if ok {
		if p, exists := n.Peers.Get(peerID); exists {
			p.UpdateLastSeen()
		}
	}

	// 发送心跳响应: TENT + 0x07 + 时间戳(8字节)
	packet := make([]byte, 13)
	copy(packet[0:4], []byte(MagicBytes))
	packet[4] = PacketTypeHeartbeatAck
	PutTimestamp(packet[5:], time.Now().UnixNano())

	n.sendRaw(conn, remoteAddr, transport, packet)
}

// processHeartbeatAck 处理心跳响应
func (n *Node) processHeartbeatAck(remoteAddr net.Addr) {
	n.mu.RLock()
	peerID, ok := n.addrToPeer[remoteAddr.String()]
	n.mu.RUnlock()
	if !ok {
		return
	}

	p, exists := n.Peers.Get(peerID)
	if !exists {
		return
	}

	p.UpdateLastSeen()
}
