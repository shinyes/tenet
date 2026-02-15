package node

import (
	"net"
	"time"

	"github.com/shinyes/tenet/peer"
)

const (
	discoveryConnectConcurrencyLimit = 8
	discoveryConnectCooldown         = 10 * time.Second
	discoveryConnectPruneThreshold   = 256
)

// sendDiscoveryRequest sends a node discovery request packet.
func (n *Node) sendDiscoveryRequest(p *peer.Peer) {
	packet := make([]byte, 5)
	copy(packet[0:4], []byte("TENT"))
	packet[4] = PacketTypeDiscoveryReq

	transport, addr, conn := p.GetTransportInfo()
	n.sendRaw(conn, addr, transport, packet)
}

// processDiscoveryRequest replies with known peers.
func (n *Node) processDiscoveryRequest(conn net.Conn, remoteAddr net.Addr, transport string, reqPayload []byte) {
	n.mu.RLock()
	peerIDs := n.Peers.IDs()
	n.mu.RUnlock()

	var entries []byte
	count := 0

	for _, pid := range peerIDs {
		p, ok := n.Peers.Get(pid)
		if !ok {
			continue
		}

		_, addr, _ := p.GetTransportInfo()
		if addr == nil {
			continue
		}
		addrStr := addr.String()

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

// processDiscoveryResponse parses discovery response and attempts unknown peer connections.
func (n *Node) processDiscoveryResponse(payload []byte) {
	if len(payload) < 2 {
		return
	}

	count := int(payload[0])<<8 | int(payload[1])
	offset := 2

	for i := 0; i < count && offset < len(payload); i++ {
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

		if peerID == n.localPeerID {
			continue
		}
		if _, exists := n.Peers.Get(peerID); exists {
			continue
		}
		if !n.tryBeginDiscoveryConnect(peerID, addrStr) {
			n.Config.Logger.Debug("skip discovery connect to %s (%s): throttled or duplicate", shortPeerID(peerID), addrStr)
			continue
		}

		n.Config.Logger.Info("discovered new peer %s (%s), trying to connect", shortPeerID(peerID), addrStr)
		go func(addr string) {
			defer n.endDiscoveryConnect()
			if err := n.Connect(addr); err != nil {
				n.Config.Logger.Error("connect %s failed: %v", addr, err)
			}
		}(addrStr)
	}
}

func (n *Node) tryBeginDiscoveryConnect(peerID, addr string) bool {
	key := peerID + "|" + addr
	now := time.Now()

	n.mu.Lock()
	if len(n.discoveryConnectSeen) > discoveryConnectPruneThreshold {
		for k, until := range n.discoveryConnectSeen {
			if now.After(until) {
				delete(n.discoveryConnectSeen, k)
			}
		}
	}
	if until, exists := n.discoveryConnectSeen[key]; exists && now.Before(until) {
		n.mu.Unlock()
		return false
	}
	n.discoveryConnectSeen[key] = now.Add(discoveryConnectCooldown)
	sem := n.discoveryConnectSem
	n.mu.Unlock()

	select {
	case sem <- struct{}{}:
		return true
	default:
		n.mu.Lock()
		delete(n.discoveryConnectSeen, key)
		n.mu.Unlock()
		return false
	}
}

func (n *Node) endDiscoveryConnect() {
	select {
	case <-n.discoveryConnectSem:
	default:
	}
}
