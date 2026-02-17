package node

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/shinyes/tenet/crypto"
	"github.com/shinyes/tenet/nat"
)

type connectResult struct {
	Conn      net.Conn
	Transport string
	Err       error
}

// connectContext orchestrates TCP/UDP hole punching and fallback handling.
func (n *Node) connectContext(ctx context.Context, remoteAddr string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	remoteUDPAddr, remoteTCPAddr, err := n.resolveConnectAddrs(remoteAddr)
	if err != nil {
		return err
	}

	udpHandshakeState, tcpHandshakeState, udpHandshakePacket, tcpHandshakePacket, err := n.buildInitiatorHandshakePackets()
	if err != nil {
		return err
	}

	resultCh, punchCancel := n.startPunchWorkers(ctx, remoteUDPAddr, remoteTCPAddr, udpHandshakePacket)
	defer punchCancel()

	n.registerPendingHandshakes(remoteUDPAddr, remoteTCPAddr, udpHandshakeState, tcpHandshakeState)

	return n.awaitConnectResult(ctx, remoteAddr, remoteUDPAddr, tcpHandshakePacket, resultCh)
}

// resolveConnectAddrs parses target UDP/TCP addresses and validates node state.
func (n *Node) resolveConnectAddrs(remoteAddr string) (*net.UDPAddr, *net.TCPAddr, error) {
	if n.conn == nil || n.LocalAddr == nil {
		return nil, nil, fmt.Errorf("node not started")
	}

	remoteUDPAddr, err := net.ResolveUDPAddr("udp", remoteAddr)
	if err != nil {
		return nil, nil, err
	}
	remoteTCPAddr, err := net.ResolveTCPAddr("tcp", remoteAddr)
	if err != nil {
		return nil, nil, err
	}
	return remoteUDPAddr, remoteTCPAddr, nil
}

// buildInitiatorHandshakePackets builds independent UDP/TCP handshake payloads.
func (n *Node) buildInitiatorHandshakePackets() (*crypto.HandshakeState, *crypto.HandshakeState, []byte, []byte, error) {
	udpHandshakeState, udpHandshakeMessage, err := crypto.NewInitiatorHandshake(
		n.Identity.NoisePrivateKey[:],
		n.Identity.NoisePublicKey[:],
		[]byte(n.Config.NetworkPassword),
	)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	tcpHandshakeState, tcpHandshakeMessage, err := crypto.NewInitiatorHandshake(
		n.Identity.NoisePrivateKey[:],
		n.Identity.NoisePublicKey[:],
		[]byte(n.Config.NetworkPassword),
	)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	udpHandshakePacket := make([]byte, 5+len(udpHandshakeMessage))
	copy(udpHandshakePacket[0:4], []byte("TENT"))
	udpHandshakePacket[4] = PacketTypeHandshake
	copy(udpHandshakePacket[5:], udpHandshakeMessage)

	tcpHandshakePacket := make([]byte, 5+len(tcpHandshakeMessage))
	copy(tcpHandshakePacket[0:4], []byte("TENT"))
	tcpHandshakePacket[4] = PacketTypeHandshake
	copy(tcpHandshakePacket[5:], tcpHandshakeMessage)

	return udpHandshakeState, tcpHandshakeState, udpHandshakePacket, tcpHandshakePacket, nil
}

// registerPendingHandshakes stores temporary handshake state keys.
func (n *Node) registerPendingHandshakes(remoteUDPAddr *net.UDPAddr, remoteTCPAddr *net.TCPAddr, udpHandshakeState, tcpHandshakeState *crypto.HandshakeState) {
	udpStateKey := "udp://" + remoteUDPAddr.String()
	tcpStateKey := "tcp://" + remoteTCPAddr.String()

	n.mu.Lock()
	n.pendingHandshakes[udpStateKey] = udpHandshakeState
	if remoteTCPAddr != nil {
		n.pendingHandshakes[tcpStateKey] = tcpHandshakeState
	}
	n.mu.Unlock()

	go func() {
		time.Sleep(30 * time.Second)
		n.mu.Lock()
		delete(n.pendingHandshakes, udpStateKey)
		delete(n.pendingHandshakes, tcpStateKey)
		n.mu.Unlock()
	}()
}

