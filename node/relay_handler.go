package node

import (
	"fmt"
	"net"

	"github.com/cykyes/tenet/crypto"
)

// connectViaRelay 使用中继发送握手包
func (n *Node) connectViaRelay(targetAddrStr string) error {
	if n.relayManager == nil {
		return fmt.Errorf("中继管理器未初始化")
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
	packet[4] = PacketTypeHandshake
	copy(packet[5:], msg)

	relayPacket, err := n.buildRelayPacket(RelayModeForward, targetAddr, packet)
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
		return nil, fmt.Errorf("目标地址为空")
	}
	addrStr := targetAddr.String()
	if len(addrStr) > 255 {
		return nil, fmt.Errorf("目标地址过长")
	}
	payload := make([]byte, 2+len(addrStr)+len(inner))
	payload[0] = mode
	payload[1] = byte(len(addrStr))
	copy(payload[2:], []byte(addrStr))
	copy(payload[2+len(addrStr):], inner)

	packet := make([]byte, 5+len(payload))
	copy(packet[0:4], []byte("TENT"))
	packet[4] = PacketTypeRelay
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

	if mode == RelayModeForward {
		// 作为中继转发给目标
		n.mu.Lock()
		n.relayForward[targetAddr.String()] = origin
		n.mu.Unlock()

		forwardPacket, err := n.buildRelayPacket(RelayModeTarget, targetAddr, inner)
		if err != nil {
			n.Config.Logger.Error("构造中继包失败: %v", err)
			return
		}
		if _, err := n.conn.WriteToUDP(forwardPacket, targetAddr); err != nil {
			n.Config.Logger.Error("中继转发失败: %v", err)
		}
		return
	}

	if mode == RelayModeTarget {
		// 目标侧解封装并本地处理
		if len(inner) < 5 || string(inner[0:4]) != "TENT" {
			return
		}
		packetType := inner[4]
		innerPayload := make([]byte, len(inner)-5)
		copy(innerPayload, inner[5:])

		n.mu.Lock()
		n.relayInbound[origin.String()] = true
		n.mu.Unlock()

		n.handlePacket(nil, origin, "udp", packetType, innerPayload)
	}
}
