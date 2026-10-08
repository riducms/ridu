package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultAddress = "127.0.0.1:4873"
	npmRegistry    = "https://registry.npmjs.org"
)

func runServe(arguments []string) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	out := flags.String("out", defaultOut, "directory holding the staged release")
	address := flags.String("address", defaultAddress, "listen address; env.sh expects the default")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	upstream, _ := url.Parse(npmRegistry)
	handler, err := newRegistry(*out, upstream)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load local release: %v\n", err)
		return 1
	}
	fmt.Printf("Serving %d local Ridu packages and CLI releases on http://%s; other packages come from %s\n", len(handler.packages), *address, npmRegistry)
	if err := http.ListenAndServe(*address, handler); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// registry serves the staged npm tarballs and CLI archives, and proxies every other package to
// the public registry so a project's third-party dependencies install normally.
type registry struct {
	npmRoot  string
	packages map[string]localPackage
	releases http.Handler
	upstream http.Handler
}

type localPackage struct {
	manifest  map[string]any
	file      string
	integrity string
	shasum    string
}

func newRegistry(out string, upstream *url.URL) (*registry, error) {
	npmRoot := filepath.Join(out, "npm")
	entries, err := os.ReadDir(npmRoot)
	if err != nil {
		return nil, fmt.Errorf("%w; run the stage command first", err)
	}
	packages := map[string]localPackage{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".tgz") {
			continue
		}
		tarball, err := os.ReadFile(filepath.Join(npmRoot, entry.Name()))
		if err != nil {
			return nil, err
		}
		manifest, err := packedManifest(tarball)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		name, _ := manifest["name"].(string)
		sha512Sum := sha512.Sum512(tarball)
		sha1Sum := sha1.Sum(tarball)
		packages[name] = localPackage{
			manifest:  manifest,
			file:      entry.Name(),
			integrity: "sha512-" + base64.StdEncoding.EncodeToString(sha512Sum[:]),
			shasum:    hex.EncodeToString(sha1Sum[:]),
		}
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	direct := proxy.Director
	proxy.Director = func(request *http.Request) {
		direct(request)
		request.Host = upstream.Host
	}
	return &registry{
		npmRoot:  npmRoot,
		packages: packages,
		releases: http.StripPrefix("/releases/", http.FileServer(http.Dir(filepath.Join(out, "releases")))),
		upstream: proxy,
	}, nil
}

func packedManifest(tarball []byte) (map[string]any, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return nil, err
	}
	archive := tar.NewReader(compressed)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("tarball has no package/package.json")
		}
		if err != nil {
			return nil, err
		}
		if header.Name == "package/package.json" {
			var manifest map[string]any
			return manifest, json.NewDecoder(archive).Decode(&manifest)
		}
	}
}

func (registry *registry) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	path, err := url.PathUnescape(request.URL.EscapedPath())
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	switch {
	case path == "/":
		names := make([]string, 0, len(registry.packages))
		for name, entry := range registry.packages {
			names = append(names, fmt.Sprintf("%s@%v", name, entry.manifest["version"]))
		}
		sort.Strings(names)
		fmt.Fprintf(response, "Local Ridu release registry\n\n%s\n", strings.Join(names, "\n"))
	case strings.HasPrefix(path, "/-/tarballs/"):
		name := strings.TrimPrefix(path, "/-/tarballs/")
		if name != filepath.Base(name) {
			http.NotFound(response, request)
			return
		}
		http.ServeFile(response, request, filepath.Join(registry.npmRoot, name))
	case strings.HasPrefix(path, "/releases/"):
		registry.releases.ServeHTTP(response, request)
	default:
		entry, local := registry.packages[strings.TrimPrefix(path, "/")]
		if !local || request.Method != http.MethodGet {
			registry.upstream.ServeHTTP(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		json.NewEncoder(response).Encode(entry.packument("http://" + request.Host))
	}
}

func (entry localPackage) packument(origin string) map[string]any {
	name, _ := entry.manifest["name"].(string)
	version, _ := entry.manifest["version"].(string)
	published := map[string]any{}
	for key, value := range entry.manifest {
		published[key] = value
	}
	published["_id"] = name + "@" + version
	published["dist"] = map[string]any{
		"tarball":   origin + "/-/tarballs/" + entry.file,
		"integrity": entry.integrity,
		"shasum":    entry.shasum,
	}
	return map[string]any{
		"name":      name,
		"dist-tags": map[string]string{"latest": version},
		"versions":  map[string]any{version: published},
	}
}