// startPunchWorkers launches TCP and UDP punch workers.
func (n *Node) startPunchWorkers(ctx context.Context, remoteUDPAddr *net.UDPAddr, remoteTCPAddr *net.TCPAddr, udpHandshakePacket []byte) (chan connectResult, context.CancelFunc) {
	resultCh := make(chan connectResult, 2)
	punchCtx, punchCancel := context.WithCancel(ctx)

	go func() {
		select {
		case <-n.closing:
			punchCancel()
		case <-punchCtx.Done():
		}
	}()

	go func() {
		tcpCtx, cancel := context.WithTimeout(punchCtx, 10*time.Second)
		defer cancel()

		puncher := nat.NewTCPHolePuncher()
		conn, err := puncher.Punch(tcpCtx, n.tcpLocalPort, remoteTCPAddr)
		if err != nil {
			resultCh <- connectResult{Transport: "tcp", Err: err}
			return
		}
		resultCh <- connectResult{Conn: conn, Transport: "tcp"}
	}()

	go func() {
		timeout := time.After(2 * time.Second)
		for {
			select {
			case <-punchCtx.Done():
				return
			case <-timeout:
				resultCh <- connectResult{Transport: "udp"}
				return
			default:
				_, _ = n.conn.WriteToUDP(udpHandshakePacket, remoteUDPAddr)
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()

	return resultCh, punchCancel
}

// awaitConnectResult resolves connect outcome from worker events and timeout.
func (n *Node) awaitConnectResult(ctx context.Context, remoteAddr string, remoteUDPAddr *net.UDPAddr, tcpHandshakePacket []byte, resultCh chan connectResult) error {
	connectTimeout := n.Config.DialTimeout
	if connectTimeout <= 0 {
		connectTimeout = 5 * time.Second
	}
	timeout := time.NewTimer(connectTimeout)
	defer timeout.Stop()

	var tcpPunchErr error

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-n.closing:
			return fmt.Errorf("node is closing")
		case result := <-resultCh:
			switch result.Transport {
			case "tcp":
				if result.Conn != nil {
					if err := n.writeTCPPacket(result.Conn, tcpHandshakePacket); err != nil {
						result.Conn.Close()
						return err
					}
					go n.handleTCP(result.Conn)
					return nil
				}
				if result.Err != nil {
					tcpPunchErr = result.Err
				}
			case "udp":
				n.scheduleRelayFallback(ctx, remoteAddr, remoteUDPAddr)
				n.awaitLateTCPUpgrade(ctx, resultCh, tcpHandshakePacket)
				return nil
			default:
				if result.Conn != nil {
					result.Conn.Close()
				}
				if result.Err != nil {
					tcpPunchErr = result.Err
				}
			}
		case <-timeout.C:
			if n.relayManager != nil {
				if err := n.connectViaRelay(remoteAddr); err == nil {
					return nil
				}
			}
			if tcpPunchErr != nil {
				return tcpPunchErr
			}
			return fmt.Errorf("connect timeout")
		}
	}
}

// scheduleRelayFallback triggers relay connect if direct mapping does not appear.
func (n *Node) scheduleRelayFallback(ctx context.Context, remoteAddr string, remoteUDPAddr *net.UDPAddr) {
	if !n.Config.EnableRelay {
		return
	}
	addrKey := remoteUDPAddr.String()
	go func() {
		select {
		case <-n.closing:
			return
		case <-ctx.Done():
			return
		case <-time.After(n.Config.DialTimeout):
		}
		n.mu.RLock()
		_, ok := n.addrToPeer[addrKey]
		n.mu.RUnlock()
		if !ok {
			n.connectViaRelay(remoteAddr)
		}
	}()
}

// awaitLateTCPUpgrade waits a short window for a late successful TCP upgrade.
func (n *Node) awaitLateTCPUpgrade(ctx context.Context, resultCh chan connectResult, tcpHandshakePacket []byte) {
	go func() {
		lateTimeout := time.NewTimer(10 * time.Second)
		defer lateTimeout.Stop()
		for {
			select {
			case <-n.closing:
				return
			case <-ctx.Done():
				return
			case result := <-resultCh:
				if result.Transport == "tcp" && result.Conn != nil {
					if err := n.writeTCPPacket(result.Conn, tcpHandshakePacket); err != nil {
						result.Conn.Close()
						return
					}
					go n.handleTCP(result.Conn)
					return
				}
				if result.Conn != nil {
					result.Conn.Close()
				}
			case <-lateTimeout.C:
				return
			}
		}
	}()
}
