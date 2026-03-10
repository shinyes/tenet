package node

import (
	"io"
	"net"
	"time"

	"github.com/shinyes/tenet/internal/pool"
)

// handleRead processes inbound UDP packets from the mux tent connection.
func (n *Node) handleRead() {
	defer n.wg.Done()

	bufPtr := pool.GetLargeBuffer()
	defer pool.PutLargeBuffer(bufPtr)
	buf := *bufPtr

	for {
		select {
		case <-n.closing:
			return
		default:
		}

		count, addr, err := n.tentConn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if err == io.EOF {
				return
			}
			select {
			case <-n.closing:
				return
			default:
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}

		udpAddr, ok := addr.(*net.UDPAddr)
		if !ok {
			continue
		}
		if count < 4 {
			continue
		}

		if string(buf[0:4]) == "NATP" && n.natProber != nil {
			resp := n.natProber.HandleProbePacket(buf[:count], udpAddr)
			if len(resp) > 0 {
				_, _ = n.tentConn.WriteTo(resp, udpAddr)
			}
			continue
		}

		if count < 5 {
			continue
		}
		if string(buf[0:4]) != "TENT" {
			continue
		}

		packetType := buf[4]
		payload := make([]byte, count-5)
		copy(payload, buf[5:count])

		if packetType == PacketTypeRelay {
			n.handleRelayPacket(udpAddr, payload)
			continue
		}

		if n.Config.EnableRelay {
			n.mu.RLock()
			originAddr, forward := n.relayForward[addr.String()]
			n.mu.RUnlock()
			if forward && originAddr != nil {
				n.tentConn.WriteTo(buf[:count], originAddr)
				continue
			}
		}

		n.handlePacket(nil, addr, "udp", packetType, payload)
	}
}
