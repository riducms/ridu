package remotefile

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestPublicAddressPolicyRejectsIANASpecialPurposeRanges(t *testing.T) {
	for _, address := range []string{
		"0.0.0.1", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.0.1", "172.16.0.1",
		"192.0.0.1", "192.0.2.1", "192.31.196.1", "192.52.193.1", "192.88.99.1",
		"192.168.0.1", "192.175.48.1", "198.18.0.1", "198.51.100.1", "203.0.113.1",
		"224.0.0.1", "240.0.0.1", "::", "::1", "::ffff:192.0.2.1", "64:ff9b::1",
		"64:ff9b:1::1", "100::1", "100:0:0:1::1", "2001::1", "2001:db8::1", "2002::1",
		"2620:4f:8000::1", "3fff::1", "5f00::1", "fc00::1", "fe80::1", "ff00::1",
	} {
		if isPublic(netip.MustParseAddr(address)) {
			t.Errorf("special-purpose address %s was accepted", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !isPublic(netip.MustParseAddr(address)) {
			t.Errorf("public address %s was rejected", address)
		}
	}
}

func TestFetchAcceptsBoundedPublicHTTPResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Disposition": {`attachment; filename="cover.png"`}}, Body: io.NopCloser(strings.NewReader("image")), Request: request}, nil
	})}
	file, err := fetchWithClient(context.Background(), client, "https://93.184.216.34/original.png", 10)
	if err != nil {
		t.Fatal(err)
	}
	if file.Filename != "cover.png" || string(file.Bytes) != "image" {
		t.Fatalf("file = %#v", file)
	}
}

func TestFetchRejectsPrivateCredentialsSchemesAndOversize(t *testing.T) {
	for _, rawURL := range []string{
		"http://127.0.0.1/file",
		"http://169.254.169.254/latest/meta-data",
		"http://100.100.100.200/latest/meta-data",
		"http://198.18.0.1/internal",
		"http://[2001:db8::1]/internal",
		"file:///tmp/file",
		"https://user:pass@93.184.216.34/file",
	} {
		if _, err := fetchWithClient(context.Background(), http.DefaultClient, rawURL, 10); err == nil {
			t.Fatalf("Fetch(%q) succeeded", rawURL)
		}
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: 11, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("too large!!")), Request: request}, nil
	})}
	if _, err := fetchWithClient(context.Background(), client, "https://93.184.216.34/file", 10); err == nil {
		t.Fatal("oversized response succeeded")
	}
}

func TestSafeClientRejectsRedirectsToSpecialUseNetworks(t *testing.T) {
	client := safeClient()
	request := &http.Request{URL: &url.URL{Scheme: "http", Host: "100.100.100.200", Path: "/latest/meta-data"}}
	if err := client.CheckRedirect(request, []*http.Request{{URL: &url.URL{Scheme: "https", Host: "93.184.216.34"}}}); err == nil {
		t.Fatal("redirect to carrier-grade NAT metadata address succeeded")
	}
}
