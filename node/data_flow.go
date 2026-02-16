package node

import "net"

// processData decrypts peer payload and dispatches framed app data.
func (n *Node) processData(remoteAddr net.Addr, payload []byte) {
	n.mu.RLock()
	peerID, ok := n.addrToPeer[remoteAddr.String()]
	n.mu.RUnlock()
	if !ok {
		return
	}

	p, ok := n.Peers.Get(peerID)
	if !ok {
		return
	}

	plaintext, err := p.Decrypt(payload)
	if err != nil {
		failures := p.IncDecryptFailures()
		n.Config.Logger.Warn("decrypt error from %s: %v", peerID[:8], err)
		if n.metrics != nil {
			n.metrics.IncErrorsTotal()
		}
		if failures >= n.Config.MaxConsecutiveDecryptFailures {
			n.Config.Logger.Warn(
				"Peer %s reached decrypt failure threshold (%d), starting fast re-handshake",
				peerID[:8],
				n.Config.MaxConsecutiveDecryptFailures,
			)
			n.triggerFastRehandshake(peerID, p)
		}
		return
	}
	p.ResetDecryptFailures()

	if len(plaintext) < 1 {
		return
	}
	frameType := plaintext[0]
	frameData := plaintext[1:]

	p.AddBytesReceived(int64(len(frameData)))
	if n.metrics != nil {
		n.metrics.AddBytesReceived(int64(len(frameData)))
	}

	switch frameType {
	case FrameTypeSingle:
		p.ResetReassembly()
		n.handleAppFrame(peerID, p, frameData)
	case FrameTypeFirst:
		p.StartReassembly(frameData)
	case FrameTypeMiddle:
		ok, overflow := p.AppendReassembly(frameData, MaxReassemblySize)
		if overflow {
			n.Config.Logger.Warn("peer %s reassembly buffer overflow, dropped", shortPeerID(peerID))
		}
		if !ok {
			return
		}
	case FrameTypeLast:
		completeData, ok, overflow := p.FinishReassembly(frameData, MaxReassemblySize)
		if overflow {
			n.Config.Logger.Warn("peer %s reassembly buffer overflow, dropped", shortPeerID(peerID))
		}
		if !ok {
			return
		}
		n.handleAppFrame(peerID, p, completeData)
	}
}
