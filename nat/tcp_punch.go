package nat

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/cykyes/tenet/transport"
)

// TCPHolePuncher implements TCP Simultaneous Open hole punching
type TCPHolePuncher struct {
}

// NewTCPHolePuncher creates a new TCP hole puncher
func NewTCPHolePuncher() *TCPHolePuncher {
	return &TCPHolePuncher{}
}

// Punch attempts to establish a TCP connection with a peer using Simultaneous Open
// It requires the local port that is already being used (or will be used) for listening.
func (tp *TCPHolePuncher) Punch(ctx context.Context, localPort int, peerAddr *net.TCPAddr) (*net.TCPConn, error) {
	if peerAddr == nil {
		return nil, fmt.Errorf("peerAddr is nil")
	}
	// 1. Prepare Local Address
	// We must bind to the specific port
	localAddr := &net.TCPAddr{
		Port: localPort,
	}

	resultChan := make(chan *net.TCPConn, 2)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Strategy:
	// EasyTier/Standard TCP Hole Punching:
	// 1. Listen on LocalPort (with SO_REUSEADDR)
	// 2. Dial PeerAddr from LocalPort (with SO_REUSEADDR)
	// Whichever succeeds first wins.

	// Goroutine 1: Dial
	go func() {
		// Retry loop for dialing
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			dialer := transport.DialConfig(localAddr)
			conn, err := dialer.DialContext(ctx, "tcp", peerAddr.String())
			if err == nil {
				// Success!
				tcpConn, ok := conn.(*net.TCPConn)
				if !ok {
					conn.Close()
					continue
				}
				select {
				case resultChan <- tcpConn:
				case <-ctx.Done():
					conn.Close()
				}
				return
			}

			// Wait a bit before retry, but not too long as timing is key
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return
			}
		}
	}()

	// Goroutine 2: Listen (Accept)
	go func() {
		lc := transport.ListenConfig()
		listener, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", localPort))
		if err != nil {
			// If we fail to listen, we can't accept incoming.
			// But maybe the dialer will succeed.
			return
		}
		defer listener.Close()

		// Accept loop with deadline to allow context check
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Set short deadline to periodically check context
			listener.(*net.TCPListener).SetDeadline(time.Now().Add(200 * time.Millisecond))
			conn, err := listener.Accept()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout, check context and retry
				}
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}

			// Check if it's the expected peer (optional but good for security)
			// For simplified hole punching we usually accept and verify handshake later.
			tcpConn, ok := conn.(*net.TCPConn)
			if !ok {
				conn.Close()
				continue
			}

			select {
			case resultChan <- tcpConn:
			case <-ctx.Done():
				conn.Close()
			}
			return
		}
	}()

	// Wait for first success
	select {
	case conn := <-resultChan:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Second): // Global timeout
		return nil, fmt.Errorf("tcp hole punch timeout")
	}
}
