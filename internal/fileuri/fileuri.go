// Package fileuri converts between operating-system paths and RFC 8089 file
// URLs, including Windows drive and UNC paths.
package fileuri

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
)

// FromPath returns the file URL for an absolute path of the running operating
// system. A Windows drive path becomes file:///C:/…, and a UNC path names its
// server as the URL host: file://server/share/….
func FromPath(path string) *url.URL {
	return fromPath(path, runtime.GOOS == "windows")
}

// ToPath returns the operating-system path that a file URL names. It accepts
// the forms FromPath writes, a UNC path written as file:////server/share/…,
// and an opaque path such as file:data.db or file:C:/data.db. The result is
// relative when the URL's path is.
func ToPath(location *url.URL) (string, error) {
	return toPath(location, runtime.GOOS == "windows")
}

func fromPath(path string, windows bool) *url.URL {
	if !windows {
		return &url.URL{Scheme: "file", Path: path}
	}
	slashed := strings.ReplaceAll(path, `\`, "/")
	// A long-path prefix names the same file without Windows' length limit.
	if share, found := strings.CutPrefix(slashed, "//?/UNC/"); found {
		slashed = "//" + share
	} else if local, found := strings.CutPrefix(slashed, "//?/"); found {
		slashed = local
	}
	if unc, found := strings.CutPrefix(slashed, "//"); found {
		if server, rest, found := strings.Cut(unc, "/"); found && server != "" {
			return &url.URL{Scheme: "file", Host: server, Path: "/" + rest}
		}
	}
	if hasDriveLetter(slashed) {
		slashed = "/" + slashed
	}
	return &url.URL{Scheme: "file", Path: slashed}
}

func toPath(location *url.URL, windows bool) (string, error) {
	if location.Scheme != "file" {
		return "", fmt.Errorf("URL scheme %q is not file", location.Scheme)
	}
	path := location.Path
	if location.Opaque != "" {
		var err error
		if path, err = url.PathUnescape(location.Opaque); err != nil {
			return "", fmt.Errorf("decode file URL path: %w", err)
		}
	}
	host := location.Host
	if strings.EqualFold(host, "localhost") {
		host = ""
	}
	if !windows {
		if host != "" {
			return "", fmt.Errorf("file URL names host %q; only local paths are supported", host)
		}
		return path, nil
	}
	if host != "" {
		path = "//" + host + path
	} else if strings.HasPrefix(path, "/") && hasDriveLetter(path[1:]) {
		path = path[1:]
	}
	return strings.ReplaceAll(path, "/", `\`), nil
}

func hasDriveLetter(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	letter := path[0] | 0x20
	return 'a' <= letter && letter <= 'z' && (len(path) == 2 || path[2] == '/')
}
