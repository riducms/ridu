// Package localnet recognizes database endpoints that never leave the host.
package localnet

import (
	"net"
	"strings"
)

// Host reports whether host names this machine: "localhost", a loopback IP
// address, or a Unix socket path. Nothing off the host can observe traffic to
// it, so transport encryption there protects nothing and adapters admit
// plaintext without an explicit override.
func Host(host string) bool {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "/") {
		return true
	}
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	address := net.ParseIP(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
	return address != nil && address.IsLoopback()
}

// HostPort is Host for a "host:port" address; an address without a port is
// read as a bare host.
func HostPort(address string) bool {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return Host(host)
	}
	return Host(address)
}
