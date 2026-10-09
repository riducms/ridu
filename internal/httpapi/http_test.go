package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/internal/teststore"
	operationkind "github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

type hijackResponseWriter struct {
	header http.Header
	local  net.Conn
	peer   net.Conn
}

func newHijackResponseWriter() *hijackResponseWriter {
	local, peer := net.Pipe()
	return &hijackResponseWriter{header: make(http.Header), local: local, peer: peer}
}

func (writer *hijackResponseWriter) Header() http.Header { return writer.header }
func (writer *hijackResponseWriter) Write(encoded []byte) (int, error) {
	return writer.local.Write(encoded)
}
func (writer *hijackResponseWriter) WriteHeader(int) {}
func (writer *hijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return writer.local, bufio.NewReadWriter(bufio.NewReader(writer.local), bufio.NewWriter(writer.local)), nil
}

type unwrapResponseWriter struct {
	http.ResponseWriter
}

func (writer *unwrapResponseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func TestStatusWriterTracksSuccessfulHijack(t *testing.T) {
	underlying := newHijackResponseWriter()
	defer underlying.peer.Close()
	tracked := &statusWriter{
		ResponseWriter: &unwrapResponseWriter{ResponseWriter: underlying},
		status:         http.StatusOK,
	}
	connection, _, err := http.NewResponseController(tracked).Hijack()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if !tracked.wroteHeader || tracked.status != http.StatusOK {
		t.Fatalf("hijacked status = wrote:%v status:%d", tracked.wroteHeader, tracked.status)
	}
}

func TestDecodeWhereEnforcesDepthExpressionMembershipAndDuplicateLimits(t *testing.T) {
	collection := schema.Collection{Fields: []schema.Field{{Name: "title", Type: schema.FieldTypeText}}}
	valid := []byte(`{"and":[{"title":{"equals":"one"}},{"title":{"in":["two","three"]}}]}`)
	if _, err := decodeWhere(valid, collection); err != nil {
		t.Fatal(err)
	}

	deep := `{"title":{"equals":"value"}}`
	for range maxWhereDepth {
		deep = `{"not":` + deep + `}`
	}
	if _, err := decodeWhere([]byte(deep), collection); err == nil || !strings.Contains(err.Error(), "depth exceeds") {
		t.Fatalf("deep where error = %v", err)
	}

	members := make([]string, maxWhereInValues+1)
	for index := range members {
		members[index] = fmt.Sprintf("%q", fmt.Sprintf("%d", index))
	}
	if _, err := decodeWhere([]byte(`{"title":{"in":[`+strings.Join(members, ",")+`]}}`), collection); err == nil || !strings.Contains(err.Error(), "between 1 and") {
		t.Fatalf("large membership error = %v", err)
	}

	fields := make([]string, maxWhereExpressions+1)
	for index := range fields {
		fields[index] = fmt.Sprintf("%q:{\"equals\":%q}", fmt.Sprintf("field%d", index), "value")
	}
	if _, err := decodeWhere([]byte("{"+strings.Join(fields, ",")+"}"), collection); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("large where error = %v", err)
	}

	if _, err := decodeWhere([]byte(`{"title":{"equals":"one"},"title":{"equals":"two"}}`), collection); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate where error = %v", err)
	}
}

func TestDecodeWhereAcceptsOnlyQueryableSystemFields(t *testing.T) {
	collection := schema.Collection{
		Capabilities: schema.Capabilities{Versions: true},
		Versions:     &schema.VersionSettings{Drafts: true},
	}
	for _, encoded := range []string{
		`{"id":{"equals":"document-1"}}`,
		`{"createdAt":{"greaterThan":"2026-01-01T00:00:00+01:00"}}`,
		`{"updatedAt":{"lessThanEqual":"2026-01-01T00:00:00Z"}}`,
		`{"_status":{"equals":"published"}}`,
	} {
		if _, err := decodeWhere([]byte(encoded), collection); err != nil {
			t.Fatalf("decodeWhere(%s): %v", encoded, err)
		}
	}
	timestampWhere, err := decodeWhere([]byte(`{"createdAt":{"greaterThan":"2026-01-01T00:00:00+01:00"}}`), collection)
	if err != nil {
		t.Fatal(err)
	}
	timestampValue, _ := timestampWhere.Node().Comparison.Value.StringValue()
	if timestampValue != "2025-12-31T23:00:00Z" {
		t.Fatalf("normalized createdAt = %q", timestampValue)
	}

	unversioned := schema.Collection{}
	for _, encoded := range []string{
		`{"deletedAt":{"exists":false}}`,
		`{"_status":{"equals":"published"}}`,
		`{"_revision":{"greaterThan":1}}`,
	} {
		if _, err := decodeWhere([]byte(encoded), unversioned); err == nil {
			t.Fatalf("decodeWhere(%s) unexpectedly accepted an unavailable system field", encoded)
		}
	}

	for _, encoded := range []string{
		`{"createdAt":{"greaterThan":"not-a-timestamp"}}`,
		`{"updatedAt":{"equals":42}}`,
		`{"createdAt":{"contains":"2026"}}`,
	} {
		if _, err := decodeWhere([]byte(encoded), unversioned); err == nil {
			t.Fatalf("decodeWhere(%s) accepted an invalid timestamp comparison", encoded)
		}
	}

	sorts, err := decodeSort([]string{"-createdAt", "updatedAt"})
	if err != nil {
		t.Fatalf("decode timestamp sort: %v", err)
	}
	if len(sorts) != 2 || sorts[0].Path.String() != "createdAt" || sorts[0].Direction != query.Descending || sorts[1].Path.String() != "updatedAt" {
		t.Fatalf("timestamp sorts = %#v", sorts)
	}
}

func TestDecodeDraftQueryPreservesAbsentAndExplicitPublicationIntent(t *testing.T) {
	tests := []struct {
		target string
		want   *bool
	}{
		{target: "/api/collections/posts"},
		{target: "/api/collections/posts?draft=true", want: boolPointer(true)},
		{target: "/api/collections/posts?draft=false", want: boolPointer(false)},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodPost, test.target, nil)
		actual, err := decodeDraftQuery(request)
		if err != nil {
			t.Fatalf("decode %s: %v", test.target, err)
		}
		if actual == nil || test.want == nil {
			if actual != nil || test.want != nil {
				t.Fatalf("decode %s = %v, want %v", test.target, actual, test.want)
			}
			continue
		}
		if *actual != *test.want {
			t.Fatalf("decode %s = %v, want %v", test.target, *actual, *test.want)
		}
	}
	for _, target := range []string{
		"/api/collections/posts?draft=maybe",
		"/api/collections/posts?draft=true&draft=false",
	} {
		if _, err := decodeDraftQuery(httptest.NewRequest(http.MethodPost, target, nil)); err == nil {
			t.Fatalf("invalid draft query succeeded: %s", target)
		}
	}
}

func boolPointer(value bool) *bool { return &value }

