package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestAcceptsGzip(t *testing.T) {
	for header, want := range map[string]bool{
		"":                    false,
		"gzip":                true,
		"br, gzip;q=0.8":      true,
		"GZIP":                true,
		"x-gzip":              true,
		"gzip;q=0":            false,
		"*":                   true,
		"*;q=0":               false,
		"br, *":               true,
		"*, gzip;q=0":         false,
		"identity":            false,
		"deflate, br":         false,
		"gzip;q=0.000, br":    false,
		"gzip ; q = 0.5, br":  true,
		"gzip;level=1;q=0.2":  true,
		"gzip;q=not-a-number": false,
	} {
		if got := acceptsGzip(header); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestCompressResponses(t *testing.T) {
	large := strings.Repeat(`{"title":"Acne care","state":"public"},`, 100)
	serve := func(method, path, acceptEncoding string, header http.Header, handler http.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, "http://ridu.test"+path, nil)
		if acceptEncoding != "" {
			request.Header.Set("Accept-Encoding", acceptEncoding)
		}
		for name, values := range header {
			request.Header[name] = values
		}
		recorder := httptest.NewRecorder()
		compressResponses(handler).ServeHTTP(recorder, request)
		return recorder
	}
	write := func(contentType, body string) http.HandlerFunc {
		return func(writer http.ResponseWriter, _ *http.Request) {
			if contentType != "" {
				writer.Header().Set("Content-Type", contentType)
			}
			writer.Header().Set("Content-Length", "999999")
			writer.Header().Set("ETag", `"v1"`)
			writer.Header().Set("Accept-Ranges", "bytes")
			_, _ = io.WriteString(writer, body)
		}
	}
	decoded := func(t *testing.T, recorder *httptest.ResponseRecorder) string {
		t.Helper()
		if recorder.Header().Get("Content-Encoding") != "gzip" {
			return recorder.Body.String()
		}
		reader, err := gzip.NewReader(bytes.NewReader(recorder.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	for _, test := range []struct {
		name, method, path, accept string
		header                     http.Header
		handler                    http.HandlerFunc
		compressed, vary           bool
	}{
		{name: "JSON read", method: http.MethodGet, path: "/api/collections/posts", accept: "gzip, br", handler: write("application/json", large), compressed: true, vary: true},
		{name: "prepared route state", method: http.MethodGet, path: "/admin/collections/posts", accept: "gzip", handler: write("application/vnd.ridu.admin-route-state+json", large), compressed: true, vary: true},
		{name: "admin HTML", method: http.MethodGet, path: "/admin/", accept: "gzip", handler: write("text/html; charset=utf-8", "<!doctype html>"+large), compressed: true, vary: true},
		{name: "sniffed text", method: http.MethodGet, path: "/api/custom", accept: "gzip", handler: write("", large), compressed: true, vary: true},
		{name: "small body", method: http.MethodGet, path: "/api/collections/posts", accept: "gzip", handler: write("application/json", `{"docs":[]}`), vary: true},
		{name: "client without gzip", method: http.MethodGet, path: "/api/collections/posts", handler: write("application/json", large), vary: true},
		{name: "gzip refused", method: http.MethodGet, path: "/api/collections/posts", accept: "gzip;q=0, br", handler: write("application/json", large), vary: true},
		{name: "image", method: http.MethodGet, path: "/api/uploads/media/photo.png", accept: "gzip", handler: write("image/png", large)},
		{name: "event stream", method: http.MethodGet, path: "/api/stream", accept: "gzip", handler: write("text/event-stream", large)},
		{name: "already encoded", method: http.MethodGet, path: "/admin/assets/app.js", accept: "gzip", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "text/javascript")
			writer.Header().Set("Content-Encoding", "br")
			_, _ = io.WriteString(writer, large)
		}},
		{name: "POST", method: http.MethodPost, path: "/api/collections/posts", accept: "gzip", handler: write("application/json", large)},
		{name: "auth read", method: http.MethodGet, path: "/api/auth/me", accept: "gzip", handler: write("application/json", large)},
		{name: "range request", method: http.MethodGet, path: "/api/uploads/media/notes.txt", accept: "gzip", header: http.Header{"Range": {"bytes=0-99"}}, handler: write("text/plain", large)},
		{name: "not modified", method: http.MethodGet, path: "/admin/assets/app.js", accept: "gzip", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "text/javascript")
			writer.WriteHeader(http.StatusNotModified)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := serve(test.method, test.path, test.accept, test.header, test.handler)
			compressed := recorder.Header().Get("Content-Encoding") == "gzip"
			if compressed != test.compressed {
				t.Fatalf("compressed = %v, want %v (headers %v)", compressed, test.compressed, recorder.Header())
			}
			if vary := strings.Contains(strings.Join(recorder.Header().Values("Vary"), ","), "Accept-Encoding"); vary != test.vary {
				t.Fatalf("Vary: Accept-Encoding = %v, want %v", vary, test.vary)
			}
			if test.compressed {
				if recorder.Header().Get("Content-Length") != "" || recorder.Header().Get("ETag") != `W/"v1"` || recorder.Header().Get("Accept-Ranges") != "" {
					t.Fatalf("compressed headers = %v", recorder.Header())
				}
				if body := decoded(t, recorder); !strings.HasSuffix(body, large) {
					t.Fatalf("decoded body has %d bytes", len(body))
				}
				if recorder.Body.Len() >= len(large) {
					t.Fatalf("gzip didn't shrink the body: %d >= %d", recorder.Body.Len(), len(large))
				}
			}
		})
	}

	t.Run("status and flushed streaming", func(t *testing.T) {
		recorder := serve(http.MethodGet, "/api/collections/posts", "gzip", nil, func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, large[:500])
			http.NewResponseController(writer).Flush()
			_, _ = io.WriteString(writer, large[500:])
		})
		if recorder.Code != http.StatusNotFound || !recorder.Flushed {
			t.Fatalf("status = %d, flushed = %v", recorder.Code, recorder.Flushed)
		}
		if body := decoded(t, recorder); body != large {
			t.Fatalf("flushed body has %d bytes, want %d", len(body), len(large))
		}
	})
	t.Run("compressed streaming", func(t *testing.T) {
		recorder := serve(http.MethodGet, "/api/collections/posts", "gzip", nil, func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, large[:2000])
			http.NewResponseController(writer).Flush()
			if !writer.(*compressingWriter).decided {
				t.Error("flush didn't send the response")
			}
			_, _ = io.WriteString(writer, large[2000:])
		})
		if recorder.Header().Get("Content-Encoding") != "gzip" || !recorder.Flushed {
			t.Fatalf("flushed = %v, headers %v", recorder.Flushed, recorder.Header())
		}
		if body := decoded(t, recorder); body != large {
			t.Fatalf("flushed body has %d bytes, want %d", len(body), len(large))
		}
	})
	t.Run("empty response keeps its status", func(t *testing.T) {
		recorder := serve(http.MethodGet, "/api/collections/posts/one", "gzip", nil, func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		})
		if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
			t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
		}
	})
}

