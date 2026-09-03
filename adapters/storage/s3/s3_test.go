package s3_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	s3storage "github.com/riducms/ridu/adapters/storage/s3"
	"github.com/riducms/ridu/storage"
)

func TestBackendSignsRequestsAndURLs(t *testing.T) {
	var authorization string
	var method string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		method = request.Method
		authorization = request.Header.Get("Authorization")
		if request.Body != nil {
			_, _ = io.Copy(io.Discard, request.Body)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Status: "204 No Content", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	backend, err := s3storage.New(s3storage.Config{Endpoint: "https://objects.example.test", Region: "test", Bucket: "media", AccessKey: "key", SecretKey: "secret", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), "a.txt", strings.NewReader("a"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(authorization, "AWS4-HMAC-SHA256") {
		t.Fatalf("authorization = %q", authorization)
	}
	if err := backend.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodHead {
		t.Fatalf("health method = %q", method)
	}
	signed, err := backend.SignedURL(context.Background(), "a.txt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(signed, "X-Amz-Signature=") {
		t.Fatalf("signed URL = %q", signed)
	}
}

func TestBackendListsOneBoundedContinuationPageAtATime(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Query().Get("max-keys") != "1" {
			t.Fatalf("max keys = %q", request.URL.Query().Get("max-keys"))
		}
		body := `<ListBucketResult><Contents><Key>media/a.txt</Key><Size>1</Size><LastModified>2026-08-24T00:00:00Z</LastModified></Contents><IsTruncated>true</IsTruncated><NextContinuationToken>next page</NextContinuationToken></ListBucketResult>`
		if requests == 2 {
			if request.URL.Query().Get("continuation-token") != "next page" {
				t.Fatalf("continuation token = %q", request.URL.Query().Get("continuation-token"))
			}
			body = `<ListBucketResult><Contents><Key>media/b.txt</Key><Size>2</Size><LastModified>2026-08-24T00:00:00Z</LastModified></Contents><IsTruncated>false</IsTruncated></ListBucketResult>`
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	backend, err := s3storage.New(s3storage.Config{Endpoint: "https://objects.example.test", Region: "test", Bucket: "media", AccessKey: "key", SecretKey: "secret", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	first, err := backend.List(context.Background(), storage.ListRequest{Prefix: "media/", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(first.Objects) != 1 || first.Objects[0].Key != "media/a.txt" || first.NextCursor != "next page" {
		t.Fatalf("requests=%d first=%#v", requests, first)
	}
	second, err := backend.List(context.Background(), storage.ListRequest{Prefix: "media/", Cursor: first.NextCursor, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(second.Objects) != 1 || second.Objects[0].Key != "media/b.txt" || second.NextCursor != "" {
		t.Fatalf("requests=%d second=%#v", requests, second)
	}
}

func TestBackendRejectsPathShapedObjectKeysBeforeSigningOrSending(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusNoContent, Status: "204 No Content", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	backend, err := s3storage.New(s3storage.Config{Endpoint: "https://objects.example.test", Region: "test", Bucket: "media", AccessKey: "key", SecretKey: "secret", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../victim", "owned/../../victim", "/absolute", "owned//victim", `owned\victim`} {
		if err := backend.Delete(context.Background(), key); err == nil {
			t.Errorf("Delete(%q) succeeded", key)
		}
		if _, err := backend.SignedURL(context.Background(), key, time.Minute); err == nil {
			t.Errorf("SignedURL(%q) succeeded", key)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid keys sent %d S3 requests", requests)
	}
}

func TestBackendRequiresExplicitPlaintextEndpointOptIn(t *testing.T) {
	config := s3storage.Config{Endpoint: "http://127.0.0.1:9000", Region: "test", Bucket: "media", AccessKey: "key", SecretKey: "secret"}
	if _, err := s3storage.New(config); err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("plaintext endpoint error = %v", err)
	}
	config.AllowInsecureEndpoint = true
	if _, err := s3storage.New(config); err != nil {
		t.Fatalf("explicit local endpoint: %v", err)
	}
}

func TestPutRejectsDeclaredSizeMismatchBeforeSending(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return emptyResponse(http.StatusNoContent), nil
	})}
	backend := newTestBackend(t, s3storage.Config{Client: client})
	for _, test := range []struct {
		name     string
		value    string
		declared int64
	}{
		{name: "short", value: "a", declared: 2},
		{name: "long", value: "ab", declared: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := backend.Put(context.Background(), "object.txt", strings.NewReader(test.value), test.declared, "text/plain")
			if err == nil || !strings.Contains(err.Error(), "size mismatch") {
				t.Fatalf("Put error = %v", err)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("mismatched payloads sent %d requests", requests)
	}
}

func TestPutHashesAndCleansABoundedNonSeekableSpool(t *testing.T) {
	spoolDirectory := t.TempDir()
	var body string
	var payloadHash string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		encoded, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = string(encoded)
		payloadHash = request.Header.Get("X-Amz-Content-Sha256")
		return emptyResponse(http.StatusNoContent), nil
	})}
	backend := newTestBackend(t, s3storage.Config{
		Client:         client,
		MaxSpoolBytes:  32,
		SpoolDirectory: spoolDirectory,
	})
	source := struct{ io.Reader }{Reader: strings.NewReader("payload")}
	if err := backend.Put(context.Background(), "object.txt", source, 7, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if body != "payload" {
		t.Fatalf("request body = %q", body)
	}
	digest := sha256.Sum256([]byte("payload"))
	if want := hex.EncodeToString(digest[:]); payloadHash != want {
		t.Fatalf("payload hash = %q, want %q", payloadHash, want)
	}
	assertDirectoryEmpty(t, spoolDirectory)
}

func TestPutRejectsNonSeekableSizeMismatchAndCleansTheSpool(t *testing.T) {
	spoolDirectory := t.TempDir()
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return emptyResponse(http.StatusNoContent), nil
	})}
	backend := newTestBackend(t, s3storage.Config{
		Client:         client,
		MaxSpoolBytes:  32,
		SpoolDirectory: spoolDirectory,
	})
	for _, test := range []struct {
		name     string
		value    string
		declared int64
	}{
		{name: "short", value: "a", declared: 2},
		{name: "long", value: "ab", declared: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := struct{ io.Reader }{Reader: strings.NewReader(test.value)}
			err := backend.Put(context.Background(), "object.txt", source, test.declared, "text/plain")
			if err == nil || !strings.Contains(err.Error(), "size mismatch") {
				t.Fatalf("Put error = %v", err)
			}
			assertDirectoryEmpty(t, spoolDirectory)
		})
	}
	if requests != 0 {
		t.Fatalf("mismatched payloads sent %d requests", requests)
	}
}

func TestPutRejectsOversizedNonSeekableSpoolWithoutReading(t *testing.T) {
	source := &countingReader{reader: strings.NewReader("payload")}
	backend := newTestBackend(t, s3storage.Config{MaxSpoolBytes: 4})
	err := backend.Put(context.Background(), "object.txt", source, 7, "text/plain")
	if err == nil || !strings.Contains(err.Error(), "spool limit") {
		t.Fatalf("Put error = %v", err)
	}
	if source.reads != 0 {
		t.Fatalf("oversized source read %d times", source.reads)
	}
}

func TestPutCancellationUnblocksAClosableSourceAndCleansTheSpool(t *testing.T) {
	spoolDirectory := t.TempDir()
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return emptyResponse(http.StatusNoContent), nil
	})}
	backend := newTestBackend(t, s3storage.Config{
		Client:         client,
		MaxSpoolBytes:  8,
		SpoolDirectory: spoolDirectory,
	})
	source := newBlockingReadCloser()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- backend.Put(ctx, "object.txt", source, 1, "text/plain")
	}()
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("Put did not start reading")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Put error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Put did not stop after cancellation")
	}
	if requests != 0 {
		t.Fatalf("canceled payload sent %d requests", requests)
	}
	assertDirectoryEmpty(t, spoolDirectory)
}

