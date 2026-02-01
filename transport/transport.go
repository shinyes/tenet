package transport

import (
	"context"
	"net"
)

// Transport defines the interface for flexible network transportation
type Transport interface {
	// Listen announces on the local network address.
	Listen(ctx context.Context, port int) (Listener, error)
	// Dial connects to the address on the named network.
	Dial(ctx context.Context, addr string) (Conn, error)
	// Protocol returns the protocol name (e.g. "tcp", "udp")
	Protocol() string
}

// Listener is a generic network listener
type Listener interface {
	// Accept waits for and returns the next connection to the listener.
	Accept() (Conn, error)
	// Close closes the listener.
	Close() error
	// Addr returns the listener's network address.
	Addr() net.Addr
}

// Conn is a generic network connection
type Conn interface {
	net.Conn
}
