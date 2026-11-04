// Package remotefile downloads user-supplied asset URLs through publicnet, so
// the server never becomes a proxy into loopback, private, link-local, or
// metadata networks.
package remotefile

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/riducms/ridu/publicnet"
)

type File struct {
	Filename string
	Bytes    []byte
}

func Fetch(ctx context.Context, rawURL string, maximumBytes int64) (File, error) {
	return fetchWithClient(ctx, publicnet.NewClient(publicnet.Options{}), rawURL, maximumBytes)
}

func fetchWithClient(ctx context.Context, client *http.Client, rawURL string, maximumBytes int64) (File, error) {
	parsed, err := publicnet.CheckURL(ctx, rawURL)
	if err != nil {
		return File{}, fmt.Errorf("remote upload %w", err)
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