func TestCORSAllowsSDKRevisionHeadersAndRejectsUnadvertisedHeaders(t *testing.T) {
	handler := New(Config{AllowedOrigins: []string{"https://admin.example.test"}})
	request := httptest.NewRequest(http.MethodOptions, "https://api.example.test/api/schema", nil)
	request.Header.Set("Origin", "https://admin.example.test")
	request.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	request.Header.Set("Access-Control-Request-Headers", "Content-Type, If-Match, Accept")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d: %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{"Accept", "Authorization", "Content-Type", "If-Match"} {
		if !strings.Contains(response.Header().Get("Access-Control-Allow-Headers"), expected) {
			t.Fatalf("allow headers = %q, missing %q", response.Header().Get("Access-Control-Allow-Headers"), expected)
		}
	}

	request = httptest.NewRequest(http.MethodOptions, "https://api.example.test/api/schema", nil)
	request.Header.Set("Origin", "https://admin.example.test")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "X-CSRF-Token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unadvertised header preflight = %d, want 403", response.Code)
	}
}

func TestScheduledPublicationTransportCarriesExactAuthIdentity(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "scheduled identity transport"},
		Collections: []schema.Collection{{
			ID: "collection-posts", Slug: "posts", Versions: &schema.VersionSettings{Drafts: true},
			Fields: []schema.Field{},
		}},
		Plugins: []schema.Plugin{},
	})
	var received *AuthIdentity
	unpublishCalled := false
	handler := New(Config{
		Manifest: manifest,
		AuthenticateAPIKey: func(context.Context, string) (AuthIdentity, error) {
			return AuthIdentity{Collection: "staff", Actor: store.Document{ID: "shared-requester", Values: store.Values{"role": store.String("publisher")}}}, nil
		},
		SchedulePublish: func(_ context.Context, collection, documentID string, runAt time.Time, timeZone string, revision int, identity *AuthIdentity) (store.ScheduledPublication, error) {
			received = identity
			return store.ScheduledPublication{ID: "schedule-1", Action: store.PublicationActionPublish, CollectionID: "collection-posts", DocumentID: documentID, ExpectedRevision: revision, RunAt: runAt, TimeZone: timeZone, CreatedAt: time.Now().UTC()}, nil
		},
		ScheduleUnpublish: func(_ context.Context, collection, documentID string, runAt time.Time, timeZone string, revision int, identity *AuthIdentity) (store.ScheduledPublication, error) {
			unpublishCalled = true
			received = identity
			return store.ScheduledPublication{ID: "schedule-2", Action: store.PublicationActionUnpublish, CollectionID: "collection-posts", DocumentID: documentID, ExpectedRevision: revision, RunAt: runAt, TimeZone: timeZone, CreatedAt: time.Now().UTC()}, nil
		},
		ScheduledPublications: func(context.Context, string, string, *AuthIdentity) ([]store.ScheduledPublication, error) {
			return nil, nil
		},
		CancelScheduledPublication: func(context.Context, string, string, string, *AuthIdentity) error { return nil },
	})
	request := httptest.NewRequest(http.MethodPost, "/api/collections/posts/post-1/schedule", strings.NewReader(`{"action":"publish","runAt":"2030-01-02T03:04:05Z","timeZone":"Asia/Kolkata"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer ridu_exact_token")
	request.Header.Set("If-Match", `"7"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("schedule response = %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"timeZone":"Asia/Kolkata"`) {
		t.Fatalf("missing schedule timezone: %s", response.Body.String())
	}
	if received == nil || received.Collection != "staff" || received.Actor.ID != "shared-requester" {
		t.Fatalf("scheduled identity = %#v", received)
	}
	unpublishRequest := httptest.NewRequest(http.MethodPost, "/api/collections/posts/post-1/schedule", strings.NewReader(`{"action":"unpublish","runAt":"2030-01-02T03:04:05Z","timeZone":"Asia/Kolkata"}`))
	unpublishRequest.Header.Set("Content-Type", "application/json")
	unpublishRequest.Header.Set("Authorization", "Bearer ridu_exact_token")
	unpublishResponse := httptest.NewRecorder()
	handler.ServeHTTP(unpublishResponse, unpublishRequest)
	if !strings.Contains(unpublishResponse.Body.String(), `"timeZone":"Asia/Kolkata"`) {
		t.Fatalf("missing unpublish timezone: %s", unpublishResponse.Body.String())
	}
	if unpublishResponse.Code != http.StatusCreated || !unpublishCalled || received == nil || received.Collection != "staff" {
		t.Fatalf("scheduled unpublish = %d, called %v, identity %#v: %s", unpublishResponse.Code, unpublishCalled, received, unpublishResponse.Body.String())
	}
	for name, body := range map[string]string{
		"missing action": `{"runAt":"2030-01-02T03:04:05Z","timeZone":"Asia/Kolkata"}`,
		"unknown action": `{"action":"archive","runAt":"2030-01-02T03:04:05Z","timeZone":"Asia/Kolkata"}`,
	} {
		t.Run(name, func(t *testing.T) {
			invalidRequest := httptest.NewRequest(http.MethodPost, "/api/collections/posts/post-1/schedule", strings.NewReader(body))
			invalidRequest.Header.Set("Content-Type", "application/json")
			invalidResponse := httptest.NewRecorder()
			handler.ServeHTTP(invalidResponse, invalidRequest)
			if invalidResponse.Code != http.StatusUnprocessableEntity {
				t.Fatalf("invalid scheduled publication = %d: %s", invalidResponse.Code, invalidResponse.Body.String())
			}
		})
	}
}

func TestNativeHTTPAuthenticationCredentials(t *testing.T) {
	api := &API{config: Config{
		Session: func(_ context.Context, token string) (AuthSession, error) {
			if token != "session-token" && token != "cookie-token" {
				return AuthSession{}, errors.New("invalid session")
			}
			return AuthSession{Collection: "staff", User: store.Document{ID: token}}, nil
		},
		AuthenticateAPIKey: func(_ context.Context, token string) (AuthIdentity, error) {
			if token != "ridu_key_secret" {
				return AuthIdentity{}, errors.New("invalid API key")
			}
			return AuthIdentity{Collection: "staff", Actor: store.Document{ID: token}}, nil
		},
	}}
	for _, test := range []struct {
		name, authorization, cookie, actorID string
	}{
		{name: "cookie", cookie: "cookie-token", actorID: "cookie-token"},
		{name: "session header", authorization: "Session session-token", actorID: "session-token"},
		{name: "case insensitive session header", authorization: "sEsSiOn session-token", actorID: "session-token"},
		{name: "API key", authorization: "Bearer ridu_key_secret", actorID: "ridu_key_secret"},
		{name: "case insensitive API key", authorization: "bEaReR ridu_key_secret", actorID: "ridu_key_secret"},
		{name: "JWT session header", authorization: "JWT session-token"},
		{name: "mixed case JWT session header", authorization: "jWt session-token"},
		{name: "session cannot be an API key", authorization: "Bearer session-token"},
		{name: "API key cannot be a session", authorization: "Session ridu_key_secret"},
		{name: "session header precedes cookie", authorization: "Session session-token", cookie: "cookie-token", actorID: "session-token"},
		{name: "API key precedes cookie", authorization: "Bearer ridu_key_secret", cookie: "cookie-token", actorID: "ridu_key_secret"},
		{name: "invalid session header never falls back to cookie", authorization: "Session stale-token", cookie: "cookie-token"},
		{name: "invalid API key never falls back to cookie", authorization: "Bearer ridu_stale_key", cookie: "cookie-token"},
		{name: "empty session header never falls back to cookie", authorization: "Session", cookie: "cookie-token"},
		{name: "application bearer secret keeps cookie", authorization: "Bearer webhook-secret", cookie: "cookie-token", actorID: "cookie-token"},
		{name: "cookie with unsupported header", authorization: "JWT session-token", cookie: "cookie-token", actorID: "cookie-token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/preferences/theme", nil)
			request.Header.Set("Authorization", test.authorization)
			if test.cookie != "" {
				request.AddCookie(&http.Cookie{Name: sessionCookie, Value: test.cookie})
			}
			identity := api.optionalIdentity(request)
			if test.actorID == "" {
				if identity != nil {
					t.Fatalf("unsupported credential authenticated: %#v", identity)
				}
				return
			}
			if identity == nil || identity.Collection != "staff" || identity.Actor.ID != test.actorID {
				t.Fatalf("identity = %#v, want staff/%s", identity, test.actorID)
			}
		})
	}
}

