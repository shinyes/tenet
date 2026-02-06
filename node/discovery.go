package node

import (
	"net"

	"github.com/cykyes/tenet/peer"
)

// sendDiscoveryRequest 向指定节点发送节点发现请求
func (n *Node) sendDiscoveryRequest(p *peer.Peer) {
	// 构建发现请求包：TENT + 0x04 (无 Payload)
	packet := make([]byte, 5)
	copy(packet[0:4], []byte("TENT"))
	packet[4] = PacketTypeDiscoveryReq

	transport, addr, conn := p.GetTransportInfo()
	n.sendRaw(conn, addr, transport, packet)
}

// processDiscoveryRequest 处理节点发现请求，返回已知节点列表
func (n *Node) processDiscoveryRequest(conn net.Conn, remoteAddr net.Addr, transport string, reqPayload []byte) {
	// 发现请求不包含 Payload，直接忽略任何额外数据
	// 直接响应已知节点列表

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
	packet[4] = PacketTypeDiscoveryResp
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
		n.Config.Logger.Info("通过节点发现发现新节点 %s (%s)，尝试连接...", peerID[:8], addrStr)
		go func(addr string) {
			if err := n.Connect(addr); err != nil {
				n.Config.Logger.Error("连接 %s 失败: %v", addr, err)
			}
		}(addrStr)
	}
}
