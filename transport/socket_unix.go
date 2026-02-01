//go:build !windows
// +build !windows

package transport

import "net"

// ListenConfig returns a net.ListenConfig for non-Windows platforms.
func ListenConfig() *net.ListenConfig {
	return &net.ListenConfig{}
}

// DialConfig returns a net.Dialer for non-Windows platforms.
func DialConfig(localAddr *net.TCPAddr) *net.Dialer {
	return &net.Dialer{LocalAddr: localAddr}
}