func TestAuditDisambiguatesSameActorIDByAuthCollection(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "audit identity"}, Collections: []schema.Collection{}, Plugins: []schema.Plugin{}})
	var events []AuditEvent
	handler := New(Config{
		Manifest: manifest,
		AuthenticateAPIKey: func(_ context.Context, token string) (AuthIdentity, error) {
			return AuthIdentity{Collection: schema.CollectionSlug(strings.Split(token, "_")[1]), Actor: store.Document{ID: "shared-actor"}}, nil
		},
		ResetPreferences: func(context.Context, *AuthIdentity) error { return nil },
		Audit:            func(event AuditEvent) { events = append(events, event) },
	})
	for _, collection := range []string{"staff", "customers"} {
		request := httptest.NewRequest(http.MethodDelete, "/api/preferences", nil)
		request.Header.Set("Authorization", "Bearer ridu_"+collection+"_secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s reset status = %d: %s", collection, response.Code, response.Body.String())
		}
	}
	if len(events) != 2 || events[0].ActorID != "shared-actor" || events[1].ActorID != "shared-actor" || events[0].ActorCollection != "staff" || events[1].ActorCollection != "customers" {
		t.Fatalf("audit identities = %#v", events)
	}
}

func TestPreferenceAndLockTransportsCarryExactAuthIdentity(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "owned identity transport"},
		Collections: []schema.Collection{{
			ID: "collection-posts", Slug: "posts", DocumentLock: &schema.DocumentLockSettings{DurationSeconds: 120},
			Fields: []schema.Field{},
		}},
		Plugins: []schema.Plugin{},
	})
	var operations []string
	assertIdentity := func(operation string, identity *AuthIdentity) {
		t.Helper()
		if identity == nil || identity.Collection != "staff" || identity.Actor.ID != "shared-actor" {
			t.Fatalf("%s identity = %#v", operation, identity)
		}
		operations = append(operations, operation)
	}
	handler := New(Config{
		Manifest: manifest,
		AuthenticateAPIKey: func(context.Context, string) (AuthIdentity, error) {
			return AuthIdentity{Collection: "staff", Actor: store.Document{ID: "shared-actor"}}, nil
		},
		GetPreference: func(_ context.Context, identity *AuthIdentity, _ string) (json.RawMessage, error) {
			assertIdentity("get preference", identity)
			return json.RawMessage(`null`), nil
		},
		SetPreference: func(_ context.Context, identity *AuthIdentity, _ string, value json.RawMessage) (json.RawMessage, error) {
			assertIdentity("set preference", identity)
			return value, nil
		},
		DeletePreference: func(_ context.Context, identity *AuthIdentity, _ string) error {
			assertIdentity("delete preference", identity)
			return nil
		},
		ResetPreferences: func(_ context.Context, identity *AuthIdentity) error {
			assertIdentity("reset preferences", identity)
			return nil
		},
		DocumentLock: func(_ context.Context, _, _ string, identity *AuthIdentity) (protocol.DocumentLockEnvelope, error) {
			assertIdentity("get lock", identity)
			return protocol.DocumentLockEnvelope{}, nil
		},
		AcquireDocumentLock: func(_ context.Context, _, _ string, _ bool, identity *AuthIdentity) (protocol.DocumentLockEnvelope, error) {
			assertIdentity("acquire lock", identity)
			return protocol.DocumentLockEnvelope{Acquired: true, Owned: true}, nil
		},
		ReleaseDocumentLock: func(_ context.Context, _, _ string, identity *AuthIdentity) error {
			assertIdentity("release lock", identity)
			return nil
		},
	})

	requests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/preferences/theme"},
		{method: http.MethodPut, path: "/api/preferences/theme", body: `{"value":"dark"}`},
		{method: http.MethodDelete, path: "/api/preferences/theme"},
		{method: http.MethodDelete, path: "/api/preferences"},
		{method: http.MethodGet, path: "/api/collections/posts/post-1/lock"},
		{method: http.MethodPost, path: "/api/collections/posts/post-1/lock", body: `{}`},
		{method: http.MethodDelete, path: "/api/collections/posts/post-1/lock"},
	}
	for _, test := range requests {
		var body io.Reader
		if test.body != "" {
			body = strings.NewReader(test.body)
		}
		request := httptest.NewRequest(test.method, test.path, body)
		request.Header.Set("Authorization", "Bearer ridu_exact_token")
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s %s = %d: %s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if len(operations) != len(requests) {
		t.Fatalf("identity-aware operations = %v", operations)
	}
}

