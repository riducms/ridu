package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCLIArchiveHoldsOnlyThePlainBinary(t *testing.T) {
	archive, err := tarArchive("ridu", []byte("binary"))
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(compressed)
	header, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	// The launcher takes the first entry named ridu, so nothing may come before it.
	if header.Name != "ridu" || header.Mode != 0o755 || header.Typeflag != tar.TypeReg {
		t.Fatalf("first entry = %+v", header)
	}
	content, _ := io.ReadAll(reader)
	if string(content) != "binary" {
		t.Fatalf("binary = %q", content)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("archive has more than the binary: %v", err)
	}
}

func TestNextLocalVersionIsAUniquePrereleaseOfTheNextPatch(t *testing.T) {
	got := nextLocalVersion("0.16.2", time.Date(2026, 10, 8, 1, 2, 3, 0, time.UTC))
	if got != "0.16.3-local.20261008010203" {
		t.Fatalf("version = %q", got)
	}
}

func TestRegistryServesStagedPackagesAndProxiesTheRest(t *testing.T) {
	out := t.TempDir()
	npmRoot := filepath.Join(out, "npm")
	if err := os.MkdirAll(npmRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	tarball := packedTarball(t, `{"name":"@riducms/ui","version":"0.16.3-local.1","svelte":"./dist/index.js"}`)
	if err := os.WriteFile(filepath.Join(npmRoot, "ridu-framework-ui.tgz"), tarball, 0o644); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte("upstream " + request.URL.Path))
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)
	handler, err := newRegistry(out, upstreamURL)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	var document struct {
		DistTags map[string]string `json:"dist-tags"`
		Versions map[string]struct {
			Svelte string `json:"svelte"`
			Dist   struct {
				Tarball   string `json:"tarball"`
				Integrity string `json:"integrity"`
			} `json:"dist"`
		} `json:"versions"`
	}
	// npm escapes the scope separator; Bun doesn't.
	for _, path := range []string{"/@riducms%2fui", "/@riducms/ui"} {
		decodeJSON(t, server.URL+path, &document)
		release := document.Versions["0.16.3-local.1"]
		if document.DistTags["latest"] != "0.16.3-local.1" || release.Svelte != "./dist/index.js" || release.Dist.Integrity == "" {
			t.Fatalf("%s packument = %+v", path, document)
		}
	}
	if body := get(t, document.Versions["0.16.3-local.1"].Dist.Tarball); !bytes.Equal(body, tarball) {
		t.Fatal("tarball differs from the staged file")
	}
	if body := get(t, server.URL+"/left-pad"); string(body) != "upstream /left-pad" {
		t.Fatalf("proxied package = %q", body)
	}
}

func packedTarball(t *testing.T, manifest string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	if err := archive.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o644, Size: int64(len(manifest))}); err != nil {
		t.Fatal(err)
	}
	archive.Write([]byte(manifest))
	archive.Close()
	compressed.Close()
	return buffer.Bytes()
}

func decodeJSON(t *testing.T, address string, target any) {
	t.Helper()
	if err := json.Unmarshal(get(t, address), target); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, address string) []byte {
	t.Helper()
	response, err := http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
