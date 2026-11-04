package remotefile

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/riducms/ridu/publicnet"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
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
		"http://[::127.0.0.1]/internal",
		"http://[::169.254.169.254]/latest/meta-data",
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

func TestFetchNamesTheUploadInAddressErrors(t *testing.T) {
	_, err := fetchWithClient(context.Background(), http.DefaultClient, "http://127.0.0.1/file", 10)
	if !errors.Is(err, publicnet.ErrNotPublic) || err.Error() != "remote upload URL resolves to a non-public address" {
		t.Fatalf("private address error = %v", err)
	}
}