func TestSameOriginFollowsTrustedProxySchemeAndAcceptsTheHostsHTTPSOriginBehindOtherProxies(t *testing.T) {
	handler := New(Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}})
	post := func(target, remote, forwardedProto, origin string) int {
		request := httptest.NewRequest(http.MethodPost, target, nil)
		request.RemoteAddr = remote
		if forwardedProto != "" {
			request.Header.Set("X-Forwarded-Proto", forwardedProto)
		}
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	for _, test := range []struct {
		name, target, remote, forwardedProto, origin string
		want                                         int
	}{
		// Routed requests reach the missing route and return 404; refused ones 403.
		{name: "HTTPS downgrade", target: "https://app.example.test/missing", remote: "203.0.113.4:1234", origin: "http://app.example.test", want: http.StatusForbidden},
		{name: "trusted proxy reports HTTPS", target: "http://app.example.test/missing", remote: "192.0.2.10:443", forwardedProto: "https", origin: "https://app.example.test", want: http.StatusNotFound},
		{name: "trusted proxy reports HTTP", target: "http://app.example.test/missing", remote: "192.0.2.10:443", forwardedProto: "http", origin: "https://app.example.test", want: http.StatusForbidden},
		// A platform edge Ridu does not trust terminates TLS: only this host's
		// own HTTPS page sends its HTTPS origin, with or without a spoofable
		// forwarded header.
		{name: "untrusted TLS proxy", target: "http://app.example.test/missing", remote: "198.51.100.10:443", origin: "https://app.example.test", want: http.StatusNotFound},
		{name: "untrusted proxy with forwarded proto", target: "http://app.example.test/missing", remote: "198.51.100.10:443", forwardedProto: "https", origin: "https://app.example.test", want: http.StatusNotFound},
		{name: "untrusted proxy, other host", target: "http://app.example.test/missing", remote: "198.51.100.10:443", forwardedProto: "https", origin: "https://evil.example.test", want: http.StatusForbidden},
		{name: "untrusted proxy, other port", target: "http://app.example.test/missing", remote: "198.51.100.10:443", origin: "https://app.example.test:8443", want: http.StatusForbidden},
	} {
		if got := post(test.target, test.remote, test.forwardedProto, test.origin); got != test.want {
			t.Errorf("%s: %s from origin %s = %d, want %d", test.name, test.target, test.origin, got, test.want)
		}
	}
}

func TestClientIPWalksTrustedForwardingChainFromNearestHop(t *testing.T) {
	api := &API{config: Config{TrustedProxies: []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.51.100.0/24"),
	}}}
	tests := []struct {
		name, remote, chain, want string
	}{
		{name: "untrusted peer ignores header", remote: "203.0.113.4:123", chain: "1.1.1.1", want: "203.0.113.4"},
		{name: "attacker prefix stops at actual client", remote: "192.0.2.4:123", chain: "1.1.1.1, 203.0.113.9", want: "203.0.113.9"},
		{name: "invalid nearest fails closed", remote: "192.0.2.4:123", chain: "1.1.1.1, not-an-ip", want: "192.0.2.4"},
		{name: "all trusted returns leftmost", remote: "192.0.2.4:123", chain: "198.51.100.8, 198.51.100.9", want: "198.51.100.8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://app.example.test/healthz", nil)
			request.RemoteAddr = test.remote
			request.Header.Set("X-Forwarded-For", test.chain)
			if got := api.clientIP(request); got != test.want {
				t.Fatalf("client IP = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPluginBodiesAreBoundedAndPanicsAreRedacted(t *testing.T) {
	var diagnostic RequestErrorEvent
	handler := New(Config{
		MaxBodyBytes: 8,
		RequestError: func(event RequestErrorEvent) { diagnostic = event },
		PluginEndpoints: []PluginEndpoint{
			{Method: http.MethodPost, Path: "/api/plugins/test/read", Handler: func(ctx EndpointContext) {
				writer, request := ctx.Writer, ctx.Request
				if ctx.RequestID == "" || ctx.ClientIP == "" || ctx.AdmitAuthAttempt == nil || ctx.ReportError == nil {
					t.Fatal("endpoint metadata is incomplete")
				}
				_, err := io.ReadAll(request.Body)
				var maximum *http.MaxBytesError
				if errors.As(err, &maximum) {
					writer.WriteHeader(http.StatusRequestEntityTooLarge)
				}
			}},
			{Method: http.MethodGet, Path: "/api/plugins/test/panic", Handler: func(EndpointContext) {
				panic("super-secret-panic-value")
			}},
		},
	})
	tooLarge := httptest.NewRecorder()
	handler.ServeHTTP(tooLarge, httptest.NewRequest(http.MethodPost, "/api/plugins/test/read", strings.NewReader("0123456789")))
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("plugin body limit status = %d", tooLarge.Code)
	}

	panicked := httptest.NewRecorder()
	handler.ServeHTTP(panicked, httptest.NewRequest(http.MethodGet, "/api/plugins/test/panic", nil))
	if panicked.Code != http.StatusInternalServerError || strings.Contains(panicked.Body.String(), "super-secret") {
		t.Fatalf("panic response = %d: %s", panicked.Code, panicked.Body.String())
	}
	if diagnostic.Error == nil || strings.Contains(diagnostic.Error.Error(), "super-secret") || !diagnostic.Panic || diagnostic.Stack == "" {
		t.Fatalf("panic diagnostic = %#v", diagnostic)
	}
}

func TestInternalOperationErrorsDoNotLeakMessagesOrIssues(t *testing.T) {
	response := httptest.NewRecorder()
	(&API{}).writeError(response, "request-1", &operation.Error{
		Code: "access_denied", Status: http.StatusInternalServerError,
		Message: "postgres://user:super-secret@db",
		Issues:  []schema.Issue{{Code: "internal_detail", Path: "database", Message: "token=super-secret"}},
	})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	body := response.Body.String()
	if strings.Contains(body, "super-secret") || strings.Contains(body, "internal_detail") || strings.Contains(body, "database") {
		t.Fatalf("internal response leaked diagnostics: %s", body)
	}
	if !strings.Contains(body, `"message":"internal server error"`) {
		t.Fatalf("internal response is not the stable public envelope: %s", body)
	}
	if !strings.Contains(body, `"code":"internal"`) {
		t.Fatalf("internal response leaked a non-internal code: %s", body)
	}
}

func TestInternalErrorsUseDefaultLoggingAndRecoverPanickingCallbacks(t *testing.T) {
	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	panicEndpoint := PluginEndpoint{Method: http.MethodGet, Path: "/api/plugins/test/panic", Handler: func(EndpointContext) {
		panic("untrusted-secret")
	}}
	response := httptest.NewRecorder()
	New(Config{PluginEndpoints: []PluginEndpoint{panicEndpoint}}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, panicEndpoint.Path, nil))
	if !strings.Contains(logs.String(), "Ridu request failed") || strings.Contains(logs.String(), "untrusted-secret") {
		t.Fatalf("default request log = %q", logs.String())
	}

	logs.Reset()
	response = httptest.NewRecorder()
	New(Config{RequestError: func(RequestErrorEvent) { panic("callback-secret") }, PluginEndpoints: []PluginEndpoint{panicEndpoint}}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, panicEndpoint.Path, nil))
	if !strings.Contains(logs.String(), "RequestError callback panicked") || strings.Contains(logs.String(), "callback-secret") || strings.Contains(logs.String(), "untrusted-secret") {
		t.Fatalf("callback fallback log = %q", logs.String())
	}
}