func TestRiduCompressesReadsButNotSignIn(t *testing.T) {
	fields := make([]schema.Field, 0, 40)
	for index := range 40 {
		name := "field" + strconv.Itoa(index)
		path, _ := query.NewPath(name)
		fields = append(fields, schema.Field{ID: schema.StableID(name), Name: name, Path: path, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar})
	}
	handler := New(Config{Manifest: schema.NewManifest(schema.Snapshot{Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: fields}}})})
	read := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "http://ridu.test"+path, nil)
		request.Header.Set("Accept-Encoding", "gzip")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	manifest := read("/api/schema")
	if manifest.Code != http.StatusOK || manifest.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("schema = %d, headers %v", manifest.Code, manifest.Header())
	}
	reader, err := gzip.NewReader(manifest.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil || !bytes.Contains(decoded, []byte("field39")) {
		t.Fatalf("decoded schema = %d bytes, %v", len(decoded), err)
	}
	if manifest.Header().Get("X-Request-ID") == "" {
		t.Fatal("compression dropped Ridu's own headers")
	}
	login := httptest.NewRequest(http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"a@example.test","password":"x","transport":"token"}`))
	login.Header.Set("Accept-Encoding", "gzip")
	login.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, login)
	if recorder.Header().Get("Content-Encoding") != "" {
		t.Fatalf("sign-in response was compressed: %v", recorder.Header())
	}
}
