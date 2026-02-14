package node

import (
	"errors"
	"fmt"
	"time"

	"github.com/shinyes/tenet/crypto"
	"github.com/shinyes/tenet/internal/protocol"
	"github.com/shinyes/tenet/peer"
)

var errFastRehandshakeBusy = errors.New("pending handshake exists")

// triggerFastRehandshake starts an immediate re-handshake to recover from
// nonce/cipher desync without waiting for reconnect backoff.
func (n *Node) triggerFastRehandshake(peerID string, p *peer.Peer) {
	if p == nil {
		return
	}
	if !p.TryBeginRehandshake() {
		return
	}
	cfg := n.Config

	now := time.Now()
	allowed, wait := p.ReserveRehandshakeAttempt(now, cfg.FastRehandshakeWindow, cfg.FastRehandshakeMaxAttemptsWindow)
	if !allowed {
		p.EndRehandshake()
		if wait > 0 {
			n.Config.Logger.Debug("peer %s fast re-handshake throttled for %v", shortPeerID(peerID), wait)
		}
		return
	}

	p.ResetDecryptFailures()

	targetAddr := p.GetOriginalAddr()
	if targetAddr == "" {
		_, addr, _ := p.GetTransportInfo()
		if addr != nil {
			targetAddr = addr.String()
		}
	}

	shortID := shortPeerID(peerID)

	if targetAddr == "" {
		p.EndRehandshake()
		n.Config.Logger.Warn("peer %s has no address for fast re-handshake", shortID)
		n.removePeerWithReconnect(peerID, true)
		return
	}

	n.Config.Logger.Warn("peer %s decrypt desync, start fast re-handshake via %s", shortID, targetAddr)

	go func() {
		defer p.EndRehandshake()

		select {
		case <-n.closing:
			return
		default:
		}

		if n.metrics != nil {
			n.metrics.IncFastRehandshakeAttempts()
		}

		if err := n.sendFastRehandshake(p); err != nil {
			if errors.Is(err, errFastRehandshakeBusy) {
				n.Config.Logger.Debug("peer %s fast re-handshake skipped: %v", shortID, err)
				return
			}

			failures, delay := p.RecordRehandshakeFailure(time.Now(), cfg.FastRehandshakeBaseBackoff, cfg.FastRehandshakeMaxBackoff)
			if n.metrics != nil {
				n.metrics.IncFastRehandshakeFailed()
			}

			n.Config.Logger.Warn(
				"fast re-handshake to peer %s failed (%d/%d), retry in %v: %v",
				shortID,
				failures,
				cfg.FastRehandshakeFailThreshold,
				delay,
				err,
			)

			if failures >= cfg.FastRehandshakeFailThreshold {
				n.Config.Logger.Warn("peer %s fast re-handshake failed repeatedly, fallback to reconnect", shortID)
				n.removePeerWithReconnect(peerID, true)
			}
			return
		}

		p.RecordRehandshakeSuccess(time.Now(), cfg.FastRehandshakeBaseBackoff)
		if n.metrics != nil {
			n.metrics.IncFastRehandshakeSuccess()
		}
		n.Config.Logger.Info("fast re-handshake initiated for peer %s", shortID)
	}()
}

func (n *Node) sendFastRehandshake(p *peer.Peer) error {
	if p == nil {
		return fmt.Errorf("peer is nil")
	}

	transport, addr, conn := p.GetTransportInfo()
	if addr == nil {
		return fmt.Errorf("peer address is nil")
	}

	if transport != "tcp" && transport != "udp" {
		return fmt.Errorf("unsupported transport for re-handshake: %s", transport)
	}

	hs, msg, err := crypto.NewInitiatorHandshake(
		n.Identity.NoisePrivateKey[:],
		n.Identity.NoisePublicKey[:],
		[]byte(n.Config.NetworkPassword),
	)
	if err != nil {
		return err
	}

	stateKey := fmt.Sprintf("%s://%s", transport, addr.String())
	n.mu.Lock()
	if _, exists := n.pendingHandshakes[stateKey]; exists {
		n.mu.Unlock()
		return fmt.Errorf("%w: %s", errFastRehandshakeBusy, stateKey)
	}
	n.pendingHandshakes[stateKey] = hs
	n.mu.Unlock()

	packet := protocol.BuildHandshakePacket(msg)
	if err := n.sendRaw(conn, addr, transport, packet); err != nil {
		n.mu.Lock()
		delete(n.pendingHandshakes, stateKey)
		n.mu.Unlock()
		return err
	}

	go func() {
		time.Sleep(n.Config.FastRehandshakePendingTTL)
		n.mu.Lock()
		delete(n.pendingHandshakes, stateKey)
		n.mu.Unlock()
	}()

	return nil
}

func shortPeerID(peerID string) string {
	if len(peerID) > 8 {
		return peerID[:8]
	}
	return peerID
}
