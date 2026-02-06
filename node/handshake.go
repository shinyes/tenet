package node

import (
	"crypto/sha256"
	"fmt"
	"net"
	"time"

	"github.com/shinyes/tenet/crypto"
	"github.com/shinyes/tenet/internal/protocol"
	"github.com/shinyes/tenet/peer"
)

// processHandshake 处理握手消息
// 优化：缩小锁范围，只在访问共享数据时持锁
func (n *Node) processHandshake(conn net.Conn, remoteAddr net.Addr, transport string, payload []byte) {
	addrStr := remoteAddr.String()
	stateKey := fmt.Sprintf("%s://%s", transport, addrStr)

	// 1. 获取或创建握手状态（需要锁）
	n.mu.Lock()
	hs, exists := n.pendingHandshakes[stateKey]
	if !exists {
		var err error
		hs, err = crypto.NewResponderHandshake(
			n.Identity.NoisePrivateKey[:],
			n.Identity.NoisePublicKey[:],
			[]byte(n.Config.NetworkPassword),
		)
		if err != nil {
			n.mu.Unlock()
			n.Config.Logger.Error("创建握手失败: %v", err)
			return
		}
		n.pendingHandshakes[stateKey] = hs
	}
	n.mu.Unlock()

	// 2. 处理握手消息（不需要锁，Noise 状态机独立）
	response, session, err := hs.ProcessMessage(payload)
	if err != nil {
		n.mu.RLock()
		_, isConnected := n.addrToPeer[addrStr]
		n.mu.RUnlock()

		if isConnected {
			return // 冗余包，安全忽略
		}

		n.Config.Logger.Warn("来自 %s 的握手错误: %v", addrStr, err)
		if n.metrics != nil {
			n.metrics.IncHandshakesFailed()
		}
		n.mu.Lock()
		delete(n.pendingHandshakes, stateKey)
		n.mu.Unlock()
		return
	}

	// 3. 发送响应（不需要锁）
	if response != nil {
		packet := protocol.BuildHandshakePacket(response)
		n.sendRaw(conn, remoteAddr, transport, packet)
	}

	// 4. 会话建立后的处理
	if session != nil {
		remotePub := session.RemotePublicKey()
		idHash := sha256.Sum256(remotePub)
		peerID := fmt.Sprintf("%x", idHash[:16])

		// 传输升级逻辑：
		existingPeer, peerExists := n.Peers.Get(peerID)
		if peerExists {
			n.handleExistingPeer(existingPeer, conn, remoteAddr, transport, session, addrStr, peerID, stateKey)
			return
		}

		// 新对端处理
		n.mu.Lock()
		delete(n.pendingHandshakes, stateKey)

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

		onConnected := n.onPeerConnected
		n.mu.Unlock()

		// 检查最大连接数（无需锁）
		if n.Config.MaxPeers > 0 && n.Peers.Count() >= n.Config.MaxPeers {
			n.Config.Logger.Warn("已达到最大连接数 %d，拒绝新连接 %s", n.Config.MaxPeers, peerID[:8])
			return
		}

		p := &peer.Peer{
			ID:           peerID,
			Addr:         remoteAddr,
			OriginalAddr: addrStr,
			Conn:         conn,
			Transport:    transport,
			LinkMode:     linkMode,
			RelayTarget:  relayTarget,
			Session:      session,
			LastSeen:     time.Now(),
		}
		n.Peers.Add(p)

		// 更新握手统计
		if n.metrics != nil {
			n.metrics.IncHandshakesTotal()
			n.metrics.IncConnectionsTotal()
		}

		n.mu.Lock()
		n.addrToPeer[addrStr] = peerID
		n.mu.Unlock()

		n.registerRelayCandidate(peerID, remoteAddr)
		go n.sendDiscoveryRequest(p)

		go n.syncChannelsWithPeer(peerID)
		if onConnected != nil {
			go onConnected(peerID)
		}
	}
}

// handleExistingPeer 处理已存在对端的握手
func (n *Node) handleExistingPeer(existingPeer *peer.Peer, conn net.Conn, remoteAddr net.Addr, transport string, session *crypto.Session, addrStr, peerID, stateKey string) {
	// 传输升级逻辑
	if transport == "tcp" && existingPeer.Transport != "tcp" {
		n.Config.Logger.Info("升级成功: 节点 %s 已切换至 TCP 链路", peerID[:8])
		existingPeer.UpgradeTransport(remoteAddr, conn, transport, session)

		n.mu.Lock()
		n.addrToPeer[addrStr] = peerID
		delete(n.pendingHandshakes, stateKey)
		n.mu.Unlock()

		n.registerRelayCandidate(peerID, remoteAddr)
		go n.syncChannelsWithPeer(peerID) // 同步频道状态
		return
	}

	// 检查中继模式
	n.mu.Lock()
	if target, ok := n.relayPendingTarget[addrStr]; ok {
		existingPeer.SetLinkMode("relay", target)
		delete(n.relayPendingTarget, addrStr)
	} else if inbound := n.relayInbound[addrStr]; inbound {
		existingPeer.SetLinkMode("relay", nil)
		delete(n.relayInbound, addrStr)
	}
	delete(n.pendingHandshakes, stateKey)
	n.mu.Unlock()

	existingPeer.UpdateLastSeen()
	n.registerRelayCandidate(peerID, remoteAddr)
}