func TestPutRewindsASeekablePayloadAfterHashing(t *testing.T) {
	var body string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		encoded, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = string(encoded)
		return emptyResponse(http.StatusNoContent), nil
	})}
	backend := newTestBackend(t, s3storage.Config{Client: client, MaxSpoolBytes: 1})
	source := strings.NewReader("prefix-payload")
	if _, err := source.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), "object.txt", source, 7, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if body != "payload" {
		t.Fatalf("request body = %q", body)
	}
}

func newTestBackend(t *testing.T, overrides s3storage.Config) *s3storage.Backend {
	t.Helper()
	config := s3storage.Config{
		Endpoint:  "https://objects.example.test",
		Region:    "test",
		Bucket:    "media",
		AccessKey: "key",
		SecretKey: "secret",
		Client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Body != nil {
				_, _ = io.Copy(io.Discard, request.Body)
			}
			return emptyResponse(http.StatusNoContent), nil
		})},
	}
	if overrides.Client != nil {
		config.Client = overrides.Client
	}
	config.MaxSpoolBytes = overrides.MaxSpoolBytes
	config.SpoolDirectory = overrides.SpoolDirectory
	backend, err := s3storage.New(config)
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func emptyResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

type countingReader struct {
	reader io.Reader
	reads  int
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	reader.reads++
	return reader.reader.Read(buffer)
}

type blockingReadCloser struct {
	started chan struct{}
	closed  chan struct{}
	start   sync.Once
	close   sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{started: make(chan struct{}), closed: make(chan struct{})}
}

func (reader *blockingReadCloser) Read([]byte) (int, error) {
	reader.start.Do(func() { close(reader.started) })
	<-reader.closed
	return 0, errors.New("reader closed")
}

func (reader *blockingReadCloser) Close() error {
	reader.close.Do(func() { close(reader.closed) })
	return nil
}

func assertDirectoryEmpty(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary directory contains %#v", entries)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
