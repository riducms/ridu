// Package remotefile downloads user-supplied asset URLs without allowing the
// server to become a proxy into loopback, private, link-local, or metadata networks.
package remotefile

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"
)

const maximumRedirects = 5

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

type File struct {
	Filename string
	Bytes    []byte
}

func Fetch(ctx context.Context, rawURL string, maximumBytes int64) (File, error) {
	client := safeClient()
	return fetchWithClient(ctx, client, rawURL, maximumBytes)
}

func fetchWithClient(ctx context.Context, client *http.Client, rawURL string, maximumBytes int64) (File, error) {
	parsed, err := validateURL(ctx, rawURL)
	if err != nil {
		return File{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return File{}, fmt.Errorf("prepare remote upload request: %w", err)
	}
	request.Header.Set("Accept", "*/*")
	request.Header.Set("User-Agent", "Ridu-Remote-Upload/1")
	response, err := client.Do(request)
	if err != nil {
		return File{}, fmt.Errorf("download remote upload: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return File{}, fmt.Errorf("remote server returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maximumBytes {
		return File{}, fmt.Errorf("remote file exceeds %d-byte upload limit", maximumBytes)
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maximumBytes+1))
	if err != nil {
		return File{}, fmt.Errorf("read remote upload: %w", err)
	}
	if int64(len(encoded)) > maximumBytes {
		return File{}, fmt.Errorf("remote file exceeds %d-byte upload limit", maximumBytes)
	}
	return File{Filename: responseFilename(response, parsed), Bytes: encoded}, nil
}

func safeClient() *http.Client {
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
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= maximumRedirects {
			return fmt.Errorf("remote upload followed too many redirects")
		}
		_, err := validateURL(request.Context(), request.URL.String())
		return err
	}
	return client
}

func validateURL(ctx context.Context, rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("remote upload URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("remote upload URL must use HTTP or HTTPS")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("remote upload URL must not contain credentials")
	}
	if _, err := publicAddresses(ctx, parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

func publicAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(host); err == nil {
		if !isPublic(address.Unmap()) {
			return nil, fmt.Errorf("remote upload URL resolves to a non-public address")
		}
		return []netip.Addr{address}, nil
	}
	resolved, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(resolved) == 0 {
		return nil, fmt.Errorf("resolve remote upload host")
	}
	addresses := make([]netip.Addr, 0, len(resolved))
	for _, address := range resolved {
		address = address.Unmap()
		if !isPublic(address) {
			return nil, fmt.Errorf("remote upload URL resolves to a non-public address")
		}
		addresses = append(addresses, address)
	}
	return addresses, nil
}

func isPublic(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func responseFilename(response *http.Response, source *url.URL) string {
	if disposition := response.Header.Get("Content-Disposition"); disposition != "" {
		if _, parameters, err := mime.ParseMediaType(disposition); err == nil && strings.TrimSpace(parameters["filename"]) != "" {
			return parameters["filename"]
		}
	}
	filename := path.Base(source.EscapedPath())
	if decoded, err := url.PathUnescape(filename); err == nil {
		filename = decoded
	}
	if filename == "" || filename == "." || filename == "/" {
		return "remote-upload"
	}
	return filename
}
