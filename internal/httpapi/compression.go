package httpapi

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// compressionMinimumBytes leaves small bodies uncompressed, where gzip's own
// header and trailer outweigh what it saves.
const compressionMinimumBytes = 1024

var gzipWriters = sync.Pool{New: func() any {
	writer, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return writer
}}

// compressResponses gzips GET responses of text, JSON, JavaScript and SVG for
// clients that accept gzip. Auth responses are never compressed: a compressed
// body that holds a secret beside text an attacker chose can reveal the secret
// through its length (BREACH). Ridu returns session and preview tokens only
// from POST responses, so GET responses carry none, and /api/auth/ is left out
// as well.
func compressResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.Header.Get("Range") != "" || strings.HasPrefix(request.URL.Path, "/api/auth/") {
			next.ServeHTTP(writer, request)
			return
		}
		compressing := &compressingWriter{ResponseWriter: writer, accepted: acceptsGzip(request.Header.Get("Accept-Encoding"))}
		defer compressing.close()
		next.ServeHTTP(compressing, request)
	})
}

// acceptsGzip reports whether an Accept-Encoding value allows gzip, explicitly
// or through "*", without refusing it with q=0.
func acceptsGzip(header string) bool {
	accepted, wildcard := false, false
	for _, entry := range strings.Split(header, ",") {
		name, parameters, _ := strings.Cut(strings.TrimSpace(entry), ";")
		allowed := true
		for _, parameter := range strings.Split(parameters, ";") {
			key, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "q") {
				quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				allowed = err == nil && quality > 0
			}
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "gzip", "x-gzip":
			if !allowed {
				return false
			}
			accepted = true
		case "*":
			wildcard = allowed
		}
	}
	return accepted || wildcard
}

func compressibleType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch {
	case mediaType == "text/event-stream":
		return false
	case strings.HasPrefix(mediaType, "text/"), mediaType == "application/json", strings.HasSuffix(mediaType, "+json"),
		mediaType == "application/javascript", mediaType == "application/xml", strings.HasSuffix(mediaType, "+xml"):
		return true
	default:
		return false
	}
}

// compressingWriter holds the first compressionMinimumBytes of a body, then
// decides from the final status and headers whether to gzip it.
type compressingWriter struct {
	http.ResponseWriter
	accepted bool
	status   int
	buffer   []byte
	decided  bool
	gzip     *gzip.Writer
}

func (writer *compressingWriter) WriteHeader(status int) {
	if writer.decided || status < http.StatusOK {
		writer.ResponseWriter.WriteHeader(status)
		return
	}
	if writer.status == 0 {
		writer.status = status
	}
}

func (writer *compressingWriter) Write(body []byte) (int, error) {
	if !writer.decided {
		if len(writer.buffer)+len(body) < compressionMinimumBytes {
			writer.buffer = append(writer.buffer, body...)
			return len(body), nil
		}
		if err := writer.decide(body); err != nil {
			return 0, err
		}
		return len(body), nil
	}
	if writer.gzip != nil {
		return writer.gzip.Write(body)
	}
	return writer.ResponseWriter.Write(body)
}

// decide sends the status and headers, then the held body and next, compressed
// when the response qualifies. A write that crosses the threshold arrives as
// next, so a large body isn't copied into the buffer.
func (writer *compressingWriter) decide(next []byte) error {
	writer.decided = true
	held := writer.buffer
	writer.buffer = nil
	size := len(held) + len(next)
	status := writer.status
	if status == 0 {
		status = http.StatusOK
	}
	header := writer.Header()
	// net/http sniffs a missing Content-Type from the first 512 bytes written;
	// do the same here, since this writer sends the header itself.
	if header.Get("Content-Type") == "" && size != 0 && header.Get("Content-Encoding") == "" {
		sample := held
		if len(sample) < 512 {
			sample = append(sample[:len(sample):len(sample)], next[:min(len(next), 512-len(sample))]...)
		}
		header.Set("Content-Type", http.DetectContentType(sample))
	}
	eligible := status != http.StatusNoContent && status != http.StatusNotModified && status != http.StatusPartialContent &&
		header.Get("Content-Encoding") == "" && header.Get("Content-Range") == "" && compressibleType(header.Get("Content-Type"))
	if eligible {
		header.Add("Vary", "Accept-Encoding")
	}
	destination := io.Writer(writer.ResponseWriter)
	if eligible && writer.accepted && size >= compressionMinimumBytes {
		header.Set("Content-Encoding", "gzip")
		header.Del("Content-Length")
		// Range requests are served uncompressed, so the gzip body has no ranges.
		header.Del("Accept-Ranges")
		if etag := header.Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
			header.Set("ETag", "W/"+etag)
		}
		writer.ResponseWriter.WriteHeader(status)
		writer.gzip = gzipWriters.Get().(*gzip.Writer)
		writer.gzip.Reset(writer.ResponseWriter)
		destination = writer.gzip
	} else if writer.status != 0 || size != 0 {
		writer.ResponseWriter.WriteHeader(status)
	}
	for _, part := range [][]byte{held, next} {
		if len(part) == 0 {
			continue
		}
		if _, err := destination.Write(part); err != nil {
			return err
		}
	}
	return nil
}

// Flush sends what the handler wrote so far, deciding with the body held.
func (writer *compressingWriter) Flush() {
	if !writer.decided {
		_ = writer.decide(nil)
	}
	if writer.gzip != nil {
		_ = writer.gzip.Flush()
	}
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (writer *compressingWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *compressingWriter) close() {
	if !writer.decided {
		_ = writer.decide(nil)
	}
	if writer.gzip != nil {
		_ = writer.gzip.Close()
		writer.gzip.Reset(io.Discard)
		gzipWriters.Put(writer.gzip)
		writer.gzip = nil
	}
}
