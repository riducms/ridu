// Package publicnet makes outbound HTTP requests that can reach only the
// public internet. Use it to fetch a URL that a person or another system
// supplied, such as a page to preview or a file to import, so the server
// can't be made to reach its own loopback, private, link-local or cloud
// metadata networks. Ridu fetches uploadFromURL sources through this client.
package publicnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// ErrNotPublic reports a URL, redirect or connection refused because its host
// resolves to an address outside the public internet.
var ErrNotPublic = errors.New("URL resolves to a non-public address")

const (
	defaultTimeout      = 15 * time.Second
	defaultMaxRedirects = 5
)

// Options tunes a client. The zero value is a client suitable for fetching
// one page or file.
type Options struct {
	// Timeout bounds each request, including its redirects and reading the
	// body. Zero means 15 seconds.
	Timeout time.Duration
	// MaxRedirects is how many redirects a request may follow. Zero means 5;
	// a negative value follows none and returns the redirect response.
	MaxRedirects int
}

// NewClient returns an HTTP client whose every connection goes to a public
// address. The host is resolved when the connection is dialed and the
// connection goes to an address that was checked, so neither a name that
// resolves to a private address nor a redirect to one gets through. Each
// redirect is checked like the first URL. The client ignores proxy settings
// from the environment and doesn't ask for compressed responses, so a size
// limit applies to the bytes the server sends.
//
// Requests must still bound what they read from a response body.
func NewClient(options Options) *http.Client {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxRedirects := options.MaxRedirects
	if maxRedirects == 0 {
		maxRedirects = defaultMaxRedirects
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := publicAddresses(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		DisableCompression:    true,
	}
	client := &http.Client{Transport: transport, Timeout: timeout}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if maxRedirects < 0 {
			return http.ErrUseLastResponse
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		_, err := CheckURL(request.Context(), request.URL.String())
		return err
	}
	return client
}

// CheckURL parses rawURL and reports whether a client from NewClient may
// request it: it must be an http or https URL without credentials whose host
// resolves only to public addresses. Checking first gives a person a clear
// error before any request is sent; the client checks again when it dials.
func CheckURL(ctx context.Context, rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("URL must use HTTP or HTTPS")
	}
	if parsed.User != nil {
		return nil, errors.New("URL must not contain credentials")
	}
	if _, err := publicAddresses(ctx, parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

// IsPublic reports whether address is a public internet address: a global
// unicast address outside every IANA special-purpose range, such as private,
// shared, documentation, benchmarking and translation networks. IPv6 must be
// in 2000::/3, and an IPv4-mapped address is judged as its IPv4 address.
func IsPublic(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() {
		return false
	}
	if address.Is6() && !globalUnicastIPv6.Contains(address) {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func publicAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(host); err == nil {
		if !IsPublic(address) {
			return nil, ErrNotPublic
		}
		return []netip.Addr{address.Unmap()}, nil
	}
	resolved, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(resolved) == 0 {
		return nil, errors.New("URL host could not be resolved")
	}
	addresses := make([]netip.Addr, 0, len(resolved))
	for _, address := range resolved {
		if !IsPublic(address) {
			return nil, ErrNotPublic
		}
		addresses = append(addresses, address.Unmap())
	}
	return addresses, nil
}

// globalUnicastIPv6 is the only IANA-allocated IPv6 global unicast block.
// IPv6 outside it, such as IPv4-compatible ::/96, is never public.
var globalUnicastIPv6 = netip.MustParsePrefix("2000::/3")

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}