// A refused host is logged once, so a health check from an unlisted host has
// a visible cause, and the log stays bounded however many hosts are refused.
func TestDeniedHostsAreLoggedOnceAndBounded(t *testing.T) {
	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	handler := New(Config{AllowedHosts: []string{"my-app.up.railway.app"}})
	for range 2 {
		request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		request.Host = "healthcheck.railway.app"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "request host is not allowed") {
			t.Fatalf("unlisted host = %d %s", response.Code, response.Body.String())
		}
	}
	malformed := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	malformed.Host = "bad_host!"
	handler.ServeHTTP(httptest.NewRecorder(), malformed)
	if count := strings.Count(logs.String(), "host="); count != 1 || !strings.Contains(logs.String(), "host=healthcheck.railway.app") || !strings.Contains(logs.String(), "RIDU_ALLOWED_HOSTS") {
		t.Fatalf("denied host logged %d times: %q", count, logs.String())
	}
	allowed := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	allowed.Host = "my-app.up.railway.app"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, allowed)
	if response.Code != http.StatusOK {
		t.Fatalf("listed host = %d %s", response.Code, response.Body.String())
	}

	logs.Reset()
	for index := range 2 * maxLoggedRefusals {
		request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		request.Host = fmt.Sprintf("probe-%d.example.test", index)
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	if count := strings.Count(logs.String(), "host=probe-"); count != maxLoggedRefusals-1 || !strings.Contains(logs.String(), "logs no further refused hosts") {
		t.Fatalf("logged %d further hosts: %q", count, logs.String())
	}
}

