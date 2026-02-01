//go:build windows
// +build windows

package transport

import (
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

// ListenConfig returns a net.ListenConfig with SO_REUSEADDR enabled
func ListenConfig() *net.ListenConfig {
	return &net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				// Windows: SO_REUSEADDR ensures we can bind multiple sockets to the same port
				// This is required for TCP Simultaneous Open (Listen + Dial from same port)
				windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
			})
		},
	}
}

// DialConfig returns a net.Dialer with SO_REUSEADDR enabled for the local address
func DialConfig(localAddr *net.TCPAddr) *net.Dialer {
	return &net.Dialer{
		LocalAddr: localAddr,
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
			})
		},
	}
}
