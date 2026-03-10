package node

import (
	"fmt"
	"net"
	"sync"

	"github.com/shinyes/tenet/internal/protocol"
)

// sendRaw writes packet over tcp or udp transport.
func (n *Node) sendRaw(conn net.Conn, addr net.Addr, transport string, packet []byte) error {
	if transport == "tcp" && conn != nil {
		if err := n.writeTCPPacket(conn, packet); err != nil {
			return fmt.Errorf("TCP write error: %w", err)
		}
		return nil
	}
	if udpAddr, ok := addr.(*net.UDPAddr); ok {
		if _, err := n.tentConn.WriteTo(packet, udpAddr); err != nil {
			return fmt.Errorf("UDP write error: %w", err)
		}
		return nil
	}
	return fmt.Errorf("unsupported transport or invalid address")
}

// writeTCPPacket encodes and writes one framed TCP packet.
func (n *Node) writeTCPPacket(conn net.Conn, packet []byte) error {
	frame := protocol.EncodeTCPFrame(packet)
	mu := n.getTCPWriteMutex(conn)
	mu.Lock()
	defer mu.Unlock()
	_, err := conn.Write(frame)
	return err
}

// getTCPWriteMutex returns per-connection write lock.
func (n *Node) getTCPWriteMutex(conn net.Conn) *sync.Mutex {
	if mu, ok := n.tcpWriteMuMap.Load(conn); ok {
		return mu.(*sync.Mutex)
	}
	newMu := &sync.Mutex{}
	actual, _ := n.tcpWriteMuMap.LoadOrStore(conn, newMu)
	return actual.(*sync.Mutex)
}

// releaseTCPWriteMutex releases per-connection write lock mapping.
func (n *Node) releaseTCPWriteMutex(conn net.Conn) {
	n.tcpWriteMuMap.Delete(conn)
}
