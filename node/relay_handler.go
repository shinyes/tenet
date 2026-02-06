package node

import (
	"fmt"
	"net"

	"github.com/shinyes/tenet/crypto"
	"github.com/shinyes/tenet/nat"
)

// getRelayAuthenticator 获取或创建中继认证器
// 每个 Node 实例使用自己的认证器，避免全局状态问题
func (n *Node) getRelayAuthenticator() *nat.RelayAuthenticator {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.relayAuth == nil {
		n.relayAuth = nat.NewRelayAuthenticator(&nat.RelayAuthConfig{
			Enabled:         n.Config.EnableRelayAuth,
			TokenTTL:        n.Config.RelayAuthTTL,
			NetworkPassword: n.Config.NetworkPassword,
		})
	}
	return n.relayAuth
}

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
// 格式: [Mode(1)] [AddrLen(1)] [Addr] [AuthTokenLen(1)] [AuthToken(可选)] [Inner]
func (n *Node) buildRelayPacket(mode byte, targetAddr *net.UDPAddr, inner []byte) ([]byte, error) {
	if targetAddr == nil {
		return nil, fmt.Errorf("目标地址为空")
	}
	addrStr := targetAddr.String()
	if len(addrStr) > 255 {
		return nil, fmt.Errorf("目标地址过长")
	}

	// 生成认证令牌（如果启用）
	var authTokenData []byte
	if n.Config.EnableRelayAuth {
		auth := n.getRelayAuthenticator()
		token := auth.GenerateTokenWithHMAC(n.Identity.ID, [16]byte{}) // 目标ID未知时使用空
		if token != nil {
			authTokenData = token.Encode()
		}
	}

	// 确保认证数据不超过 255 字节
	if len(authTokenData) > 255 {
		authTokenData = authTokenData[:255]
	}

	payloadLen := 2 + len(addrStr) + 1 + len(authTokenData) + len(inner)
	payload := make([]byte, payloadLen)
	payload[0] = mode
	payload[1] = byte(len(addrStr))
	offset := 2
	copy(payload[offset:], []byte(addrStr))
	offset += len(addrStr)
	payload[offset] = byte(len(authTokenData))
	offset++
	copy(payload[offset:], authTokenData)
	offset += len(authTokenData)
	copy(payload[offset:], inner)

	packet := make([]byte, 5+len(payload))
	copy(packet[0:4], []byte("TENT"))
	packet[4] = PacketTypeRelay
	copy(packet[5:], payload)
	return packet, nil
}

// handleRelayPacket 处理中继封装包
// 格式: [Mode(1)] [AddrLen(1)] [Addr] [AuthTokenLen(1)] [AuthToken(可选)] [Inner]
func (n *Node) handleRelayPacket(origin *net.UDPAddr, payload []byte) {
	if !n.Config.EnableRelay {
		return
	}
	if origin == nil || len(payload) < 3 {
		return
	}

	mode := payload[0]
	addrLen := int(payload[1])
	if len(payload) < 2+addrLen+1 {
		return
	}
	addrStr := string(payload[2 : 2+addrLen])
	offset := 2 + addrLen

	// 解析认证令牌
	authTokenLen := int(payload[offset])
	offset++
	if len(payload) < offset+authTokenLen {
		return
	}
	authTokenData := payload[offset : offset+authTokenLen]
	offset += authTokenLen

	inner := payload[offset:]
	if len(inner) == 0 {
		return
	}

	// 验证认证令牌（如果启用）
	if n.Config.EnableRelayAuth && authTokenLen > 0 {
		auth := n.getRelayAuthenticator()
		token, err := nat.DecodeToken(authTokenData)
		if err != nil {
			n.Config.Logger.Debug("中继认证令牌解码失败: %v", err)
			if n.metrics != nil {
				n.metrics.IncRelayAuthFailed()
			}
			return
		}
		if err := auth.VerifyToken(token, nil); err != nil {
			n.Config.Logger.Debug("中继认证失败: %v", err)
			if n.metrics != nil {
				n.metrics.IncRelayAuthFailed()
			}
			return
		}
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
