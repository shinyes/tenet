package node

import (
	"net"
	"time"

	"github.com/cykyes/tenet/internal/protocol"
)

// heartbeatLoop 心跳循环，定期向所有节点发送心跳并检测超时
func (n *Node) heartbeatLoop() {
	defer n.wg.Done()

	ticker := time.NewTicker(n.Config.HeartbeatInterval)
	defer ticker.Stop()

	// 清理计时器：每分钟清理一次过期的映射，防止内存泄漏
	cleanupTicker := time.NewTicker(time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-n.closing:
			return
		case <-ticker.C:
			n.sendHeartbeats()
			n.checkHeartbeatTimeouts()
		case <-cleanupTicker.C:
			n.cleanupStaleMappings()
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
		protocol.PutTimestamp(packet[5:], time.Now().UnixNano())

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
	n.removePeerWithReconnect(peerID, true)
}

// removePeerWithReconnect 移除节点并可选地触发重连
func (n *Node) removePeerWithReconnect(peerID string, shouldReconnect bool) {
	p, ok := n.Peers.Get(peerID)
	if !ok {
		return
	}

	// 更新断开统计
	if n.metrics != nil {
		n.metrics.IncDisconnectsTotal()
	}

	// 保存原始地址用于重连
	originalAddr := p.GetOriginalAddr()

	// 关闭 KCP 会话（如果有）
	if n.kcpTransport != nil {
		n.kcpTransport.RemoveSession(peerID)
	}

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

	// 使用重连管理器进行重连（如果启用）
	if shouldReconnect && originalAddr != "" && n.reconnectManager != nil {
		n.reconnectManager.ScheduleReconnect(peerID, originalAddr)
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
	protocol.PutTimestamp(packet[5:], time.Now().UnixNano())

	n.sendRaw(conn, remoteAddr, transport, packet)
}

// cleanupStaleMappings 清理过期的内部映射，防止内存泄漏
func (n *Node) cleanupStaleMappings() {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now().Unix()
	handshakeTimeout := int64(60) // 握手状态超时时间（秒）

	// 清理不再有效的 addrToPeer 映射
	for addr, peerID := range n.addrToPeer {
		if _, exists := n.Peers.Get(peerID); !exists {
			delete(n.addrToPeer, addr)
		}
	}

	// 清理过期的 pendingHandshakes（超过 60 秒未完成的握手）
	for key, hs := range n.pendingHandshakes {
		if now-hs.CreatedAt() > handshakeTimeout {
			delete(n.pendingHandshakes, key)
			n.Config.Logger.Debug("清理过期握手状态: %s", key)
		}
	}

	// 清理 relayForward 映射（保留最近活跃的）
	// 只保留对应 peer 仍然存在的映射
	for targetAddr := range n.relayForward {
		found := false
		for _, peerID := range n.Peers.IDs() {
			if p, ok := n.Peers.Get(peerID); ok {
				_, addr, _ := p.GetTransportInfo()
				if addr != nil && addr.String() == targetAddr {
					found = true
					break
				}
			}
		}
		if !found {
			delete(n.relayForward, targetAddr)
		}
	}

	// 清理 relayInbound 映射
	for addr := range n.relayInbound {
		if _, exists := n.addrToPeer[addr]; !exists {
			delete(n.relayInbound, addr)
		}
	}

	// 清理 relayPendingTarget 映射
	for addr := range n.relayPendingTarget {
		if _, exists := n.addrToPeer[addr]; !exists {
			delete(n.relayPendingTarget, addr)
		}
	}
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
