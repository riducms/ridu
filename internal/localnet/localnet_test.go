package localnet

import "testing"

func TestHostAdmitsOnlyEndpointsOnThisMachine(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost":                     true,
		"LOCALHOST.":                    true,
		"127.0.0.1":                     true,
		"127.8.0.1":                     true,
		"::1":                           true,
		"[::1]":                         true,
		"/var/run/postgresql":           true,
		"/tmp/mongodb-27017.sock":       true,
		"":                              false,
		"db.sock":                       false,
		"db.example":                    false,
		"localhost.example":             false,
		"app.localhost":                 false,
		"10.0.0.5":                      false,
		"0.0.0.0":                       false,
		"::ffff:10.0.0.5":               false,
		"postgres.internal.example.com": false,
	} {
		if got := Host(host); got != want {
			t.Errorf("Host(%q) = %t, want %t", host, got, want)
		}
	}
	for address, want := range map[string]bool{
		"localhost:5432":   true,
		"[::1]:27017":      true,
		"127.0.0.1":        true,
		"db.example:27017": false,
	} {
		if got := HostPort(address); got != want {
			t.Errorf("HostPort(%q) = %t, want %t", address, got, want)
		}
	}
}
