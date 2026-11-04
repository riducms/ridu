package publicnet_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"

	"github.com/riducms/ridu/publicnet"
)

func TestIsPublicRejectsIANASpecialPurposeRanges(t *testing.T) {
	for _, address := range []string{
		"0.0.0.1", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.0.1", "172.16.0.1",
		"192.0.0.1", "192.0.2.1", "192.31.196.1", "192.52.193.1", "192.88.99.1",
		"192.168.0.1", "192.175.48.1", "198.18.0.1", "198.51.100.1", "203.0.113.1",
		"224.0.0.1", "240.0.0.1", "::", "::1", "::ffff:192.0.2.1", "64:ff9b::1",
		"64:ff9b:1::1", "100::1", "100:0:0:1::1", "2001::1", "2001:db8::1", "2002::1",
		"2620:4f:8000::1", "3fff::1", "5f00::1", "fc00::1", "fe80::1", "ff00::1",
		// IPv6 outside the 2000::/3 global unicast block, including
		// IPv4-compatible forms of loopback and link-local metadata.
		"::7f00:1", "::a9fe:a9fe", "::1:0:0:1", "4000::1", "e000::1",
	} {
		if publicnet.IsPublic(netip.MustParseAddr(address)) {
			t.Errorf("special-purpose address %s was accepted", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "::ffff:8.8.8.8", "2606:4700:4700::1111"} {
		if !publicnet.IsPublic(netip.MustParseAddr(address)) {
			t.Errorf("public address %s was rejected", address)
		}
	}
}

func TestCheckURLRefusesPrivateHostsCredentialsAndOtherSchemes(t *testing.T) {
	for rawURL, private := range map[string]bool{
		"http://127.0.0.1/file":                       true,
		"http://169.254.169.254/latest/meta-data":     true,
		"http://100.100.100.200/latest/meta-data":     true,
		"http://[::127.0.0.1]/internal":               true,
		"http://[::169.254.169.254]/latest/meta-data": true,
		"http://localhost/file":                       true,
		"file:///tmp/file":                            false,
		"https://user:pass@93.184.216.34/file":        false,
		"https:///no-host":                            false,
	} {
		_, err := publicnet.CheckURL(context.Background(), rawURL)
		if err == nil || errors.Is(err, publicnet.ErrNotPublic) != private {
			t.Errorf("CheckURL(%q) = %v", rawURL, err)
		}
	}
	if parsed, err := publicnet.CheckURL(context.Background(), " https://93.184.216.34/a?b=c "); err != nil || parsed.Host != "93.184.216.34" {
		t.Fatalf("public URL = %v, %v", parsed, err)
	}
}

func TestClientRefusesToDialLocalServers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	// The request skips CheckURL, so only the dialer stands between it and
	// the loopback server.
	_, err := publicnet.NewClient(publicnet.Options{}).Get(server.URL)
	if !errors.Is(err, publicnet.ErrNotPublic) {
		t.Fatalf("loopback request error = %v", err)
	}
}

func TestClientChecksEveryRedirect(t *testing.T) {
	client := publicnet.NewClient(publicnet.Options{MaxRedirects: 2})
	public := &http.Request{URL: &url.URL{Scheme: "https", Host: "93.184.216.34"}}
	metadata := &http.Request{URL: &url.URL{Scheme: "http", Host: "100.100.100.200", Path: "/latest/meta-data"}}
	if err := client.CheckRedirect(metadata, []*http.Request{public}); !errors.Is(err, publicnet.ErrNotPublic) {
		t.Fatalf("redirect to a metadata address = %v", err)
	}
	if err := client.CheckRedirect(public, []*http.Request{public}); err != nil {
		t.Fatalf("redirect to a public address = %v", err)
	}
	if err := client.CheckRedirect(public, []*http.Request{public, public}); err == nil {
		t.Fatal("a third redirect was followed")
	}
	none := publicnet.NewClient(publicnet.Options{MaxRedirects: -1})
	if err := none.CheckRedirect(public, []*http.Request{public}); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirects off = %v", err)
	}
}
