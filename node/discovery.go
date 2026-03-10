package node

import (
	"net"
	"sort"
	"time"

	"github.com/shinyes/tenet/peer"
)

const (
	discoveryConnectConcurrencyLimit = 8
	discoveryConnectCooldown         = 10 * time.Second
	discoveryConnectPruneThreshold   = 256
	discoveryConnectSeenHardLimit    = 1024
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
	if !n.isDiscoverySourceAuthenticated(remoteAddr) {
		n.Config.Logger.Debug("ignore discovery request from unauthenticated source %v", remoteAddr)
		return
	}

	peerIDs := n.Peers.IDs()

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
func (n *Node) processDiscoveryResponse(remoteAddr net.Addr, payload []byte) {
	if !n.isDiscoverySourceAuthenticated(remoteAddr) {
		n.Config.Logger.Debug("ignore discovery response from unauthenticated source %v", remoteAddr)
		return
	}

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

func (n *Node) isDiscoverySourceAuthenticated(remoteAddr net.Addr) bool {
	if remoteAddr == nil {
		return false
	}

	n.mu.RLock()
	peerID, ok := n.addrToPeer[remoteAddr.String()]
	n.mu.RUnlock()
	if !ok {
		return false
	}

	_, exists := n.Peers.Get(peerID)
	return exists
}

func (n *Node) tryBeginDiscoveryConnect(peerID, addr string) bool {
	key := peerID + "|" + addr
	now := time.Now()

	n.mu.Lock()
	if len(n.discoveryConnectSeen) > discoveryConnectPruneThreshold {
		n.pruneDiscoveryConnectSeenLocked(now)
	}
	if until, exists := n.discoveryConnectSeen[key]; exists && now.Before(until) {
		n.mu.Unlock()
		return false
	}
	if len(n.discoveryConnectSeen) >= discoveryConnectSeenHardLimit {
		n.pruneDiscoveryConnectSeenLocked(now)
		if len(n.discoveryConnectSeen) >= discoveryConnectSeenHardLimit {
			n.trimDiscoveryConnectSeenLocked(discoveryConnectSeenHardLimit - 1)
		}
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

func (n *Node) pruneDiscoveryConnectSeenLocked(now time.Time) {
	for k, until := range n.discoveryConnectSeen {
		if !now.Before(until) {
			delete(n.discoveryConnectSeen, k)
		}
	}
}

func (n *Node) trimDiscoveryConnectSeenLocked(targetSize int) {
	if targetSize < 0 {
		targetSize = 0
	}
	if len(n.discoveryConnectSeen) <= targetSize {
		return
	}

	type seenEntry struct {
		key   string
		until time.Time
	}

	entries := make([]seenEntry, 0, len(n.discoveryConnectSeen))
	for key, until := range n.discoveryConnectSeen {
		entries = append(entries, seenEntry{key: key, until: until})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].until.Before(entries[j].until)
	})

	removeCount := len(n.discoveryConnectSeen) - targetSize
	if removeCount > len(entries) {
		removeCount = len(entries)
	}
	for i := 0; i < removeCount; i++ {
		delete(n.discoveryConnectSeen, entries[i].key)
	}
}

func (n *Node) endDiscoveryConnect() {
	select {
	case <-n.discoveryConnectSem:
	default:
	}
}
