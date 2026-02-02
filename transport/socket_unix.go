//go:build !windows
// +build !windows

package transport

import (
	"net"
	"syscall"
)

// ListenConfig returns a net.ListenConfig with SO_REUSEADDR and SO_REUSEPORT enabled.
// This is required for TCP Simultaneous Open (Listen + Dial from same port) on Unix/Linux/Android.
func ListenConfig() *net.ListenConfig {
	return &net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			err := c.Control(func(fd uintptr) {
				// SO_REUSEADDR allows binding to an address that is already in use
				if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
					opErr = err
					return
				}
				// SO_REUSEPORT allows multiple sockets to bind to the same port
				// This is essential for TCP hole punching on Linux/Android
				if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1); err != nil {
					opErr = err
					return
				}
			})
			if err != nil {
				return err
			}
			return opErr
		},
	}
}

// DialConfig returns a net.Dialer with SO_REUSEADDR and SO_REUSEPORT enabled for the local address.
// This allows dialing from a specific local port that may already be in use by a listener.
func DialConfig(localAddr *net.TCPAddr) *net.Dialer {
	return &net.Dialer{
		LocalAddr: localAddr,
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			err := c.Control(func(fd uintptr) {
				if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
					opErr = err
					return
				}
				if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1); err != nil {
					opErr = err
					return
				}
			})
			if err != nil {
				return err
			}
			return opErr
		},
	}
}
