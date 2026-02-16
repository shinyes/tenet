package node

import (
	"io"
	"net"
	"time"

	"github.com/shinyes/tenet/internal/pool"
)

// acceptTCP accepts inbound TCP sessions until node closes.
func (n *Node) acceptTCP() {
	defer n.wg.Done()
	if n.tcpListener == nil {
		return
	}

	for {
		conn, err := n.tcpListener.Accept()
		if err != nil {
			select {
			case <-n.closing:
				return
			default:
				continue
			}
		}
		go n.handleTCP(conn)
	}
}

// handleTCP reads framed TENT packets from a TCP connection.
func (n *Node) handleTCP(conn net.Conn) {
	defer n.releaseTCPWriteMutex(conn)
	defer conn.Close()

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	header := make([]byte, 2)
	frameBuf := pool.GetLargeBuffer()
	defer pool.PutLargeBuffer(frameBuf)

	for {
		_, err := io.ReadFull(conn, header)
		if err != nil {
			if err != io.EOF {
				n.Config.Logger.Error("read tcp from %s failed: %v", conn.RemoteAddr(), err)
			}
			return
		}

		length := uint16(header[0])<<8 | uint16(header[1])
		if length == 0 {
			return
		}

		var buf []byte
		if int(length) <= len(*frameBuf) {
			buf = (*frameBuf)[:length]
		} else {
			buf = make([]byte, length)
		}

		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return
		}

		if length < 5 || string(buf[0:4]) != "TENT" {
			continue
		}

		packetType := buf[4]
		payload := make([]byte, length-5)
		copy(payload, buf[5:])
		n.handlePacket(conn, conn.RemoteAddr(), "tcp", packetType, payload)
	}
}
