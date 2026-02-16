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
func (n *Node) connectContext(ctx context.Context, addrStr string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	rUDPAddr, rTCPAddr, err := n.resolveConnectAddrs(addrStr)
	if err != nil {
		return err
	}

	hsUDP, hsTCP, packetUDP, packetTCP, err := n.buildInitiatorHandshakePackets()
	if err != nil {
		return err
	}

	resultChan, punchCancel := n.startPunchWorkers(ctx, rUDPAddr, rTCPAddr, packetUDP)
	defer punchCancel()

	n.registerPendingHandshakes(rUDPAddr, rTCPAddr, hsUDP, hsTCP)

	return n.awaitConnectResult(ctx, addrStr, rUDPAddr, packetTCP, resultChan)
}

// resolveConnectAddrs parses target UDP/TCP addresses and validates node state.
func (n *Node) resolveConnectAddrs(addrStr string) (*net.UDPAddr, *net.TCPAddr, error) {
	if n.conn == nil || n.LocalAddr == nil {
		return nil, nil, fmt.Errorf("node not started")
	}

	rUDPAddr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		return nil, nil, err
	}
	rTCPAddr, err := net.ResolveTCPAddr("tcp", addrStr)
	if err != nil {
		return nil, nil, err
	}
	return rUDPAddr, rTCPAddr, nil
}

// buildInitiatorHandshakePackets builds independent UDP/TCP handshake payloads.
func (n *Node) buildInitiatorHandshakePackets() (*crypto.HandshakeState, *crypto.HandshakeState, []byte, []byte, error) {
	hsUDP, msgUDP, err := crypto.NewInitiatorHandshake(
		n.Identity.NoisePrivateKey[:],
		n.Identity.NoisePublicKey[:],
		[]byte(n.Config.NetworkPassword),
	)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	hsTCP, msgTCP, err := crypto.NewInitiatorHandshake(
		n.Identity.NoisePrivateKey[:],
		n.Identity.NoisePublicKey[:],
		[]byte(n.Config.NetworkPassword),
	)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	packetUDP := make([]byte, 5+len(msgUDP))
	copy(packetUDP[0:4], []byte("TENT"))
	packetUDP[4] = PacketTypeHandshake
	copy(packetUDP[5:], msgUDP)

	packetTCP := make([]byte, 5+len(msgTCP))
	copy(packetTCP[0:4], []byte("TENT"))
	packetTCP[4] = PacketTypeHandshake
	copy(packetTCP[5:], msgTCP)

	return hsUDP, hsTCP, packetUDP, packetTCP, nil
}

// registerPendingHandshakes stores temporary handshake state keys.
func (n *Node) registerPendingHandshakes(rUDPAddr *net.UDPAddr, rTCPAddr *net.TCPAddr, hsUDP, hsTCP *crypto.HandshakeState) {
	udpStateKey := "udp://" + rUDPAddr.String()
	tcpStateKey := "tcp://" + rTCPAddr.String()

	n.mu.Lock()
	n.pendingHandshakes[udpStateKey] = hsUDP
	if rTCPAddr != nil {
		n.pendingHandshakes[tcpStateKey] = hsTCP
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
func (n *Node) startPunchWorkers(ctx context.Context, rUDPAddr *net.UDPAddr, rTCPAddr *net.TCPAddr, packetUDP []byte) (chan connectResult, context.CancelFunc) {
	resultChan := make(chan connectResult, 2)
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
		conn, err := puncher.Punch(tcpCtx, n.tcpLocalPort, rTCPAddr)
		if err != nil {
			resultChan <- connectResult{Transport: "tcp", Err: err}
			return
		}
		resultChan <- connectResult{Conn: conn, Transport: "tcp"}
	}()

	go func() {
		timeout := time.After(2 * time.Second)
		for {
			select {
			case <-punchCtx.Done():
				return
			case <-timeout:
				resultChan <- connectResult{Transport: "udp"}
				return
			default:
				_, _ = n.conn.WriteToUDP(packetUDP, rUDPAddr)
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()

	return resultChan, punchCancel
}

// awaitConnectResult resolves connect outcome from worker events and timeout.
func (n *Node) awaitConnectResult(ctx context.Context, addrStr string, rUDPAddr *net.UDPAddr, packetTCP []byte, resultChan chan connectResult) error {
	connectTimeout := n.Config.DialTimeout
	if connectTimeout <= 0 {
		connectTimeout = 5 * time.Second
	}
	timeout := time.NewTimer(connectTimeout)
	defer timeout.Stop()

	var tcpErr error

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-n.closing:
			return fmt.Errorf("node is closing")
		case res := <-resultChan:
			switch res.Transport {
			case "tcp":
				if res.Conn != nil {
					if err := n.writeTCPPacket(res.Conn, packetTCP); err != nil {
						res.Conn.Close()
						return err
					}
					go n.handleTCP(res.Conn)
					return nil
				}
				if res.Err != nil {
					tcpErr = res.Err
				}
			case "udp":
				n.scheduleRelayFallback(ctx, addrStr, rUDPAddr)
				n.awaitLateTCPUpgrade(ctx, resultChan, packetTCP)
				return nil
			default:
				if res.Conn != nil {
					res.Conn.Close()
				}
				if res.Err != nil {
					tcpErr = res.Err
				}
			}
		case <-timeout.C:
			if n.relayManager != nil {
				if err := n.connectViaRelay(addrStr); err == nil {
					return nil
				}
			}
			if tcpErr != nil {
				return tcpErr
			}
			return fmt.Errorf("connect timeout")
		}
	}
}

// scheduleRelayFallback triggers relay connect if direct mapping does not appear.
func (n *Node) scheduleRelayFallback(ctx context.Context, addrStr string, rUDPAddr *net.UDPAddr) {
	if !n.Config.EnableRelay {
		return
	}
	addrKey := rUDPAddr.String()
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
			n.connectViaRelay(addrStr)
		}
	}()
}

// awaitLateTCPUpgrade waits a short window for a late successful TCP upgrade.
func (n *Node) awaitLateTCPUpgrade(ctx context.Context, resultChan chan connectResult, packetTCP []byte) {
	go func() {
		lateTimeout := time.NewTimer(10 * time.Second)
		defer lateTimeout.Stop()
		for {
			select {
			case <-n.closing:
				return
			case <-ctx.Done():
				return
			case res2 := <-resultChan:
				if res2.Transport == "tcp" && res2.Conn != nil {
					if err := n.writeTCPPacket(res2.Conn, packetTCP); err != nil {
						res2.Conn.Close()
						return
					}
					go n.handleTCP(res2.Conn)
					return
				}
				if res2.Conn != nil {
					res2.Conn.Close()
				}
			case <-lateTimeout.C:
				return
			}
		}
	}()
}