func TestDeniedOriginsAreLoggedOnceWithTheRequestOrigin(t *testing.T) {
	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	handler := New(Config{})
	send := func(method, origin string) int {
		request := httptest.NewRequest(method, "http://api.example.test/missing", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	for range 2 {
		if code := send(http.MethodPost, "https://Web.Example.test"); code != http.StatusForbidden {
			t.Fatalf("cross-origin POST = %d, want 403", code)
		}
	}
	// A GET is answered without CORS headers rather than refused, so it is not logged.
	send(http.MethodGet, "https://reader.example.test")
	if count := strings.Count(logs.String(), "origin=https://web.example.test request_origin=http://api.example.test"); count != 1 || strings.Contains(logs.String(), "reader.example.test") || !strings.Contains(logs.String(), "RIDU_ALLOWED_ORIGINS") {
		t.Fatalf("denied origin logs: %q", logs.String())
	}
}

func TestFailedAndRateLimitedLoginsEmitAuditEventsWithoutIdentityData(t *testing.T) {
	var events []AuditEvent
	handler := New(Config{
		Login: func(context.Context, string, string, string, LoginMetadata) (AuthSession, error) {
			return AuthSession{}, errors.New("invalid credentials")
		},
		Audit: func(event AuditEvent) { events = append(events, event) },
	})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/users/login", strings.NewReader(`{"email":"private@example.test","password":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if len(events) != 1 || events[0].Action != "login_failed" || events[0].Collection != "users" || events[0].DocumentID != "" || events[0].ActorID != "" {
		t.Fatalf("failed-login audits = %#v", events)
	}

	events = nil
	handler = New(Config{
		Login: func(context.Context, string, string, string, LoginMetadata) (AuthSession, error) {
			return AuthSession{}, nil
		},
		AllowAuthIPAttempt: func(context.Context, string, string, int, time.Duration) (bool, error) { return false, nil },
		Audit:              func(event AuditEvent) { events = append(events, event) },
	})
	request = httptest.NewRequest(http.MethodPost, "/api/auth/users/login", strings.NewReader(`{"email":"private@example.test","password":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if len(events) != 1 || events[0].Action != "login_rate_limited" || events[0].DocumentID != "" || events[0].ActorID != "" {
		t.Fatalf("rate-limit audits = %#v", events)
	}
}

func TestMultipartUploadsUseBoundedMemorySpoolingAndRemoveTemporaryFiles(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "multipart bounds"},
		Collections: []schema.Collection{{ID: "collection-media", Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 10 << 20}}},
		Plugins:     []schema.Plugin{},
	})
	var spooledPath string
	handler := New(Config{Manifest: manifest, Upload: func(_ context.Context, _ string, input UploadInput, _ *AuthIdentity, _ bool) (store.Document, error) {
		reader := input.Reader
		file, ok := reader.(*os.File)
		if !ok {
			return store.Document{}, fmt.Errorf("large multipart file remained memory-backed as %T", reader)
		}
		spooledPath = file.Name()
		if _, err := io.Copy(io.Discard, reader); err != nil {
			return store.Document{}, err
		}
		return store.Document{ID: "media-1", Values: store.Values{}}, nil
	}})
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	part, err := multipartWriter.CreateFormFile("file", "large.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, io.LimitReader(zeroReader{}, maxMultipartMemory+1)); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/collections/media", &body)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("large multipart response = %d: %s", response.Code, response.Body.String())
	}
	if spooledPath == "" {
		t.Fatal("large multipart upload did not use a temporary file")
	}
	if _, err := os.Stat(spooledPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("multipart temporary file remained after request: %v", err)
	}
}

func TestAccessCheckedPublicUploadDeliveryIsNeverSharedCacheable(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "access checked upload"},
		Collections: []schema.Collection{{
			ID: "media", Slug: "media", Capabilities: schema.Capabilities{Upload: true},
			Upload: &schema.UploadSettings{Private: false},
		}},
		Plugins: []schema.Plugin{},
	})
	handler := New(Config{
		Manifest: manifest,
		Session: func(context.Context, string) (AuthSession, error) {
			return AuthSession{Collection: "users", User: store.Document{ID: "editor"}}, nil
		},
		OpenUpload: func(_ context.Context, _, _ string, identity *AuthIdentity) (io.ReadCloser, storage.Object, error) {
			if identity == nil {
				return nil, storage.Object{}, &operation.Error{Code: "access_denied", Status: http.StatusForbidden, Message: "denied"}
			}
			return io.NopCloser(strings.NewReader("safe")), storage.Object{Size: 4, ContentType: "text/plain"}, nil
		},
	})

	authenticated := httptest.NewRequest(http.MethodGet, "/api/uploads/media/ridu/app/objects/0123456789abcdef0123456789abcdef/safe.txt", nil)
	authenticated.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticated)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "private, no-store" || response.Body.String() != "safe" {
		t.Fatalf("authenticated delivery = %d cache=%q body=%q", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}

	anonymous := httptest.NewRequest(http.MethodGet, "/api/uploads/media/ridu/app/objects/0123456789abcdef0123456789abcdef/safe.txt", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, anonymous)
	if response.Code != http.StatusForbidden || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("anonymous delivery = %d cache=%q", response.Code, response.Header().Get("Cache-Control"))
	}
}

type blockingMultipartBody struct {
	reads   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (body *blockingMultipartBody) Read([]byte) (int, error) {
	body.reads.Add(1)
	select {
	case body.started <- struct{}{}:
	default:
	}
	<-body.release
	return 0, io.EOF
}

type countingMultipartBody struct{ reads atomic.Int32 }

func (body *countingMultipartBody) Read([]byte) (int, error) {
	body.reads.Add(1)
	return 0, io.EOF
}

func TestMultipartAdmissionPrecedesAnyRequestBodyRead(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "early multipart admission"},
		Collections: []schema.Collection{{ID: "collection-media", Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 256 << 20}}},
		Plugins:     []schema.Plugin{},
	})
	var held atomic.Bool
	handler := New(Config{
		Manifest: manifest,
		AcquireUpload: func(context.Context, string) (func(), error) {
			if !held.CompareAndSwap(false, true) {
				return nil, &operation.Error{Code: "rate_limited", Status: http.StatusTooManyRequests, Message: "upload processing capacity is busy"}
			}
			return func() { held.Store(false) }, nil
		},
		Upload: func(context.Context, string, UploadInput, *AuthIdentity, bool) (store.Document, error) {
			return store.Document{}, &operation.Error{Code: "access_denied", Status: http.StatusForbidden, Message: "operation is not permitted"}
		},
	})
	firstBody := &blockingMultipartBody{started: make(chan struct{}, 1), release: make(chan struct{})}
	first := httptest.NewRequest(http.MethodPost, "/api/collections/media", firstBody)
	first.Header.Set("Content-Type", "multipart/form-data; boundary=ridu")
	firstDone := make(chan int, 1)
	go func() {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, first)
		firstDone <- response.Code
	}()
	select {
	case <-firstBody.started:
	case <-time.After(time.Second):
		t.Fatal("admitted multipart request did not begin parsing")
	}
	for index := 0; index < 100; index++ {
		body := &countingMultipartBody{}
		request := httptest.NewRequest(http.MethodPost, "/api/collections/media", body)
		request.Header.Set("Content-Type", "multipart/form-data; boundary=ridu")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusTooManyRequests || body.reads.Load() != 0 {
			t.Fatalf("unadmitted request %d = status %d body reads %d: %s", index, response.Code, body.reads.Load(), response.Body.String())
		}
	}
	close(firstBody.release)
	select {
	case status := <-firstDone:
		if status != http.StatusBadRequest {
			t.Fatalf("released malformed multipart status = %d", status)
		}
	case <-time.After(time.Second):
		t.Fatal("released multipart request did not finish")
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}

func TestDecodeListQueryExpandsDepthIntoRelationshipPopulation(t *testing.T) {
	relatedPath, err := query.NewPath("related")
	if err != nil {
		t.Fatal(err)
	}
	coverPath, err := query.NewPath("cover")
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Fields: []schema.Field{
		{Name: "title", Type: schema.FieldTypeText},
		{Name: "related", Path: relatedPath, Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{CollectionID: "posts", CollectionSlug: "posts"}},
		{Name: "cover", Path: coverPath, Type: schema.FieldTypeUpload, Upload: &schema.UploadField{CollectionID: "media", CollectionSlug: "media"}},
	}}

	request := httptest.NewRequest(http.MethodGet, "/api/collections/posts?depth=2", nil)
	options, decodeErr := decodeListQuery(request.URL.Query(), collection, false)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(options.populate) != 2 || options.populate[0].Path.String() != "related" || options.populate[0].Depth != 2 || options.populate[1].Path.String() != "cover" || options.populate[1].Depth != 2 {
		t.Fatalf("depth population = %#v", options.populate)
	}

	for _, target := range []string{
		"/api/collections/posts?depth=6",
		"/api/collections/posts?depth=2&populate=%7B%22related%22%3Atrue%7D",
	} {
		if _, decodeErr := decodeListQuery(httptest.NewRequest(http.MethodGet, target, nil).URL.Query(), collection, false); decodeErr == nil {
			t.Fatalf("invalid depth query succeeded: %s", target)
		}
	}
}

func TestDecodeListQueryPopulatesNestedGroupArrayAndBlockRelationships(t *testing.T) {
	groupPath, _ := query.NewPath("meta", "reviewer")
	arrayPath, _ := query.NewPath("sections", "editor")
	sourcePath, _ := query.NewPath("source")
	layoutPath, _ := query.NewPath("layout")
	person := schema.RelationshipField{CollectionID: "people", CollectionSlug: "people"}
	quote := schema.BlockType{Slug: "quote", TypeName: "Quote", Fields: []schema.Field{{Name: "source", Path: sourcePath, Type: schema.FieldTypeRelationship, Relationship: &person}}}
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: schematest.Bind(t, "posts", []schema.BlockType{quote},
		schema.Field{Name: "meta", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "reviewer", Path: groupPath, Type: schema.FieldTypeRelationship, Relationship: &person}}}},
		schema.Field{Name: "sections", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "editor", Path: arrayPath, Type: schema.FieldTypeRelationship, Relationship: &person}}}},
		schema.Field{Name: "layout", Path: layoutPath, Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{BlockReferences: []string{"quote"}}},
	)}

	request := httptest.NewRequest(http.MethodGet, `/api/collections/posts?populate=%7B%22meta.reviewer%22%3Atrue%2C%22sections.editor%22%3Atrue%2C%22layout.quote.source%22%3Atrue%7D`, nil)
	options, err := decodeListQuery(request.URL.Query(), collection, false)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, candidate := range options.populate {
		seen[candidate.Path.String()] = true
	}
	for _, expected := range []string{"meta.reviewer", "sections.editor", "layout.quote.source"} {
		if !seen[expected] {
			t.Fatalf("nested population paths = %#v, missing %q", seen, expected)
		}
	}

	depthRequest := httptest.NewRequest(http.MethodGet, `/api/collections/posts?depth=2`, nil)
	depthOptions, err := decodeListQuery(depthRequest.URL.Query(), collection, false)
	if err != nil || len(depthOptions.populate) != 3 {
		t.Fatalf("nested depth population = %#v, %v", depthOptions.populate, err)
	}
}

func TestDecodeSelectionAcceptsGeneratedSystemFieldsOnlyWhenEnabled(t *testing.T) {
	titlePath, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	labelPath, _ := query.NewPath("label")
	postsPath, _ := query.NewPath("posts")
	collection := schema.Collection{
		Capabilities: schema.Capabilities{Trash: true, Versions: true},
		Versions:     &schema.VersionSettings{},
		Fields: []schema.Field{
			{Name: "title", Path: titlePath, Type: schema.FieldTypeText},
			{Name: "label", Path: labelPath, Type: schema.FieldTypeVirtual, Category: schema.FieldCategoryPresentation, Virtual: &schema.VirtualField{ValueType: schema.ValueTypeString}},
			{Name: "posts", Path: postsPath, Type: schema.FieldTypeJoin, Category: schema.FieldCategoryPresentation, Join: &schema.JoinField{}},
		},
	}
	selection, err := decodeSelection(`{"id":true,"createdAt":true,"updatedAt":true,"deletedAt":true,"_status":true,"_revision":true,"title":true,"label":true,"posts":true}`, collection)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.stored) != 1 || selection.stored[0].String() != "title" {
		t.Fatalf("stored selection = %#v, want only title projected", selection.stored)
	}
	outputs := map[string]bool{}
	for _, path := range selection.output {
		outputs[path.String()] = true
	}
	if len(outputs) != 2 || !outputs["label"] || !outputs["posts"] {
		t.Fatalf("output selection = %#v, want label and posts", selection.output)
	}
	for _, encoded := range []string{`{}`, `{"title":false}`, `{"id":true,"createdAt":true}`} {
		selection, err := decodeSelection(encoded, collection)
		if err != nil {
			t.Fatalf("decode metadata-only selection %s: %v", encoded, err)
		}
		if selection.stored == nil || len(selection.stored) != 0 || selection.output == nil || len(selection.output) != 0 {
			t.Fatalf("metadata-only selection %s = %#v, want non-nil empty projection", encoded, selection)
		}
	}

	for _, encoded := range []string{`{"deletedAt":true}`, `{"_status":true}`, `{"_revision":true}`} {
		if _, err := decodeSelection(encoded, schema.Collection{}); err == nil {
			t.Fatalf("disabled system selection %s succeeded", encoded)
		}
	}
}

func TestDecodePopulationUsesOneFlatSelectionForPolymorphicTargets(t *testing.T) {
	subjectPath, err := query.NewPath("subject")
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Fields: []schema.Field{{
		Name: "subject", Path: subjectPath, Type: schema.FieldTypeRelationship,
		Relationship: &schema.RelationshipField{Polymorphic: true, Targets: []schema.RelationshipTarget{
			{CollectionID: "authors", CollectionSlug: "authors"},
			{CollectionID: "posts", CollectionSlug: "posts"},
		}},
	}}}
	population, err := decodePopulation(`{"subject":{"select":{"name":true,"title":true}}}`, collection)
	if err != nil {
		t.Fatal(err)
	}
	if len(population) != 1 || population[0].Path.String() != "subject" || len(population[0].Select) != 2 {
		t.Fatalf("population = %#v", population)
	}
	metadataPopulation, err := decodePopulation(`{"subject":{"select":{"id":true,"createdAt":true,"updatedAt":true,"deletedAt":true,"_status":true,"_revision":true}}}`, collection)
	if err != nil {
		t.Fatal(err)
	}
	metadataPaths := map[string]bool{}
	for _, path := range metadataPopulation[0].Select {
		metadataPaths[path.String()] = true
	}
	if len(metadataPaths) != 6 || !metadataPaths["id"] || !metadataPaths["createdAt"] || !metadataPaths["updatedAt"] || !metadataPaths["deletedAt"] || !metadataPaths["_status"] || !metadataPaths["_revision"] {
		t.Fatalf("decoded metadata = %#v, want all metadata forwarded for operation validation", metadataPaths)
	}
	for _, test := range []struct {
		encoded   string
		projected bool
		depth     int
	}{
		{encoded: `{"subject":{}}`, projected: true, depth: 1},
		{encoded: `{"subject":{"select":{"id":true,"name":false}}}`, projected: true, depth: 1},
		{encoded: `{"subject":{"depth":2}}`, projected: false, depth: 2},
		{encoded: `{"subject":{"depth":2,"select":{}}}`, projected: true, depth: 2},
	} {
		population, err := decodePopulation(test.encoded, collection)
		if err != nil || len(population) != 1 {
			t.Fatalf("decode population %s = %#v, %v", test.encoded, population, err)
		}
		if (population[0].Select != nil) != test.projected || population[0].Depth != test.depth {
			t.Fatalf("population %s select/depth = %#v/%d", test.encoded, population[0].Select, population[0].Depth)
		}
	}
	if _, err := decodePopulation(`{"subject":{"select":null}}`, collection); err == nil {
		t.Fatal("null population select succeeded")
	}
	for _, encoded := range []string{
		`{"subject":{"select":{"name":1}}}`,
		`{"subject":{"depth":0}}`,
		`{"subject":{"depth":6}}`,
		`{"subject":{"depth":1,"name":true}}`,
	} {
		if _, err := decodePopulation(encoded, collection); err == nil {
			t.Fatalf("invalid population %s succeeded", encoded)
		}
	}

	if _, err := decodePopulation(`{"subject":{"authors":{"name":true},"posts":{"title":true}}}`, collection); err == nil {
		t.Fatal("per-target nested polymorphic selection succeeded")
	}
}

func TestDecodePopulationTreatsBooleanDepthAndSelectAsAuthoredFields(t *testing.T) {
	subjectPath, err := query.NewPath("subject")
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Fields: []schema.Field{{
		Name: "subject", Path: subjectPath, Type: schema.FieldTypeRelationship,
		Relationship: &schema.RelationshipField{CollectionID: "targets", CollectionSlug: "targets"},
	}}}

	for _, test := range []struct {
		encoded string
		want    map[string]bool
	}{
		{encoded: `{"subject":{"depth":true}}`, want: map[string]bool{"depth": true}},
		{encoded: `{"subject":{"select":true}}`, want: map[string]bool{"select": true}},
		{encoded: `{"subject":{"depth":true,"select":true,"name":false}}`, want: map[string]bool{"depth": true, "select": true}},
	} {
		population, err := decodePopulation(test.encoded, collection)
		if err != nil {
			t.Fatalf("decodePopulation(%s): %v", test.encoded, err)
		}
		if len(population) != 1 || population[0].Depth != 1 || population[0].Select == nil {
			t.Fatalf("population %s = %#v", test.encoded, population)
		}
		got := map[string]bool{}
		for _, path := range population[0].Select {
			got[path.String()] = true
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("population %s selection = %#v, want %#v", test.encoded, got, test.want)
		}
	}
}

func TestDecodeListQueryAcceptsTrashOnlyForEnabledCollections(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/collections/posts?trash=true", nil)
	options, err := decodeListQuery(request.URL.Query(), schema.Collection{Capabilities: schema.Capabilities{Trash: true}}, false)
	if err != nil || !options.trashOnly {
		t.Fatalf("trash query = %#v, %v", options, err)
	}
	if _, err := decodeListQuery(request.URL.Query(), schema.Collection{}, false); err == nil {
		t.Fatal("trash query succeeded for a collection without trash")
	}
	invalid := httptest.NewRequest("GET", "/api/collections/posts?trash=yes", nil)
	if _, err := decodeListQuery(invalid.URL.Query(), schema.Collection{Capabilities: schema.Capabilities{Trash: true}}, false); err == nil {
		t.Fatal("invalid trash query value succeeded")
	}
}

func TestDecodeListQueryAcceptsAccessEnrichmentOnlyOnCollectionLists(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/collections/posts?include-access=true", nil)
	options, err := decodeListQuery(request.URL.Query(), schema.Collection{}, true)
	if err != nil || !options.includeAccess {
		t.Fatalf("include-access query = %#v, %v", options, err)
	}
	for _, target := range []string{
		"/api/collections/posts?include-access=yes",
		"/api/collections/posts?include-access=true&include-access=false",
	} {
		if _, err := decodeListQuery(httptest.NewRequest(http.MethodGet, target, nil).URL.Query(), schema.Collection{}, true); err == nil {
			t.Fatalf("invalid include-access query succeeded: %s", target)
		}
	}
	if _, err := decodeListQuery(request.URL.Query(), schema.Collection{}, false); err == nil {
		t.Fatal("include-access succeeded outside a collection list")
	}
}

func TestDecodeListQueryAcceptsPaginationOnlyOnCollectionLists(t *testing.T) {
	for target, skipTotal := range map[string]bool{
		"/api/collections/posts":                  false,
		"/api/collections/posts?pagination=true":  false,
		"/api/collections/posts?pagination=false": true,
	} {
		options, err := decodeListQuery(httptest.NewRequest(http.MethodGet, target, nil).URL.Query(), schema.Collection{}, true)
		if err != nil || options.skipTotal != skipTotal {
			t.Fatalf("%s = skip total %t, %v; want %t", target, options.skipTotal, err, skipTotal)
		}
	}
	for target, message := range map[string]string{
		"/api/collections/posts?pagination=0":                           "pagination query parameter must be true or false",
		"/api/collections/posts?pagination=":                            "pagination query parameter must be true or false",
		"/api/collections/posts?pagination=false&pagination=false":      "pagination query parameter must be provided once",
		"/api/collections/posts?Pagination=false":                       `unknown query parameter "Pagination"`,
		"/api/collections/posts?pagination=FALSE&include-access=true":   "pagination query parameter must be true or false",
		"/api/collections/posts?include-access=true&pagination=invalid": "pagination query parameter must be true or false",
	} {
		_, err := decodeListQuery(httptest.NewRequest(http.MethodGet, target, nil).URL.Query(), schema.Collection{}, true)
		var operationError *operation.Error
		if !errors.As(err, &operationError) || operationError.Status != http.StatusBadRequest || operationError.Code != "bad_query" || operationError.Message != message {
			t.Fatalf("%s error = %#v, want bad_query %q", target, err, message)
		}
	}
	count := httptest.NewRequest(http.MethodGet, "/api/collections/posts/count?pagination=false", nil)
	if _, err := decodeListQuery(count.URL.Query(), schema.Collection{}, false); err == nil {
		t.Fatal("pagination succeeded on a count query")
	}
}

func TestCollectionListPaginationFalseOmitsTotals(t *testing.T) {
	collection := schema.Collection{
		ID: "collection-posts", Slug: "posts",
		Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
	}
	engine, err := operation.New(operation.Config{
		Store: teststore.New(), Collections: []operation.Collection{{Schema: collection}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"post_1", "post_2", "post_3"} {
		if _, err := engine.Execute(t.Context(), operation.Request{
			Operation: operationkind.Create, Collection: "posts", ImportID: id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	handler := New(Config{
		Engine: engine,
		Manifest: schema.NewManifest(schema.Snapshot{
			Version: schema.CurrentVersion, Collections: []schema.Collection{collection},
		}),
	})
	request := func(target string, status int) map[string]any {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != status {
			t.Fatalf("GET %s = %d, want %d: %s", target, response.Code, status, response.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	for target, want := range map[string]map[string]any{
		"/api/collections/posts?limit=2&sort=id":                              {"page": 1.0, "limit": 2.0, "totalDocs": 3.0, "totalPages": 2.0, "hasNextPage": true, "hasPrevPage": false},
		"/api/collections/posts?limit=2&sort=id&pagination=false":             {"page": 1.0, "limit": 2.0, "hasNextPage": true, "hasPrevPage": false},
		"/api/collections/posts?limit=2&page=2&pagination=false":              {"page": 2.0, "limit": 2.0, "hasNextPage": false, "hasPrevPage": true},
		"/api/collections/posts?limit=3&pagination=false":                     {"page": 1.0, "limit": 3.0, "hasNextPage": false, "hasPrevPage": false},
		"/api/collections/posts?limit=2&page=3&pagination=false":              {"page": 3.0, "limit": 2.0, "hasNextPage": false, "hasPrevPage": true},
		"/api/collections/posts?limit=2&pagination=false&include-access=true": {"page": 1.0, "limit": 2.0, "hasNextPage": true, "hasPrevPage": false},
	} {
		body := request(target, http.StatusOK)
		if !reflect.DeepEqual(body["pagination"], want) {
			t.Fatalf("GET %s pagination = %#v, want %#v", target, body["pagination"], want)
		}
	}
	page := request("/api/collections/posts?limit=2&page=2&sort=id&pagination=false", http.StatusOK)
	if docs, _ := page["docs"].([]any); len(docs) != 1 || docs[0].(map[string]any)["id"] != "post_3" {
		t.Fatalf("last uncounted page docs = %#v", page["docs"])
	}
	for target, message := range map[string]string{
		"/api/collections/posts?pagination=no":          "pagination query parameter must be true or false",
		"/api/collections/posts/count?pagination=false": `unknown query parameter "pagination"`,
	} {
		failure, _ := request(target, http.StatusBadRequest)["error"].(map[string]any)
		if failure["code"] != "bad_query" || failure["message"] != message {
			t.Fatalf("GET %s error = %#v, want bad_query %q", target, failure, message)
		}
	}
}

func TestCollectionListAccessEnvelopeIsOptIn(t *testing.T) {
	collection := schema.Collection{
		ID: "collection-posts", Slug: "posts",
		Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
	}
	engine, err := operation.New(operation.Config{
		Store: teststore.New(), Collections: []operation.Collection{{Schema: collection}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), operation.Request{
		Operation: operationkind.Create, Collection: "posts", ImportID: "post_1",
	}); err != nil {
		t.Fatal(err)
	}
	handler := New(Config{
		Engine: engine,
		Manifest: schema.NewManifest(schema.Snapshot{
			Version: schema.CurrentVersion, Collections: []schema.Collection{collection},
		}),
	})

	request := func(target string) map[string]any {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", target, response.Code, response.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	if _, exists := request("/api/collections/posts")["access"]; exists {
		t.Fatal("ordinary collection page unexpectedly contained access metadata")
	}
	enriched := request("/api/collections/posts?include-access=true")
	access, ok := enriched["access"].(map[string]any)
	if !ok {
		t.Fatalf("enriched access = %#v", enriched["access"])
	}
	documents, ok := access["documents"].(map[string]any)
	if !ok || len(documents) != 1 || documents["post_1"] == nil || access["collection"] == nil {
		t.Fatalf("enriched access = %#v", access)
	}
}

func TestPreviewLifecycleSnapshotPrecedesTransportAuthentication(t *testing.T) {
	var order []string
	handler := New(Config{
		PreviewLifecycleEpoch: func() uint64 {
			order = append(order, "epoch")
			return 7
		},
		Session: func(context.Context, string) (AuthSession, error) {
			order = append(order, "session")
			return AuthSession{Collection: "users", User: store.Document{ID: "user_1"}}, nil
		},
		CreateCollectionPreviewToken: func(_ context.Context, _, _ string, identity *AuthIdentity) (PreviewToken, error) {
			order = append(order, "mint")
			if identity == nil || identity.PreviewEpoch != 7 {
				t.Fatalf("preview identity = %#v", identity)
			}
			return PreviewToken{Token: "token", Resource: "collection", Slug: "posts", DocumentID: "post_1", ExpiresAt: time.Now().Add(time.Minute)}, nil
		},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/preview/collections/posts/post_1/token", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("preview mint = %d: %s", response.Code, response.Body.String())
	}
	if len(order) != 3 || order[0] != "epoch" || order[1] != "session" || order[2] != "mint" {
		t.Fatalf("preview lifecycle order = %v", order)
	}
}
