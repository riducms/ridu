package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/riducms/ridu/internal/jsonlimit"
	"github.com/riducms/ridu/internal/localization"
	operationengine "github.com/riducms/ridu/internal/operation"
	populationwalk "github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

const sessionCookie = "ridu_session"

const (
	maxSortFields         = 16
	maxSelectionFields    = 256
	maxMultipartMemory    = 8 << 20
	defaultRequestTimeout = 60 * time.Second
	defaultAdminCSP       = "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https: http:; font-src 'self' data:; connect-src 'self'; frame-src 'self' https: http:"
)

var defaultCORSRequestHeaders = []string{"Accept", "Authorization", "Content-Type", "If-Match"}

type AuthSession struct {
	ID         string
	Token      string
	Collection schema.CollectionSlug
	User       store.Document
	ExpiresAt  time.Time
	// PreparedSessions and PreparedAPIKeys reuse this exact server-resolved
	// session during admin bootstrap. They are never serialized or exposed to
	// application configuration.
	PreparedSessions func(context.Context) ([]AuthSessionInfo, error)
	PreparedAPIKeys  func(context.Context) ([]APIKeyInfo, error)
}

type AuthIdentity struct {
	Collection   schema.CollectionSlug
	Actor        store.Document
	PreviewEpoch uint64
}

type AuthSessionInfo struct {
	ID         string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	IPAddress  string
	UserAgent  string
	Current    bool
}

type APIKey struct {
	ID        string
	Name      string
	Key       string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type APIKeyInfo struct {
	ID         string
	Name       string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
}

type LoginMetadata struct {
	IPAddress string
	UserAgent string
}

// LocaleOptions carries the validated transport-level localization selection
// into storage-aware application operations such as upload duplication.
type LocaleOptions struct {
	Locale          string
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
	AllLocales      bool
}

type Config struct {
	AdminLoad func(context.Context, AdminLoaderRequest) (json.RawMessage, error)
	Manifest  schema.Manifest
	// Snapshot is the engine's resolved schema, the same content as Manifest,
	// shared read-only so requests reuse its block definition views and the
	// admin presents it without copying. Without one, New reads a copy of Manifest.
	Snapshot schema.Snapshot
	// LocalizationForRequest returns the content localization presented to one
	// identity, or nil when it sees the manifest's settings unchanged. It only
	// narrows admin presentation; it never weakens API validation or access.
	LocalizationForRequest       func(context.Context, *AuthIdentity) (*schema.LocalizationSettings, error)
	Engine                       *operationengine.Engine
	AdminAssets                  fs.FS
	MaxBodyBytes                 int64
	SecureCookies                bool
	Login                        func(context.Context, string, string, string, LoginMetadata) (AuthSession, error)
	Session                      func(context.Context, string) (AuthSession, error)
	RotateSession                func(context.Context, string) (AuthSession, error)
	Logout                       func(context.Context, string) error
	LogoutAll                    func(context.Context, string) error
	Sessions                     func(context.Context, string) ([]AuthSessionInfo, error)
	RevokeSession                func(context.Context, string, string) error
	RequestPasswordReset         func(context.Context, string, string) error
	ResetPassword                func(context.Context, string, string, string) error
	RequestVerification          func(context.Context, string, string) error
	VerifyEmail                  func(context.Context, string, string) error
	ChangePassword               func(context.Context, string, string, string) error
	AuthBootstrapAvailable       func(context.Context, string) (bool, error)
	CreateAuthUser               func(context.Context, string, store.Values, string, *AuthIdentity, LocaleOptions, *bool) (store.Document, error)
	CreateAPIKey                 func(context.Context, string, string, time.Time) (APIKey, error)
	APIKeys                      func(context.Context, string) ([]APIKeyInfo, error)
	RevokeAPIKey                 func(context.Context, string, string) error
	AuthenticateAPIKey           func(context.Context, string) (AuthIdentity, error)
	AuthenticateExternal         func(context.Context, map[string][]string) (AuthIdentity, error)
	PreviewLifecycleEpoch        func() uint64
	CreateCollectionPreviewToken func(context.Context, string, string, *AuthIdentity) (PreviewToken, error)
	CreateGlobalPreviewToken     func(context.Context, string, *AuthIdentity) (PreviewToken, error)
	RevokePreviewToken           func(context.Context, string, *AuthIdentity) error
	FindCollectionPreview        func(context.Context, string, string, string) (store.Document, error)
	FindGlobalPreview            func(context.Context, string, string) (store.Document, error)
	GetPreference                func(context.Context, *AuthIdentity, string) (json.RawMessage, error)
	SetPreference                func(context.Context, *AuthIdentity, string, json.RawMessage) (json.RawMessage, error)
	DeletePreference             func(context.Context, *AuthIdentity, string) error
	ResetPreferences             func(context.Context, *AuthIdentity) error
	ForceUnlock                  func(context.Context, string, string, *AuthIdentity) error
	DocumentLock                 func(context.Context, string, string, *AuthIdentity) (protocol.DocumentLockEnvelope, error)
	AcquireDocumentLock          func(context.Context, string, string, bool, *AuthIdentity) (protocol.DocumentLockEnvelope, error)
	ReleaseDocumentLock          func(context.Context, string, string, *AuthIdentity) error
	SchedulePublish              func(context.Context, string, string, time.Time, string, int, *AuthIdentity) (store.ScheduledPublication, error)
	ScheduleUnpublish            func(context.Context, string, string, time.Time, string, int, *AuthIdentity) (store.ScheduledPublication, error)
	ScheduledPublications        func(context.Context, string, string, *AuthIdentity) ([]store.ScheduledPublication, error)
	CancelScheduledPublication   func(context.Context, string, string, string, *AuthIdentity) error
	AllowAuthIPAttempt           func(context.Context, string, string, int, time.Duration) (bool, error)
	AllowAuthAttempt             func(context.Context, string, string, string, int, time.Duration) (bool, error)
	AllowedOrigins               []string
	AllowedRequestHeaders        []string
	AllowedHosts                 []string
	TrustedProxies               []netip.Prefix
	AuthRateLimit                int
	AuthRateWindow               time.Duration
	Audit                        func(AuditEvent)
	Ready                        func(context.Context) error
	Observe                      func(RequestObservation)
	RequestError                 func(RequestErrorEvent)
	RequestTimeout               time.Duration
	ContentSecurityPolicy        string
	DisableContentSecurityPolicy bool
	StrictTransportSecurity      string
	AcquireUpload                func(context.Context, string) (release func(), err error)
	Upload                       func(context.Context, string, UploadInput, *AuthIdentity, bool) (store.Document, error)
	UpdateUpload                 func(context.Context, string, string, UploadInput, *AuthIdentity, bool) (store.Document, error)
	RemoteUpload                 func(context.Context, string, string, UploadInput, *AuthIdentity) (store.Document, error)
	PreviewUpload                func(context.Context, string, string, string, *AuthIdentity) (io.ReadCloser, storage.Object, string, error)
	OpenUploadSource             func(context.Context, string, string, *AuthIdentity) (io.ReadCloser, storage.Object, error)
	Duplicate                    func(context.Context, string, string, store.Values, *AuthIdentity, LocaleOptions) (store.Document, error)
	OpenUpload                   func(context.Context, string, string, *AuthIdentity) (io.ReadCloser, storage.Object, error)
	CreateUploadGrants           func(context.Context, string, string, []protocol.UploadGrantRequestItem, time.Duration) ([]protocol.UploadGrant, error)
	OpenUploadGrant              func(context.Context, string, string, string) (io.ReadCloser, storage.Object, error)
	PluginEndpoints              []PluginEndpoint
	PluginTransports             []PluginEndpoint
	CustomEndpoints              []CustomEndpoint
}

type PreviewToken struct {
	Token      string
	Resource   string
	Slug       string
	DocumentID string
	ExpiresAt  time.Time
}

type EndpointContext struct {
	Writer           http.ResponseWriter
	Request          *http.Request
	RequestID        string
	ClientIP         string
	RouteParams      map[string]string
	Actor            *store.Document
	ActorCollection  schema.CollectionSlug
	AdmitAuthAttempt func(context.Context, string, string) error
	ReportError      func(error, string)
}

type EndpointHandler func(EndpointContext)

type PluginEndpoint struct {
	Method       string
	Path         string
	MaxBodyBytes int64
	Handler      EndpointHandler
}

type CustomEndpointScope uint8

const (
	CustomEndpointRoot CustomEndpointScope = iota
	CustomEndpointCollection
	CustomEndpointGlobal
)

type CustomEndpoint struct {
	Method       string
	Pattern      string
	Scope        CustomEndpointScope
	Resource     schema.CollectionSlug
	MaxBodyBytes int64
	Handler      EndpointHandler
}

type AuditEvent struct {
	Time            time.Time
	RequestID       string
	ClientIP        string
	Action          string
	Collection      string
	DocumentID      string
	ActorID         string
	ActorCollection schema.CollectionSlug
}

type RequestObservation struct {
	Time          time.Time
	RequestID     string
	Method        string
	Path          string
	Status        int
	ErrorCode     string
	ErrorReason   string
	ResponseBytes int64
	Duration      time.Duration
}

type RequestErrorEvent struct {
	Time      time.Time
	RequestID string
	Method    string
	Path      string
	Error     error
	Panic     bool
	Stack     string
}

type API struct {
	config Config
	// manifest is the shared, read-only presentation manifest.
	manifest         schema.Snapshot
	manifestEncoding struct {
		once  sync.Once
		value encodedAdminManifest
		err   error
	}
	collections      map[string]schema.Collection
	globals          map[string]schema.Global
	pluginEndpoints  map[string]PluginEndpoint
	pluginTransports map[string]PluginEndpoint
	customEndpoints  []CustomEndpoint
	allowedHosts     []allowedHost
	allowedHeaders   []string
	allowedHeaderSet map[string]struct{}
	restrictHosts    bool
}

func New(config Config) http.Handler {
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = 1 << 20
	}
	if config.AuthRateLimit <= 0 {
		config.AuthRateLimit = 10
	}
	if config.AuthRateWindow <= 0 {
		config.AuthRateWindow = time.Minute
	}
	if config.RequestTimeout == 0 {
		config.RequestTimeout = defaultRequestTimeout
	} else if config.RequestTimeout < 0 {
		config.RequestTimeout = 0
	}
	if config.ContentSecurityPolicy == "" {
		config.ContentSecurityPolicy = defaultAdminCSP
	}
	api := &API{config: config, collections: make(map[string]schema.Collection), globals: make(map[string]schema.Global), pluginEndpoints: make(map[string]PluginEndpoint), pluginTransports: make(map[string]PluginEndpoint), customEndpoints: append([]CustomEndpoint(nil), config.CustomEndpoints...), allowedHeaderSet: make(map[string]struct{}), restrictHosts: len(config.AllowedHosts) != 0}
	for _, header := range append(append([]string(nil), defaultCORSRequestHeaders...), config.AllowedRequestHeaders...) {
		header = http.CanonicalHeaderKey(strings.TrimSpace(header))
		if !validHeaderName(header) {
			continue
		}
		key := strings.ToLower(header)
		if _, exists := api.allowedHeaderSet[key]; exists {
			continue
		}
		api.allowedHeaderSet[key] = struct{}{}
		api.allowedHeaders = append(api.allowedHeaders, header)
	}
	sort.Strings(api.allowedHeaders)
	for _, configured := range config.AllowedHosts {
		if parsed, err := parseAllowedHost(configured); err == nil {
			api.allowedHosts = append(api.allowedHosts, parsed)
		}
	}
	snapshot := config.Snapshot
	if snapshot.Version == 0 {
		snapshot = config.Manifest.Snapshot()
	}
	api.manifest = snapshot
	for _, collection := range snapshot.Collections {
		api.collections[string(collection.Slug)] = collection
	}
	for _, global := range snapshot.Globals {
		api.globals[string(global.Slug)] = global
	}
	for _, endpoint := range config.PluginEndpoints {
		api.pluginEndpoints[endpoint.Method+" "+endpoint.Path] = endpoint
	}
	for _, endpoint := range config.PluginTransports {
		api.pluginTransports[endpoint.Method+" "+endpoint.Path] = endpoint
	}
	return http.HandlerFunc(api.serveHTTP)
}

func (api *API) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	requestID := newRequestID()
	started := time.Now()
	tracked := &statusWriter{ResponseWriter: writer, status: http.StatusOK, method: request.Method, path: request.URL.Path}
	writer = tracked
	api.securityHeaders(writer, request)
	if api.config.RequestTimeout > 0 {
		ctx, cancel := context.WithTimeout(request.Context(), api.config.RequestTimeout)
		defer cancel()
		request = request.WithContext(ctx)
	}
	defer func() {
		if api.config.Observe != nil {
			func() {
				defer func() { _ = recover() }()
				api.config.Observe(RequestObservation{Time: started.UTC(), RequestID: requestID, Method: request.Method, Path: request.URL.Path, Status: tracked.status, ErrorCode: tracked.errorCode, ErrorReason: tracked.reason(), ResponseBytes: tracked.bytes, Duration: time.Since(started)})
			}()
		}
	}()
	defer func() {
		if recovered := recover(); recovered != nil {
			failure := errors.New("panic recovered in request handler")
			api.reportRequestError(request, requestID, failure, true, string(debug.Stack()))
			tracked.errorCode = string(protocol.ErrorInternal)
			if !tracked.wroteHeader {
				writer.Header().Set("Content-Type", "application/json")
				writeJSON(writer, http.StatusInternalServerError, protocol.ErrorEnvelope{Error: protocol.ErrorPayload{
					Code: protocol.ErrorInternal, Status: http.StatusInternalServerError, Message: "internal server error", RequestID: requestID, Issues: []protocol.ValidationIssue{},
				}})
			}
		}
	}()
	writer.Header().Set("X-Request-ID", requestID)
	if !api.hostAllowed(request.Host) {
		writer.Header().Set("Content-Type", "application/json")
		api.writeError(writer, requestID, &operationengine.Error{Code: "host_denied", Status: 400, Message: "request host is not allowed"})
		return
	}
	if api.cors(writer, request, requestID) {
		return
	}
	request, credentialError := api.attachCredentialState(request)
	if credentialError != nil {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Cache-Control", "no-store")
		api.writeError(writer, requestID, credentialError)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/admin") && api.config.AdminAssets != nil {
		request = request.WithContext(context.WithValue(request.Context(), adminPreparedRequestIDContextKey{}, requestID))
		api.admin(writer, request, requestID)
		return
	}
	if request.URL.Path == "/healthz" {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method != http.MethodGet {
			api.methodNotAllowed(writer, requestID, http.MethodGet)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if request.URL.Path == "/readyz" {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method != http.MethodGet {
			api.methodNotAllowed(writer, requestID, http.MethodGet)
			return
		}
		if api.config.Ready != nil {
			if err := api.config.Ready(request.Context()); err != nil {
				tracked.errorCode = "readiness_failed"
				api.reportRequestError(request, requestID, err, false, "")
				writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
				return
			}
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
		return
	}
	if scope, resource, scoped := api.customEndpointScope(request.URL.Path); scoped {
		if api.invokeCustomEndpoint(writer, request, requestID, scope, resource) {
			return
		}
	} else if api.invokeCustomEndpoint(writer, request, requestID, CustomEndpointRoot, "") {
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	if request.URL.Path == "/api/schema" {
		if request.Method != http.MethodGet {
			api.methodNotAllowed(writer, requestID, http.MethodGet)
			return
		}
		snapshot, _, err := api.presentedManifest(request.Context(), api.optionalIdentity(request))
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.SchemaEnvelope{Schema: snapshot})
		return
	}
	if endpoint, exists := api.pluginTransports[request.Method+" "+request.URL.Path]; exists {
		actor, collection := api.pluginActor(request)
		api.invokeEndpoint(writer, request, requestID, endpoint.MaxBodyBytes, endpoint.Handler, nil, actor, collection)
		return
	}
	var transportMethods []string
	for _, endpoint := range api.pluginTransports {
		if endpoint.Path == request.URL.Path {
			transportMethods = append(transportMethods, endpoint.Method)
		}
	}
	if len(transportMethods) != 0 {
		sort.Strings(transportMethods)
		api.methodNotAllowed(writer, requestID, transportMethods...)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/plugins/") {
		api.pluginEndpoint(writer, request, requestID)
		return
	}
	if request.URL.Path == "/api/preferences" || request.URL.Path == "/api/preferences/" {
		api.resetPreferences(writer, request, requestID)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/preferences/") {
		api.preference(writer, request, requestID)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/auth/") {
		api.auth(writer, request, requestID)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/uploads/") {
		api.upload(writer, request, requestID)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/access/") {
		api.accessCapabilities(writer, request, requestID)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/preview/") {
		api.preview(writer, request, requestID)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/collections/") {
		api.collection(writer, request, requestID)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/admin/collection-list/") {
		request = request.WithContext(context.WithValue(request.Context(), adminPreparedRequestIDContextKey{}, requestID))
		api.adminCollectionList(writer, request, requestID)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/admin/loaders/") {
		request = request.WithContext(context.WithValue(request.Context(), adminPreparedRequestIDContextKey{}, requestID))
		api.adminLoader(writer, request, requestID)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/globals/") {
		api.global(writer, request, requestID)
		return
	}
	api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "route was not found"})
}

func (api *API) customEndpointScope(path string) (CustomEndpointScope, schema.CollectionSlug, bool) {
	for _, candidate := range []struct {
		prefix string
		scope  CustomEndpointScope
	}{
		{prefix: "/api/collections/", scope: CustomEndpointCollection},
		{prefix: "/api/globals/", scope: CustomEndpointGlobal},
	} {
		if !strings.HasPrefix(path, candidate.prefix) {
			continue
		}
		remainder := strings.TrimPrefix(path, candidate.prefix)
		resource, _, _ := strings.Cut(remainder, "/")
		if resource == "" {
			return CustomEndpointRoot, "", false
		}
		if candidate.scope == CustomEndpointCollection {
			if _, exists := api.collections[resource]; !exists {
				return CustomEndpointRoot, "", false
			}
		} else if _, exists := api.globals[resource]; !exists {
			return CustomEndpointRoot, "", false
		}
		return candidate.scope, schema.CollectionSlug(resource), true
	}
	return CustomEndpointRoot, "", false
}

func (api *API) invokeCustomEndpoint(writer http.ResponseWriter, request *http.Request, requestID string, scope CustomEndpointScope, resource schema.CollectionSlug) bool {
	for _, endpoint := range api.customEndpoints {
		if endpoint.Scope != scope || endpoint.Resource != resource || endpoint.Method != request.Method {
			continue
		}
		params, matched := matchEndpointPattern(endpoint.Pattern, request.URL)
		if !matched {
			continue
		}
		for name, value := range params {
			request.SetPathValue(name, value)
		}
		identity := api.optionalIdentity(request)
		actor, collection := identityActor(identity), identityCollection(identity)
		api.invokeEndpoint(writer, request, requestID, endpoint.MaxBodyBytes, endpoint.Handler, params, actor, collection)
		return true
	}
	return false
}

func (api *API) hasMatchingCustomEndpoint(request *http.Request) bool {
	scope, resource, scoped := api.customEndpointScope(request.URL.Path)
	if !scoped {
		scope = CustomEndpointRoot
		resource = ""
	}
	for _, endpoint := range api.customEndpoints {
		if endpoint.Scope != scope || endpoint.Resource != resource || endpoint.Method != request.Method {
			continue
		}
		if _, matched := matchEndpointPattern(endpoint.Pattern, request.URL); matched {
			return true
		}
	}
	return false
}

func matchEndpointPattern(pattern string, requestURL *url.URL) (map[string]string, bool) {
	patternSegments := splitEndpointPath(pattern)
	requestPath := requestURL.EscapedPath()
	if requestPath != "/" && strings.HasSuffix(requestPath, "/") {
		requestPath = strings.TrimSuffix(requestPath, "/")
	}
	requestSegments := splitEndpointPath(requestPath)
	if len(patternSegments) != len(requestSegments) {
		return nil, false
	}
	params := make(map[string]string)
	for index, patternSegment := range patternSegments {
		requestSegment, err := url.PathUnescape(requestSegments[index])
		if err != nil || strings.ContainsAny(requestSegment, "/\\") {
			return nil, false
		}
		if strings.HasPrefix(patternSegment, ":") {
			if requestSegment == "" {
				return nil, false
			}
			params[strings.TrimPrefix(patternSegment, ":")] = requestSegment
			continue
		}
		if requestSegment != patternSegment {
			return nil, false
		}
	}
	return params, true
}

func splitEndpointPath(path string) []string {
	if path == "/" || path == "" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

func decodeEscapedPathSegments(path string) ([]string, error) {
	rawSegments := strings.Split(path, "/")
	segments := make([]string, len(rawSegments))
	for index, raw := range rawSegments {
		segment, err := url.PathUnescape(raw)
		if err != nil {
			return nil, err
		}
		segments[index] = segment
	}
	return segments, nil
}

func (api *API) preview(writer http.ResponseWriter, request *http.Request, requestID string) {
	remainder := strings.TrimPrefix(request.URL.EscapedPath(), "/api/preview/")
	if remainder == "token/revoke" {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		if api.config.RevokePreviewToken == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "preview token service is unavailable"})
			return
		}
		var input struct {
			Token string `json:"token"`
		}
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		identity := api.optionalIdentity(request)
		if err := api.config.RevokePreviewToken(request.Context(), input.Token, identity); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writer.Header().Set("Cache-Control", "private, no-store")
		writeJSON(writer, http.StatusOK, protocol.AuthActionEnvelope{Success: true})
		return
	}
	segments, pathError := decodeEscapedPathSegments(remainder)
	if pathError != nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "malformed preview path", Cause: pathError})
		return
	}
	resource := ""
	slug := ""
	documentID := ""
	tokenRoute := false
	switch {
	case len(segments) == 3 && segments[0] == "collections" && segments[1] != "" && segments[2] != "":
		resource, slug, documentID = "collection", segments[1], segments[2]
	case len(segments) == 4 && segments[0] == "collections" && segments[1] != "" && segments[2] != "" && segments[3] == "token":
		resource, slug, documentID, tokenRoute = "collection", segments[1], segments[2], true
	case len(segments) == 2 && segments[0] == "globals" && segments[1] != "":
		resource, slug, documentID = "global", segments[1], segments[1]
	case len(segments) == 3 && segments[0] == "globals" && segments[1] != "" && segments[2] == "token":
		resource, slug, documentID, tokenRoute = "global", segments[1], segments[1], true
	default:
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "malformed preview path"})
		return
	}
	if tokenRoute {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		previewEpoch := uint64(0)
		if api.config.PreviewLifecycleEpoch != nil {
			previewEpoch = api.config.PreviewLifecycleEpoch()
		}
		identity := api.optionalIdentity(request)
		if identity != nil {
			identity.PreviewEpoch = previewEpoch
		}
		var actor *store.Document
		if identity != nil {
			actor = &identity.Actor
		}
		var token PreviewToken
		var err error
		if resource == "collection" && api.config.CreateCollectionPreviewToken != nil {
			token, err = api.config.CreateCollectionPreviewToken(request.Context(), slug, documentID, identity)
		} else if resource == "global" && api.config.CreateGlobalPreviewToken != nil {
			token, err = api.config.CreateGlobalPreviewToken(request.Context(), slug, identity)
		} else {
			err = &operationengine.Error{Code: "not_found", Status: 404, Message: "preview token service is unavailable"}
		}
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writer.Header().Set("Cache-Control", "private, no-store")
		writeJSON(writer, http.StatusCreated, protocol.PreviewTokenEnvelope{PreviewToken: protocol.PreviewToken{
			Token: token.Token, Resource: token.Resource, Slug: token.Slug,
			DocumentID: token.DocumentID, ExpiresAt: token.ExpiresAt.Format(time.RFC3339Nano),
		}})
		api.audit(request, requestID, actor, "create-preview-token", slug, documentID)
		return
	}
	if request.Method != http.MethodGet {
		api.methodNotAllowed(writer, requestID, http.MethodGet)
		return
	}
	credential := bearerCredential(request)
	var document store.Document
	var err error
	if resource == "collection" && api.config.FindCollectionPreview != nil {
		document, err = api.config.FindCollectionPreview(request.Context(), credential, slug, documentID)
	} else if resource == "global" && api.config.FindGlobalPreview != nil {
		document, err = api.config.FindGlobalPreview(request.Context(), credential, slug)
	} else {
		err = &operationengine.Error{Code: "not_found", Status: 404, Message: "preview service is unavailable"}
	}
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(document)})
}

func bearerCredential(request *http.Request) string {
	scheme, credential, found := strings.Cut(request.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(credential)
}

func (api *API) resetPreferences(writer http.ResponseWriter, request *http.Request, requestID string) {
	if request.Method != http.MethodDelete {
		api.methodNotAllowed(writer, requestID, http.MethodDelete)
		return
	}
	identity := api.optionalIdentity(request)
	if api.config.ResetPreferences == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "preference reset is not available"})
		return
	}
	if err := api.config.ResetPreferences(request.Context(), identity); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusOK, protocol.AuthActionEnvelope{Success: true})
	actor := identityActor(identity)
	actorID := ""
	if actor != nil {
		actorID = actor.ID
	}
	api.audit(request, requestID, actor, "preference_reset", "", actorID, identityCollection(identity))
}

func (api *API) accessCapabilities(writer http.ResponseWriter, request *http.Request, requestID string) {
	if request.Method != http.MethodPost {
		api.methodNotAllowed(writer, requestID, http.MethodPost)
		return
	}
	remainder := strings.TrimPrefix(request.URL.Path, "/api/access/")
	segments := strings.Split(remainder, "/")
	if len(segments) == 3 && segments[0] == "collections" && segments[1] != "" && segments[2] == "selection" {
		api.filteredCollectionSelection(writer, request, requestID, segments[1])
		return
	}
	if len(segments) != 2 || segments[1] == "" || (segments[0] != "collections" && segments[0] != "globals") {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "malformed access capability path"})
		return
	}
	var input struct {
		ID    string       `json:"id"`
		Data  store.Values `json:"data"`
		Trash bool         `json:"trash"`
	}
	if err := api.decodeJSON(writer, request, &input); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	collectionKey := segments[1]
	if segments[0] == "globals" {
		if _, exists := api.globals[segments[1]]; !exists {
			api.writeError(writer, requestID, &operationengine.Error{Code: "unknown_global", Status: 404, Message: fmt.Sprintf("global %q was not found", segments[1])})
			return
		}
		if input.ID != "" || input.Trash {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "global capabilities do not accept document or trash options"})
			return
		}
		collectionKey, input.ID = "global:"+segments[1], segments[1]
	} else if _, exists := api.collections[segments[1]]; !exists {
		api.writeError(writer, requestID, &operationengine.Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", segments[1])})
		return
	}
	localeOptions, err := decodeLocaleQuery(request.URL.Query())
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	identity := api.optionalIdentity(request)
	capabilities, err := api.readDocumentAccess(request.Context(), collectionKey, input.ID, input.Data, input.Trash, identity, localeOptions)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusOK, capabilities)
}

func (api *API) filteredCollectionSelection(writer http.ResponseWriter, request *http.Request, requestID, slug string) {
	collection, exists := api.collections[slug]
	if !exists {
		api.writeError(writer, requestID, &operationengine.Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", slug)})
		return
	}
	var input protocol.CollectionSelectionInput
	if err := api.decodeJSON(writer, request, &input); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	var filter query.Expression
	var err error
	if len(input.Where) != 0 {
		filter, err = decodeWhere(input.Where, collection)
		if err != nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid where query", Cause: err})
			return
		}
	}
	localeOptions, err := decodeLocaleQuery(request.URL.Query())
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	identity := api.optionalIdentity(request)
	selection, err := api.config.Engine.ResolveFilteredSelection(request.Context(), operationengine.FilteredSelectionRequest{
		Collection: slug, Filter: filter, Actor: identityActor(identity), ActorCollection: identityCollection(identity), TrashOnly: input.Trash,
		Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
		DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales,
	})
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	items := make([]protocol.CollectionSelectionItem, len(selection.Items))
	for index, item := range selection.Items {
		items[index] = protocol.CollectionSelectionItem{ID: item.ID, Access: accessCapabilitiesJSON(item.Capabilities)}
	}
	writeJSON(writer, http.StatusOK, protocol.CollectionSelectionEnvelope{Items: items, TotalDocs: len(items)})
}

func accessCapabilitiesJSON(capabilities operationengine.AccessCapabilities) protocol.AccessCapabilitiesEnvelope {
	fields := make(map[string]protocol.FieldCapabilities, len(capabilities.Fields))
	for path, field := range capabilities.Fields {
		fields[path] = protocol.FieldCapabilities{Read: field.Read, Create: field.Create, Update: field.Update}
	}
	var blockFields map[string]map[string]protocol.FieldCapabilities
	for slug, definition := range capabilities.BlockFields {
		if blockFields == nil {
			blockFields = make(map[string]map[string]protocol.FieldCapabilities, len(capabilities.BlockFields))
		}
		blockFields[slug] = make(map[string]protocol.FieldCapabilities, len(definition))
		for path, field := range definition {
			blockFields[slug][path] = protocol.FieldCapabilities{Read: field.Read, Create: field.Create, Update: field.Update}
		}
	}
	operations := capabilities.Operations
	return protocol.AccessCapabilitiesEnvelope{
		Operations: protocol.OperationCapabilities{
			Admin: operations.Admin, Create: operations.Create, Read: operations.Read,
			ReadVersions: operations.ReadVersions, Update: operations.Update,
			Delete: operations.Delete, Duplicate: operations.Duplicate,
			Publish: operations.Publish, Unpublish: operations.Unpublish,
			RestoreDeleted: operations.RestoreDeleted, DeletePermanent: operations.DeletePermanent,
			SelectAll: operations.SelectAll,
		},
		Fields:      fields,
		BlockFields: blockFields,
	}
}

func (api *API) preference(writer http.ResponseWriter, request *http.Request, requestID string) {
	key := strings.TrimPrefix(request.URL.Path, "/api/preferences/")
	if key == "" || strings.Contains(key, "/") {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "malformed preference path"})
		return
	}
	identity := api.optionalIdentity(request)
	switch request.Method {
	case http.MethodGet:
		value, err := api.config.GetPreference(request.Context(), identity, key)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.PreferenceEnvelope[json.RawMessage]{Value: value})
	case http.MethodPut:
		var input protocol.PreferenceEnvelope[json.RawMessage]
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		value, err := api.config.SetPreference(request.Context(), identity, key, input.Value)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.PreferenceEnvelope[json.RawMessage]{Value: value})
	case http.MethodDelete:
		if err := api.config.DeletePreference(request.Context(), identity, key); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DeleteEnvelope{ID: key, Deleted: true})
	default:
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (api *API) global(writer http.ResponseWriter, request *http.Request, requestID string) {
	remainder := strings.TrimPrefix(request.URL.Path, "/api/globals/")
	segments := strings.Split(remainder, "/")
	if len(segments) == 0 || len(segments) > 3 || segments[0] == "" {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "malformed global path"})
		return
	}
	global, exists := api.globals[segments[0]]
	if !exists {
		api.writeError(writer, requestID, &operationengine.Error{Code: "unknown_global", Status: 404, Message: fmt.Sprintf("global %q was not found", segments[0])})
		return
	}
	identity := api.optionalIdentity(request)
	actor := identityActor(identity)
	actorCollection := identityCollection(identity)
	key, slug := "global:"+segments[0], segments[0]
	if len(segments) == 2 && segments[1] == "validate" {
		api.liveValidation(writer, request, requestID, key, slug)
		return
	}
	if len(segments) == 1 {
		switch request.Method {
		case http.MethodGet:
			options, err := decodeListQuery(request.URL.Query(), global, false)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			document, err := api.readDocument(request.Context(), key, slug, identity, options)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: document})
		case http.MethodPatch:
			values, err := api.decodeValues(writer, request)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			localeOptions, err := decodeLocaleQuery(request.URL.Query())
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			draft, err := decodeDraftQuery(request)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			operationRequest := operationengine.Request{Operation: operation.Update, Collection: key, ID: slug, Data: values, Actor: actor, ActorCollection: actorCollection, ExpectedRevision: revisionHeader(request), Draft: draft}
			localeOptions.apply(&operationRequest)
			result, err := api.config.Engine.Execute(request.Context(), operationRequest)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(*result.Document)})
			api.audit(request, requestID, actor, "update-global", slug, slug)
		default:
			api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodPatch)
		}
		return
	}
	if len(segments) == 2 && segments[1] == "copy-locale" {
		api.copyLocale(writer, request, requestID, key, slug, actor, actorCollection, "copy-global-locale")
		return
	}
	if global.Versions == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "global version route was not found"})
		return
	}
	action := segments[1]
	switch action {
	case "versions":
		if request.Method != http.MethodGet || len(segments) < 2 || len(segments) > 3 {
			api.methodNotAllowed(writer, requestID, http.MethodGet)
			return
		}
		localeOptions, err := decodeLocaleQuery(request.URL.Query())
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		versionLocaleOptions := operationengine.LocalizationOptions{
			Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
			DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales, ActorCollection: actorCollection,
		}
		if len(segments) == 3 {
			if segments[2] == "count" {
				count, err := api.config.Engine.CountVersions(request.Context(), key, slug, actor, versionLocaleOptions)
				if err != nil {
					api.writeError(writer, requestID, err)
					return
				}
				writeJSON(writer, http.StatusOK, protocol.CountEnvelope{TotalDocs: count})
				return
			}
			revision, err := strconv.Atoi(segments[2])
			if err != nil || revision < 1 {
				api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "version revision must be a positive integer"})
				return
			}
			version, err := api.config.Engine.Version(request.Context(), key, slug, revision, actor, versionLocaleOptions)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			writeJSON(writer, http.StatusOK, map[string]any{"version": versionJSON(version)})
			return
		}
		versions, err := api.config.Engine.Versions(request.Context(), key, slug, actor, versionLocaleOptions)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"versions": versionsJSON(versions)})
	case "publish", "unpublish", "discard-draft":
		if request.Method != http.MethodPost || len(segments) != 2 {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		kind := operation.Publish
		if action == "unpublish" {
			kind = operation.Unpublish
		} else if action == "discard-draft" {
			kind = operation.DiscardDraft
		}
		values, err := api.decodeOptionalValues(writer, request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		localeOptions, err := decodeLocaleQuery(request.URL.Query())
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		operationRequest := operationengine.Request{Operation: kind, Collection: key, ID: slug, Data: values, Actor: actor, ActorCollection: actorCollection, ExpectedRevision: revisionHeader(request)}
		localeOptions.apply(&operationRequest)
		result, err := api.config.Engine.Execute(request.Context(), operationRequest)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(*result.Document)})
		api.audit(request, requestID, actor, action+"-global", slug, slug)
	case "restore":
		if request.Method != http.MethodPost || len(segments) != 3 {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		revision, err := strconv.Atoi(segments[2])
		if err != nil || revision < 1 {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "restore revision must be a positive integer"})
			return
		}
		draft, queryError := restoreDraftQuery(request)
		if queryError != nil {
			api.writeError(writer, requestID, queryError)
			return
		}
		localeOptions, localeError := decodeLocaleQuery(request.URL.Query())
		if localeError != nil {
			api.writeError(writer, requestID, localeError)
			return
		}
		result, err := api.config.Engine.Restore(request.Context(), key, slug, revision, revisionHeader(request), draft, actor, operationengine.LocalizationOptions{
			Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
			DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales, ActorCollection: actorCollection,
		})
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(*result.Document)})
		action := "restore-global"
		if draft {
			action = "restore-global-as-draft"
		}
		api.audit(request, requestID, actor, action, slug, slug)
	default:
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "global route was not found"})
	}
}

func (api *API) pluginEndpoint(writer http.ResponseWriter, request *http.Request, requestID string) {
	identity := request.Method + " " + request.URL.Path
	if endpoint, exists := api.pluginEndpoints[identity]; exists {
		actor, collection := api.pluginActor(request)
		api.invokeEndpoint(writer, request, requestID, endpoint.MaxBodyBytes, endpoint.Handler, nil, actor, collection)
		return
	}
	var allowed []string
	for _, endpoint := range api.pluginEndpoints {
		if endpoint.Path == request.URL.Path {
			allowed = append(allowed, endpoint.Method)
		}
	}
	if len(allowed) != 0 {
		api.methodNotAllowed(writer, requestID, allowed...)
		return
	}
	api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "plugin endpoint was not found"})
}

func (api *API) pluginActor(request *http.Request) (*store.Document, schema.CollectionSlug) {
	identity := api.optionalIdentity(request)
	if identity == nil {
		return nil, ""
	}
	actor := store.CloneDocument(identity.Actor)
	return &actor, identity.Collection
}

func (api *API) invokeEndpoint(writer http.ResponseWriter, request *http.Request, requestID string, maxBodyBytes int64, handler EndpointHandler, params map[string]string, actor *store.Document, collection schema.CollectionSlug) {
	limit := maxBodyBytes
	if limit == 0 {
		limit = api.config.MaxBodyBytes
	}
	if limit > 0 && request.Body != nil {
		request.Body = http.MaxBytesReader(writer, request.Body, limit)
	}
	report := func(err error, code string) {
		if tracked, ok := writer.(*statusWriter); ok && validErrorCode(code) {
			tracked.errorCode = code
		}
		if err != nil {
			api.reportRequestError(request, requestID, err, false, "")
		}
	}
	admitAuthAttempt := func(ctx context.Context, scope, identity string) error {
		return api.admitAuthAttempt(ctx, api.clientIP(request), scope, identity)
	}
	handler(EndpointContext{Writer: writer, Request: request, RequestID: requestID, ClientIP: api.clientIP(request), RouteParams: params, Actor: actor, ActorCollection: collection, AdmitAuthAttempt: admitAuthAttempt, ReportError: report})
}

func (api *API) admitAuthAttempt(ctx context.Context, clientIP, scope, identity string) error {
	allowed := true
	var err error
	if api.config.AllowAuthIPAttempt != nil {
		allowed, err = api.config.AllowAuthIPAttempt(ctx, clientIP, scope, api.config.AuthRateLimit, api.config.AuthRateWindow)
	}
	if err == nil && allowed && api.config.AllowAuthAttempt != nil {
		allowed, err = api.config.AllowAuthAttempt(ctx, clientIP, scope, identity, api.config.AuthRateLimit, api.config.AuthRateWindow)
	}
	if err != nil {
		return err
	}
	if !allowed {
		return &operationengine.Error{Code: "rate_limited", Status: http.StatusTooManyRequests, Message: "too many requests; try again later"}
	}
	return nil
}

func validErrorCode(code string) bool {
	if code == "" || len(code) > 64 {
		return false
	}
	for index, character := range code {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || index > 0 && (character == '_' || character == '-') {
			continue
		}
		return false
	}
	return true
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	errorCode   string
	errorReason string
	wroteHeader bool
	method      string
	path        string
}

func (writer *statusWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	writer.wroteHeader = true
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusWriter) Write(encoded []byte) (int, error) {
	if !writer.wroteHeader {
		writer.wroteHeader = true
		writer.status = http.StatusOK
	}
	written, err := writer.ResponseWriter.Write(encoded)
	writer.bytes += int64(written)
	return written, err
}

func (writer *statusWriter) FlushError() error {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(writer.ResponseWriter).Flush()
}

func (writer *statusWriter) Flush() {
	_ = writer.FlushError()
}

func (writer *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	connection, readWriter, err := http.NewResponseController(writer.ResponseWriter).Hijack()
	if err == nil && !writer.wroteHeader {
		writer.wroteHeader = true
		writer.status = http.StatusOK
	}
	return connection, readWriter, err
}

func (writer *statusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (api *API) admin(writer http.ResponseWriter, request *http.Request, requestID string) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/admin")
	assetPath := strings.TrimPrefix(path, "/")
	if assetPath != "" {
		if info, err := fs.Stat(api.config.AdminAssets, assetPath); err == nil && !info.IsDir() && assetPath != "index.html" {
			if strings.HasPrefix(assetPath, "assets/") {
				writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				writer.Header().Set("Cache-Control", "public, max-age=3600")
			}
			request.URL.Path = path
			http.FileServer(http.FS(api.config.AdminAssets)).ServeHTTP(writer, request)
			return
		}
	}
	index, err := fs.ReadFile(api.config.AdminAssets, "index.html")
	if err != nil {
		http.Error(writer, "admin assets are unavailable", http.StatusInternalServerError)
		return
	}
	setAdminPreparedHeaders(writer.Header())
	if request.Method == http.MethodHead {
		// HEAD proves route availability without resolving a session, running hooks, or reading
		// route data. Keep it above every bootstrap operation.
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		return
	}
	metadata, metadataError := api.adminBootstrapMetadata()
	wantsState := acceptsMediaType(request.Header.Get("Accept"), protocol.AdminPreparedRouteStateMediaType)
	if metadataError != nil {
		state := api.prepareAdminStateSafely(request, requestID, "", func() protocol.AdminPreparedRouteStateV1 {
			return api.prepareAdminFallbackState(request, "metadata_unavailable", "The admin will load using its browser route controller.")
		})
		if wantsState {
			writer.Header().Set("Content-Type", protocol.AdminPreparedRouteStateMediaType)
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(marshalBoundedAdminState(adminNavigationState(state, request)))
			return
		}
		prefix, suffix, split := adminHTMLPrefix(index, adminBootstrapMetadata{}, state)
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		if !split {
			_, _ = writer.Write(index)
			return
		}
		_, _ = writer.Write(prefix)
		_, _ = writer.Write([]byte("<template id=\"ridu-admin-initial-state\">"))
		_, _ = writer.Write(marshalBoundedAdminState(state))
		_, _ = writer.Write([]byte("</template>\n\t\t"))
		_, _ = writer.Write(suffix)
		return
	}
	if wantsState {
		// SPA navigation uses the same URL and preparation path as a document request; content
		// negotiation changes only the envelope that carries the result.
		writer.Header().Set("Content-Type", protocol.AdminPreparedRouteStateMediaType)
		state := api.prepareAdminStateSafely(request, requestID, metadata.BuildID, func() protocol.AdminPreparedRouteStateV1 {
			return api.prepareAdminState(request, metadata, true)
		})
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(marshalBoundedAdminState(adminNavigationState(state, request)))
		return
	}
	state, runtime, identity, classification := api.prepareAdminStateBaseSafely(request, requestID, metadata)
	prefix, suffix, split := adminHTMLPrefix(index, metadata, state)
	if !split {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(index)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(prefix)
	// Runtime/session resolution has already selected the theme and route module groups. Flush
	// those preload hints while route data and custom loaders finish through their owning APIs.
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
	if state.Outcome == protocol.AdminPreparedRoutePrepared && state.Route != nil && runtime != nil {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					api.reportRequestError(request, requestID, errors.New("panic recovered while preparing an admin route"), true, string(debug.Stack()))
					state.Outcome = protocol.AdminPreparedRouteFallback
					state.Route = nil
					state.Loaders = nil
					state.ModuleGroups = []string{"entry"}
					state.Diagnostic = &protocol.AdminPreparedRouteDiagnosticV1{Code: "route_prepare_failed", Message: "This route will load using its browser controller."}
				}
			}()
			api.completeAdminPreparedRoute(&state, request, runtime, identity, classification)
		}()
	}
	encoded := marshalBoundedAdminState(state)
	// The template is inert data, not server-rendered UI. It lands before the entry script in the
	// suffix, so Svelte can validate and stage the complete snapshot before revealing the route.
	_, _ = writer.Write([]byte("<template id=\"ridu-admin-initial-state\">"))
	_, _ = writer.Write(encoded)
	_, _ = writer.Write([]byte("</template>\n\t\t"))
	_, _ = writer.Write(suffix)
}

func acceptsMediaType(header, expected string) bool {
	for _, candidate := range strings.Split(header, ",") {
		mediaType, parameters, err := mime.ParseMediaType(strings.TrimSpace(candidate))
		quality := 1.0
		if encoded := parameters["q"]; encoded != "" {
			quality, err = strconv.ParseFloat(encoded, 64)
		}
		if err == nil && quality > 0 && strings.EqualFold(mediaType, expected) {
			return true
		}
	}
	return false
}

func (api *API) collection(writer http.ResponseWriter, request *http.Request, requestID string) {
	remainder := strings.TrimPrefix(request.URL.EscapedPath(), "/api/collections/")
	segments, pathError := decodeEscapedPathSegments(remainder)
	if pathError != nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "malformed collection path", Cause: pathError})
		return
	}
	if len(segments) > 4 || len(segments) == 0 || segments[0] == "" || len(segments) >= 2 && segments[1] == "" {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "malformed collection path"})
		return
	}
	collection, exists := api.collections[segments[0]]
	if !exists {
		api.writeError(writer, requestID, &operationengine.Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", segments[0])})
		return
	}
	if len(segments) == 2 && segments[1] == "validate" {
		api.liveValidation(writer, request, requestID, segments[0], "")
		return
	}
	identity := api.optionalIdentity(request)
	actor := identityActor(identity)
	actorCollection := identityCollection(identity)
	if len(segments) >= 3 {
		api.documentAction(writer, request, requestID, collection, segments, actor, identity)
		return
	}
	if len(segments) == 1 {
		switch request.Method {
		case http.MethodGet:
			options, err := decodeListQuery(request.URL.Query(), collection, true)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			page, err := api.readCollectionPage(request.Context(), segments[0], identity, options)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			if options.includeAccess {
				writeJSON(writer, http.StatusOK, page)
			} else {
				writeJSON(writer, http.StatusOK, protocol.PageEnvelope[json.RawMessage]{Docs: page.Docs, Pagination: page.Pagination})
			}
		case http.MethodPost:
			if collection.Auth != nil {
				api.authCollectionCreateError(writer, requestID, collection)
				return
			}
			if collection.Upload != nil && strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/form-data") {
				api.saveUpload(writer, request, requestID, collection, "", identity)
				return
			}
			if collection.Upload != nil {
				api.writeError(writer, requestID, &operationengine.Error{Code: "bad_operation", Status: 400, Message: "upload collections require multipart or remote upload creation"})
				return
			}
			values, err := api.decodeValues(writer, request)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			localeOptions, err := decodeLocaleQuery(request.URL.Query())
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			draft, err := decodeDraftQuery(request)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			operationRequest := operationengine.Request{Operation: operation.Create, Collection: segments[0], Data: values, Actor: actor, ActorCollection: actorCollection, Draft: draft}
			localeOptions.apply(&operationRequest)
			result, err := api.config.Engine.Execute(request.Context(), operationRequest)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			writeJSON(writer, http.StatusCreated, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(*result.Document)})
			api.audit(request, requestID, actor, "create", segments[0], result.Document.ID)
		case http.MethodDelete:
			trashValues := request.URL.Query()["trash"]
			if len(trashValues) != 1 || trashValues[0] != "true" {
				api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "collection-wide delete requires trash=true"})
				return
			}
			api.emptyCollectionTrash(writer, request, requestID, collection, identity)
		default:
			api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodPost, http.MethodDelete)
		}
		return
	}
	if segments[1] == "count" && request.Method == http.MethodGet {
		options, err := decodeListQuery(request.URL.Query(), collection, false)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		count, err := api.readCollectionCount(request.Context(), segments[0], identity, options)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.CountEnvelope{TotalDocs: count})
		return
	}
	if segments[1] == "bulk" && request.Method == http.MethodPost {
		api.bulkCollection(writer, request, requestID, collection, identity)
		return
	}
	if segments[1] == "remote-upload" && request.Method == http.MethodPost {
		api.createRemoteUpload(writer, request, requestID, collection, identity)
		return
	}
	if segments[1] == "upload-preview" && request.Method == http.MethodPost {
		api.previewUpload(writer, request, requestID, collection, identity)
		return
	}
	id := segments[1]
	switch request.Method {
	case http.MethodGet:
		options, err := decodeListQuery(request.URL.Query(), collection, false)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		document, err := api.readDocument(request.Context(), segments[0], id, identity, options)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: document})
		api.audit(request, requestID, actor, "read", segments[0], id, actorCollection)
	case http.MethodPatch:
		values, err := api.decodeValues(writer, request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		localeOptions, err := decodeLocaleQuery(request.URL.Query())
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		draft, err := decodeDraftQuery(request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		operationRequest := operationengine.Request{Operation: operation.Update, Collection: segments[0], ID: id, Data: values, Actor: actor, ActorCollection: actorCollection, ExpectedRevision: revisionHeader(request), Draft: draft}
		localeOptions.apply(&operationRequest)
		result, err := api.config.Engine.Execute(request.Context(), operationRequest)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(*result.Document)})
	case http.MethodDelete:
		localeOptions, err := decodeLocaleQuery(request.URL.Query())
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		operationRequest := operationengine.Request{Operation: operation.Delete, Collection: segments[0], ID: id, Actor: actor, ActorCollection: actorCollection}
		localeOptions.apply(&operationRequest)
		result, err := api.config.Engine.Execute(request.Context(), operationRequest)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DeleteEnvelope{ID: result.Document.ID, Deleted: true})
		api.audit(request, requestID, actor, "delete", segments[0], result.Document.ID)
	default:
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodPatch, http.MethodDelete)
	}
}

func (api *API) emptyCollectionTrash(writer http.ResponseWriter, request *http.Request, requestID string, collection schema.Collection, identity *AuthIdentity) {
	actor := identityActor(identity)
	actorCollection := identityCollection(identity)
	if request.Method != http.MethodDelete {
		api.methodNotAllowed(writer, requestID, http.MethodDelete)
		return
	}
	if !collection.Capabilities.Trash {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_operation", Status: 400, Message: "collection does not support trash"})
		return
	}
	localeOptions, err := decodeLocaleQuery(request.URL.Query())
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	result, err := api.config.Engine.Execute(request.Context(), operationengine.Request{
		Operation: operation.Read, Collection: string(collection.Slug), Page: 1, Limit: 100, SkipTotal: true,
		Actor: actor, ActorCollection: actorCollection, TrashOnly: true, Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
		DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales,
	})
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	if result.Page.HasNextPage {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "empty trash supports at most 100 documents per atomic operation"})
		return
	}
	if len(result.Page.Documents) == 0 {
		writeJSON(writer, http.StatusOK, protocol.BulkEnvelope[json.RawMessage]{Docs: []json.RawMessage{}})
		return
	}
	requests := make([]operationengine.Request, len(result.Page.Documents))
	for index, document := range result.Page.Documents {
		requests[index] = operationengine.Request{Operation: operation.DeletePermanent, Collection: string(collection.Slug), ID: document.ID, Actor: actor, ActorCollection: actorCollection}
		localeOptions.apply(&requests[index])
	}
	results, err := api.config.Engine.ExecuteBatch(request.Context(), requests)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	documents := make([]json.RawMessage, len(results))
	for index, item := range results {
		documents[index] = documentJSON(*item.Document)
		api.audit(request, requestID, actor, "empty-trash", string(collection.Slug), item.Document.ID)
	}
	writeJSON(writer, http.StatusOK, protocol.BulkEnvelope[json.RawMessage]{Docs: documents})
}

func (api *API) documentAction(writer http.ResponseWriter, request *http.Request, requestID string, collection schema.Collection, segments []string, actor *store.Document, identity *AuthIdentity) {
	collectionName, id, action := segments[0], segments[1], segments[2]
	actorCollection := identityCollection(identity)
	if len(segments) == 3 && action == "copy-locale" {
		api.copyLocale(writer, request, requestID, collectionName, id, actor, actorCollection, "copy-locale")
		return
	}
	if len(segments) == 3 && action == "upload" {
		api.saveUpload(writer, request, requestID, collection, id, identity)
		return
	}
	if len(segments) == 3 && action == "upload-source" {
		api.uploadSource(writer, request, requestID, collection, id, identity)
		return
	}
	if len(segments) == 3 && action == "duplicate" {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		if collection.Auth != nil {
			api.authCollectionCreateError(writer, requestID, collection)
			return
		}
		overrides, err := api.decodeValues(writer, request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		if api.config.Duplicate == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "document duplication is not available"})
			return
		}
		localeOptions, err := decodeLocaleQuery(request.URL.Query())
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		duplicated, err := api.config.Duplicate(request.Context(), collectionName, id, overrides, identity, LocaleOptions{
			Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
			DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales,
		})
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusCreated, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(duplicated)})
		api.audit(request, requestID, actor, "duplicate", collectionName, duplicated.ID)
		return
	}
	if len(segments) == 3 && (action == "restore-deleted" || action == "permanent") {
		if !collection.Capabilities.Trash {
			api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "trash route was not found"})
			return
		}
		kind, method := operation.RestoreDeleted, http.MethodPost
		if action == "permanent" {
			kind, method = operation.DeletePermanent, http.MethodDelete
		}
		if request.Method != method {
			api.methodNotAllowed(writer, requestID, method)
			return
		}
		localeOptions, err := decodeLocaleQuery(request.URL.Query())
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		operationRequest := operationengine.Request{Operation: kind, Collection: collectionName, ID: id, Actor: actor, ActorCollection: actorCollection}
		localeOptions.apply(&operationRequest)
		result, err := api.config.Engine.Execute(request.Context(), operationRequest)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		if action == "permanent" {
			writeJSON(writer, http.StatusOK, protocol.DeleteEnvelope{ID: result.Document.ID, Deleted: true})
		} else {
			writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(*result.Document)})
		}
		api.audit(request, requestID, actor, action, collectionName, id)
		return
	}
	if len(segments) == 3 && action == "lock" {
		api.documentLock(writer, request, requestID, collectionName, id, identity)
		return
	}
	if len(segments) == 4 && action == "joins" {
		if request.Method != http.MethodPatch {
			api.methodNotAllowed(writer, requestID, http.MethodPatch)
			return
		}
		var input protocol.JoinMutationInput
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		localeOptions, err := decodeLocaleQuery(request.URL.Query())
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		result, err := api.config.Engine.MutateJoin(request.Context(), operationengine.JoinMutationRequest{
			Collection: collectionName, ID: id, Field: segments[3],
			Additions: input.Additions, Removals: input.Removals, Actor: actor, ActorCollection: actorCollection,
			Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
			DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales,
		})
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.JoinMutationEnvelope[json.RawMessage]{
			Doc: documentJSON(result.Document), Added: result.Added, Removed: result.Removed,
		})
		api.audit(request, requestID, actor, "mutate-join", collectionName, id)
		return
	}
	if collection.Versions == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "version route was not found"})
		return
	}
	switch action {
	case "versions":
		if request.Method != http.MethodGet {
			api.methodNotAllowed(writer, requestID, http.MethodGet)
			return
		}
		localeOptions, err := decodeLocaleQuery(request.URL.Query())
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		versionLocaleOptions := operationengine.LocalizationOptions{
			Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
			DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales, ActorCollection: actorCollection,
		}
		if len(segments) == 4 {
			if segments[3] == "count" {
				count, err := api.config.Engine.CountVersions(request.Context(), collectionName, id, actor, versionLocaleOptions)
				if err != nil {
					api.writeError(writer, requestID, err)
					return
				}
				writeJSON(writer, http.StatusOK, protocol.CountEnvelope{TotalDocs: count})
				return
			}
			revision, err := strconv.Atoi(segments[3])
			if err != nil || revision < 1 {
				api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "version revision must be a positive integer"})
				return
			}
			version, err := api.config.Engine.Version(request.Context(), collectionName, id, revision, actor, versionLocaleOptions)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			writeJSON(writer, http.StatusOK, map[string]any{"version": versionJSON(version)})
			return
		}
		versions, err := api.config.Engine.Versions(request.Context(), collectionName, id, actor, versionLocaleOptions)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"versions": versionsJSON(versions)})
	case "schedule":
		api.scheduledPublication(writer, request, requestID, collectionName, id, segments, actor, identity)
	case "publish", "unpublish", "discard-draft":
		if request.Method != http.MethodPost || len(segments) != 3 {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		kind := operation.Publish
		if action == "unpublish" {
			kind = operation.Unpublish
		} else if action == "discard-draft" {
			kind = operation.DiscardDraft
		}
		values, err := api.decodeOptionalValues(writer, request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		localeOptions, err := decodeLocaleQuery(request.URL.Query())
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		operationRequest := operationengine.Request{Operation: kind, Collection: collectionName, ID: id, Data: values, Actor: actor, ActorCollection: actorCollection, ExpectedRevision: revisionHeader(request)}
		localeOptions.apply(&operationRequest)
		result, err := api.config.Engine.Execute(request.Context(), operationRequest)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(*result.Document)})
		api.audit(request, requestID, actor, action, collectionName, id)
	case "restore":
		if request.Method != http.MethodPost || len(segments) != 4 {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		revision, err := strconv.Atoi(segments[3])
		if err != nil || revision < 1 {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "restore revision must be a positive integer"})
			return
		}
		draft, queryError := restoreDraftQuery(request)
		if queryError != nil {
			api.writeError(writer, requestID, queryError)
			return
		}
		localeOptions, localeError := decodeLocaleQuery(request.URL.Query())
		if localeError != nil {
			api.writeError(writer, requestID, localeError)
			return
		}
		result, err := api.config.Engine.Restore(request.Context(), collectionName, id, revision, revisionHeader(request), draft, actor, operationengine.LocalizationOptions{
			Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
			DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales, ActorCollection: actorCollection,
		})
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(*result.Document)})
		action := "restore"
		if draft {
			action = "restore-as-draft"
		}
		api.audit(request, requestID, actor, action, collectionName, id)
	default:
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "version route was not found"})
	}
}

func (api *API) copyLocale(writer http.ResponseWriter, request *http.Request, requestID, collection, id string, actor *store.Document, actorCollection schema.CollectionSlug, auditAction string) {
	if request.Method != http.MethodPost {
		api.methodNotAllowed(writer, requestID, http.MethodPost)
		return
	}
	var input struct {
		From schema.LocaleCode `json:"from"`
		To   schema.LocaleCode `json:"to"`
	}
	if err := api.decodeJSON(writer, request, &input); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	document, err := api.config.Engine.CopyLocale(request.Context(), collection, id, input.From, input.To, revisionHeader(request), actor, operationengine.LocalizationOptions{ActorCollection: actorCollection})
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(document)})
	api.audit(request, requestID, actor, auditAction, collection, id)
}

func (api *API) authCollectionCreateError(writer http.ResponseWriter, requestID string, collection schema.Collection) {
	api.writeError(writer, requestID, &operationengine.Error{
		Code: "bad_operation", Status: 400,
		Message: fmt.Sprintf("auth collection %q must be created through /api/auth/%s/create-user", collection.Slug, collection.Slug),
	})
}

func (api *API) documentLock(writer http.ResponseWriter, request *http.Request, requestID, collection, id string, identity *AuthIdentity) {
	if api.config.DocumentLock == nil || api.config.AcquireDocumentLock == nil || api.config.ReleaseDocumentLock == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "document locking was not found"})
		return
	}
	actor := identityActor(identity)
	switch request.Method {
	case http.MethodGet:
		state, err := api.config.DocumentLock(request.Context(), collection, id, identity)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, state)
	case http.MethodPost:
		var input struct {
			Takeover bool `json:"takeover"`
		}
		if request.Body != nil && request.ContentLength != 0 {
			if err := api.decodeJSON(writer, request, &input); err != nil {
				api.writeError(writer, requestID, err)
				return
			}
		}
		state, err := api.config.AcquireDocumentLock(request.Context(), collection, id, input.Takeover, identity)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, state)
		api.audit(request, requestID, actor, map[bool]string{true: "takeover-lock", false: "acquire-lock"}[input.Takeover], collection, id)
	case http.MethodDelete:
		if err := api.config.ReleaseDocumentLock(request.Context(), collection, id, identity); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DeleteEnvelope{ID: id, Deleted: true})
		api.audit(request, requestID, actor, "release-lock", collection, id)
	default:
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodPost, http.MethodDelete)
	}
}

func restoreDraftQuery(request *http.Request) (bool, error) {
	value := request.URL.Query().Get("draft")
	switch value {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, &operationengine.Error{Code: "bad_request", Status: 400, Message: "draft must be true or false"}
	}
}

func (api *API) scheduledPublication(writer http.ResponseWriter, request *http.Request, requestID, collection, documentID string, segments []string, actor *store.Document, identity *AuthIdentity) {
	if api.config.SchedulePublish == nil || api.config.ScheduledPublications == nil || api.config.CancelScheduledPublication == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publishing is unavailable"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		if len(segments) != 3 {
			api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publication was not found"})
			return
		}
		result, err := api.readScheduledPublications(request.Context(), collection, documentID, identity)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.ScheduledPublicationsEnvelope{ScheduledPublications: result})
	case http.MethodPost:
		if len(segments) != 3 {
			api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publication route was not found"})
			return
		}
		var input struct {
			Action   string `json:"action"`
			RunAt    string `json:"runAt"`
			TimeZone string `json:"timeZone"`
		}
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		runAt, err := time.Parse(time.RFC3339, input.RunAt)
		if err != nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "validation", Status: 422, Message: "runAt must be an RFC 3339 timestamp"})
			return
		}
		var job store.ScheduledPublication
		switch input.Action {
		case string(store.PublicationActionPublish):
			job, err = api.config.SchedulePublish(request.Context(), collection, documentID, runAt, input.TimeZone, revisionHeader(request), identity)
		case string(store.PublicationActionUnpublish):
			if api.config.ScheduleUnpublish == nil {
				api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled unpublishing is unavailable"})
				return
			}
			job, err = api.config.ScheduleUnpublish(request.Context(), collection, documentID, runAt, input.TimeZone, revisionHeader(request), identity)
		default:
			api.writeError(writer, requestID, &operationengine.Error{Code: "validation", Status: 422, Message: "action must be publish or unpublish"})
			return
		}
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusCreated, protocol.ScheduledPublicationEnvelope{ScheduledPublication: scheduledPublicationJSON(job)})
		api.audit(request, requestID, actor, "schedule-"+input.Action, collection, documentID)
	case http.MethodDelete:
		if len(segments) != 4 {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "scheduled publication id is required"})
			return
		}
		if err := api.config.CancelScheduledPublication(request.Context(), collection, documentID, segments[3], identity); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DeleteEnvelope{ID: segments[3], Deleted: true})
		api.audit(request, requestID, actor, "cancel-scheduled-publication", collection, documentID)
	default:
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodPost, http.MethodDelete)
	}
}

func scheduledPublicationJSON(job store.ScheduledPublication) protocol.ScheduledPublication {
	return protocol.ScheduledPublication{
		ID: job.ID, Action: string(job.Action), DocumentID: job.DocumentID, ExpectedRevision: job.ExpectedRevision,
		RunAt: job.RunAt.UTC().Format(time.RFC3339Nano), TimeZone: job.TimeZone, Attempts: job.Attempts,
		LastError: job.LastError, CreatedAt: job.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (api *API) bulkCollection(writer http.ResponseWriter, request *http.Request, requestID string, collection schema.Collection, identity *AuthIdentity) {
	actor := identityActor(identity)
	actorCollection := identityCollection(identity)
	if request.Method != http.MethodPost {
		api.methodNotAllowed(writer, requestID, http.MethodPost)
		return
	}
	var input struct {
		Action string       `json:"action"`
		IDs    []string     `json:"ids"`
		Data   store.Values `json:"data,omitempty"`
	}
	if err := api.decodeJSON(writer, request, &input); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	if len(input.IDs) == 0 || len(input.IDs) > operationengine.MaxBatchDocuments {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: fmt.Sprintf("bulk operations require between 1 and %d document IDs", operationengine.MaxBatchDocuments)})
		return
	}
	seen := make(map[string]bool, len(input.IDs))
	localeOptions, err := decodeLocaleQuery(request.URL.Query())
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	kind := operation.Update
	switch input.Action {
	case "update":
		if input.Data == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "bulk update requires data"})
			return
		}
	case "publish":
		kind = operation.Publish
	case "unpublish":
		kind = operation.Unpublish
	case "delete":
		kind = operation.Delete
	case "restoreDeleted":
		kind = operation.RestoreDeleted
	case "deletePermanent":
		kind = operation.DeletePermanent
	default:
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "bulk action must be update, publish, unpublish, delete, restoreDeleted, or deletePermanent"})
		return
	}
	requests := make([]operationengine.Request, len(input.IDs))
	for index, id := range input.IDs {
		if id == "" || seen[id] {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "bulk document IDs must be non-empty and unique"})
			return
		}
		seen[id] = true
		requests[index] = operationengine.Request{Operation: kind, Collection: string(collection.Slug), ID: id, Data: store.CloneValues(input.Data), Actor: actor, ActorCollection: actorCollection}
		localeOptions.apply(&requests[index])
	}
	results, err := api.config.Engine.ExecuteBatch(request.Context(), requests)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	documents := make([]json.RawMessage, len(results))
	for index, result := range results {
		documents[index] = documentJSON(*result.Document)
		api.audit(request, requestID, actor, "bulk-"+input.Action, string(collection.Slug), result.Document.ID)
	}
	writeJSON(writer, http.StatusOK, protocol.BulkEnvelope[json.RawMessage]{Docs: documents})
}

func revisionHeader(request *http.Request) int {
	encoded := strings.Trim(request.Header.Get("If-Match"), "\"")
	revision, _ := strconv.Atoi(encoded)
	return revision
}

func (api *API) upload(writer http.ResponseWriter, request *http.Request, requestID string) {
	remainder := strings.TrimPrefix(request.URL.Path, "/api/uploads/")
	if collection, grants := strings.CutSuffix(remainder, "/grants"); grants && collection != "" && !strings.Contains(collection, "/") {
		api.uploadGrants(writer, request, requestID, collection)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodHead)
		return
	}
	segments := strings.SplitN(remainder, "/", 2)
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" || api.config.OpenUpload == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "upload was not found"})
		return
	}
	key := remainder
	if strings.HasPrefix(segments[1], "ridu/") {
		key = segments[1]
	}
	var reader io.ReadCloser
	var object storage.Object
	var err error
	if grant := request.URL.Query().Get("grant"); grant != "" {
		if api.config.OpenUploadGrant == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "upload was not found"})
			return
		}
		reader, object, err = api.config.OpenUploadGrant(request.Context(), segments[0], key, grant)
	} else {
		reader, object, err = api.config.OpenUpload(request.Context(), segments[0], key, api.optionalIdentity(request))
	}
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	serveUpload(writer, request, reader, object)
}

// uploadGrants mints short-lived delivery URLs for elements such as <img>
// that cannot send an Authorization header. Grants are bound to a session.
func (api *API) uploadGrants(writer http.ResponseWriter, request *http.Request, requestID, collection string) {
	if request.Method != http.MethodPost {
		api.methodNotAllowed(writer, requestID, http.MethodPost)
		return
	}
	if api.config.CreateUploadGrants == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "upload grants are not available"})
		return
	}
	token, _, ok := sessionCredential(request)
	if !ok {
		api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "a session is required to create upload grants"})
		return
	}
	var input protocol.UploadGrantsRequest
	if err := api.decodeJSON(writer, request, &input); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	if input.ExpiresIn < 0 {
		api.writeError(writer, requestID, &operationengine.Error{Code: "validation", Status: 422, Message: "expiresIn must be a positive number of seconds"})
		return
	}
	grants, err := api.config.CreateUploadGrants(request.Context(), token, collection, input.Items, time.Duration(input.ExpiresIn)*time.Second)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, protocol.UploadGrantsEnvelope{Grants: grants})
}

func serveUpload(writer http.ResponseWriter, request *http.Request, reader io.ReadCloser, object storage.Object) {
	defer reader.Close()
	contentType := object.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Content-Length", strconv.FormatInt(object.Size, 10))
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	writer.Header().Set("X-Frame-Options", "DENY")
	if activeUploadContent(contentType) {
		writer.Header().Set("Content-Disposition", "attachment")
	}
	// Every delivery is resolved through collection read access, which may be
	// actor-, request-, or row-dependent even when Upload.Private is false.
	// Shared cacheability requires a future explicit actor-independent contract.
	writer.Header().Set("Cache-Control", "private, no-store")
	if request.Method == http.MethodHead {
		writer.WriteHeader(http.StatusOK)
		return
	}
	writer.WriteHeader(http.StatusOK)
	_, _ = io.Copy(writer, reader)
}

func activeUploadContent(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	switch strings.ToLower(mediaType) {
	case "text/html", "application/xhtml+xml", "image/svg+xml", "application/xml", "text/xml":
		return true
	default:
		return false
	}
}

func (api *API) auth(writer http.ResponseWriter, request *http.Request, requestID string) {
	path := strings.TrimPrefix(request.URL.Path, "/api/auth/")
	if collection, matched := authBootstrapAction(path); matched {
		api.authBootstrap(writer, request, requestID, collection)
		return
	}
	if collection, matched := authUserCreateAction(path); matched {
		api.createAuthUser(writer, request, requestID, collection)
		return
	}
	escapedPath := strings.TrimPrefix(request.URL.EscapedPath(), "/api/auth/")
	if collection, id, matched := accountUnlockAction(escapedPath); matched {
		api.accountUnlock(writer, request, requestID, collection, id)
		return
	}
	if collection, action, matched := collectionAuthAction(path); matched {
		api.collectionAuthAction(writer, request, requestID, collection, action)
		return
	}
	if strings.HasSuffix(path, "/login") {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		collection := strings.TrimSuffix(path, "/login")
		if collection == "" || strings.Contains(collection, "/") || api.config.Login == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "unknown_auth_collection", Status: 404, Message: "auth collection was not found"})
			return
		}
		var credentials protocol.LoginRequest
		if err := api.decodeJSON(writer, request, &credentials); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		if credentials.Transport != "" && credentials.Transport != protocol.SessionTransportCookie && credentials.Transport != protocol.SessionTransportToken {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: `login transport must be "cookie" or "token"`})
			return
		}
		clientIP := api.clientIP(request)
		if rateError := api.admitAuthAttempt(request.Context(), clientIP, collection, credentials.Email); rateError != nil {
			api.audit(request, requestID, nil, "login_rate_limited", collection, "")
			api.writeError(writer, requestID, rateError)
			return
		}
		session, err := api.config.Login(request.Context(), collection, credentials.Email, credentials.Password, LoginMetadata{
			IPAddress: clientIP, UserAgent: request.UserAgent(),
		})
		if err != nil {
			api.audit(request, requestID, nil, "login_failed", collection, "")
			api.writeError(writer, requestID, err)
			return
		}
		transport := transportCookie
		if credentials.Transport == protocol.SessionTransportToken {
			transport = transportHeader
		}
		api.writeIssuedSession(writer, session, transport)
		api.audit(request, requestID, &session.User, "login", collection, session.User.ID, session.Collection)
		return
	}
	if path == "logout-all" {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		token, transport, ok := sessionCredential(request)
		if !ok || api.config.LogoutAll == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		identity := api.optionalIdentity(request)
		if err := api.config.LogoutAll(request.Context(), token); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		if transport == transportCookie {
			api.clearSessionCookie(writer)
		}
		writeJSON(writer, http.StatusOK, protocol.LogoutEnvelope{LoggedOut: true})
		api.audit(request, requestID, identityActor(identity), "logout_all", "", "", identityCollection(identity))
		return
	}
	if path == "change-password" {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		token, transport, ok := sessionCredential(request)
		if !ok || api.config.ChangePassword == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		var input struct {
			CurrentPassword string `json:"currentPassword"`
			Password        string `json:"password"`
		}
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		identity := api.optionalIdentity(request)
		if err := api.config.ChangePassword(request.Context(), token, input.CurrentPassword, input.Password); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		if transport == transportCookie {
			api.clearSessionCookie(writer)
		}
		writeJSON(writer, http.StatusOK, protocol.AuthActionEnvelope{Success: true})
		api.audit(request, requestID, identityActor(identity), "password_change", "", "", identityCollection(identity))
		return
	}
	if path == "api-keys" {
		api.apiKeys(writer, request, requestID)
		return
	}
	if strings.HasPrefix(path, "api-keys/") {
		api.revokeAPIKey(writer, request, requestID, strings.TrimPrefix(path, "api-keys/"))
		return
	}
	if path == "logout" {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		identity := api.optionalIdentity(request)
		token, transport, ok := sessionCredential(request)
		if ok && api.config.Logout != nil {
			if err := api.config.Logout(request.Context(), token); err != nil {
				api.writeError(writer, requestID, err)
				return
			}
		}
		if !ok || transport == transportCookie {
			api.clearSessionCookie(writer)
		}
		writeJSON(writer, http.StatusOK, protocol.LogoutEnvelope{LoggedOut: true})
		api.audit(request, requestID, identityActor(identity), "logout", "", "", identityCollection(identity))
		return
	}
	if path == "sessions" {
		if request.Method != http.MethodGet {
			api.methodNotAllowed(writer, requestID, http.MethodGet)
			return
		}
		token, _, ok := sessionCredential(request)
		if !ok || api.config.Sessions == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		sessions, err := api.config.Sessions(request.Context(), token)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		response := protocol.AuthSessionsEnvelope{Sessions: make([]protocol.AuthSessionInfo, len(sessions))}
		for index, session := range sessions {
			response.Sessions[index] = protocol.AuthSessionInfo{
				ID: session.ID, CreatedAt: session.CreatedAt.Format(time.RFC3339Nano),
				LastSeenAt: session.LastSeenAt.Format(time.RFC3339Nano),
				ExpiresAt:  session.ExpiresAt.Format(time.RFC3339Nano),
				IPAddress:  session.IPAddress, UserAgent: session.UserAgent, Current: session.Current,
			}
		}
		writeJSON(writer, http.StatusOK, response)
		return
	}
	if strings.HasPrefix(path, "sessions/") {
		if request.Method != http.MethodDelete {
			api.methodNotAllowed(writer, requestID, http.MethodDelete)
			return
		}
		sessionID := strings.TrimPrefix(path, "sessions/")
		if sessionID == "" || strings.Contains(sessionID, "/") {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "session ID is invalid"})
			return
		}
		token, _, ok := sessionCredential(request)
		if !ok || api.config.RevokeSession == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		identity := api.optionalIdentity(request)
		if err := api.config.RevokeSession(request.Context(), token, sessionID); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DeleteEnvelope{ID: sessionID, Deleted: true})
		api.audit(request, requestID, identityActor(identity), "session_revoke", "", sessionID, identityCollection(identity))
		return
	}
	if path == "rotate" {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		token, transport, ok := sessionCredential(request)
		if !ok || api.config.RotateSession == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		session, err := api.config.RotateSession(request.Context(), token)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		api.writeIssuedSession(writer, session, transport)
		return
	}
	if path == "me" {
		if request.Method != http.MethodGet {
			api.methodNotAllowed(writer, requestID, http.MethodGet)
			return
		}
		state := api.credentialState(request)
		if state.session == nil && state.cookieErr != nil {
			api.writeError(writer, requestID, state.cookieErr)
			return
		}
		if state.session == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		writer.Header().Set("Cache-Control", "private, no-store")
		writeJSON(writer, http.StatusOK, sessionEnvelope(*state.session))
		return
	}
	api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "auth route was not found"})
}

func authBootstrapAction(path string) (string, bool) {
	segments := strings.Split(path, "/")
	if len(segments) == 2 && segments[0] != "" && segments[1] == "bootstrap" {
		return segments[0], true
	}
	return "", false
}

func (api *API) authBootstrap(writer http.ResponseWriter, request *http.Request, requestID, collection string) {
	if request.Method != http.MethodGet {
		api.methodNotAllowed(writer, requestID, http.MethodGet)
		return
	}
	if api.config.AuthBootstrapAvailable == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "unknown_auth_collection", Status: 404, Message: "auth collection was not found"})
		return
	}
	available, err := api.config.AuthBootstrapAvailable(request.Context(), collection)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, protocol.AuthBootstrapEnvelope{Available: available})
}

func authUserCreateAction(path string) (string, bool) {
	segments := strings.Split(path, "/")
	if len(segments) == 2 && segments[0] != "" && segments[1] == "create-user" {
		return segments[0], true
	}
	return "", false
}

func (api *API) createAuthUser(writer http.ResponseWriter, request *http.Request, requestID, collection string) {
	if request.Method != http.MethodPost {
		api.methodNotAllowed(writer, requestID, http.MethodPost)
		return
	}
	if api.config.CreateAuthUser == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "unknown_auth_collection", Status: 404, Message: "auth collection was not found"})
		return
	}
	var input struct {
		Data     store.Values `json:"data"`
		Password string       `json:"password"`
	}
	if err := api.decodeJSON(writer, request, &input); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	identity := api.optionalIdentity(request)
	actor := identityActor(identity)
	localeOptions, err := decodeLocaleQuery(request.URL.Query())
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	draft, err := decodeDraftQuery(request)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	document, err := api.config.CreateAuthUser(request.Context(), collection, input.Data, input.Password, identity, localeOptions.public(), draft)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusCreated, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(document)})
	api.audit(request, requestID, actor, "create-auth-user", collection, document.ID)
}

func accountUnlockAction(path string) (string, string, bool) {
	segments, err := decodeEscapedPathSegments(path)
	if err != nil {
		return "", "", false
	}
	if len(segments) != 3 || segments[0] == "" || segments[1] == "" || segments[2] != "unlock" {
		return "", "", false
	}
	return segments[0], segments[1], true
}

func (api *API) accountUnlock(writer http.ResponseWriter, request *http.Request, requestID, collection, id string) {
	if request.Method != http.MethodPost {
		api.methodNotAllowed(writer, requestID, http.MethodPost)
		return
	}
	identity := api.optionalIdentity(request)
	actor := identityActor(identity)
	if api.config.ForceUnlock == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "account unlock is not available"})
		return
	}
	if err := api.config.ForceUnlock(request.Context(), collection, id, identity); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusOK, protocol.AuthActionEnvelope{Success: true})
	api.audit(request, requestID, actor, "force_unlock", collection, id)
}

func (api *API) apiKeys(writer http.ResponseWriter, request *http.Request, requestID string) {
	token, _, ok := sessionCredential(request)
	if !ok {
		api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
		return
	}
	identity := api.optionalIdentity(request)
	switch request.Method {
	case http.MethodGet:
		if api.config.APIKeys == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "auth_feature_disabled", Status: 404, Message: "API keys are not enabled"})
			return
		}
		keys, err := api.config.APIKeys(request.Context(), token)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		response := protocol.APIKeysEnvelope{APIKeys: make([]protocol.APIKeyInfo, len(keys))}
		for index, key := range keys {
			response.APIKeys[index] = apiKeyInfoEnvelope(key)
		}
		writeJSON(writer, http.StatusOK, response)
	case http.MethodPost:
		if api.config.CreateAPIKey == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "auth_feature_disabled", Status: 404, Message: "API keys are not enabled"})
			return
		}
		var input struct {
			Name      string `json:"name"`
			ExpiresAt string `json:"expiresAt"`
		}
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		var expiresAt time.Time
		if input.ExpiresAt != "" {
			var err error
			expiresAt, err = time.Parse(time.RFC3339, input.ExpiresAt)
			if err != nil {
				api.writeError(writer, requestID, &operationengine.Error{Code: "validation", Status: 422, Message: "API key expiry must be an RFC 3339 timestamp"})
				return
			}
		}
		key, err := api.config.CreateAPIKey(request.Context(), token, input.Name, expiresAt)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusCreated, protocol.APIKeyEnvelope{APIKey: protocol.APIKey{ID: key.ID, Name: key.Name, Key: key.Key, CreatedAt: key.CreatedAt.Format(time.RFC3339Nano), ExpiresAt: formatOptionalTime(key.ExpiresAt)}})
		api.audit(request, requestID, identityActor(identity), "api_key_create", "", key.ID, identityCollection(identity))
	default:
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodPost)
	}
}

func (api *API) revokeAPIKey(writer http.ResponseWriter, request *http.Request, requestID, id string) {
	if request.Method != http.MethodDelete {
		api.methodNotAllowed(writer, requestID, http.MethodDelete)
		return
	}
	if id == "" || strings.Contains(id, "/") {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "API key ID is invalid"})
		return
	}
	token, _, ok := sessionCredential(request)
	if !ok || api.config.RevokeAPIKey == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
		return
	}
	identity := api.optionalIdentity(request)
	if err := api.config.RevokeAPIKey(request.Context(), token, id); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusOK, protocol.DeleteEnvelope{ID: id, Deleted: true})
	api.audit(request, requestID, identityActor(identity), "api_key_revoke", "", id, identityCollection(identity))
}

func apiKeyInfoEnvelope(key APIKeyInfo) protocol.APIKeyInfo {
	return protocol.APIKeyInfo{ID: key.ID, Name: key.Name, CreatedAt: key.CreatedAt.Format(time.RFC3339Nano), LastUsedAt: formatOptionalTime(key.LastUsedAt), ExpiresAt: formatOptionalTime(key.ExpiresAt)}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func collectionAuthAction(path string) (string, string, bool) {
	segments := strings.Split(path, "/")
	if len(segments) != 2 || segments[0] == "" {
		return "", "", false
	}
	switch segments[1] {
	case "forgot-password", "reset-password", "request-verification", "verify":
		return segments[0], segments[1], true
	default:
		return "", "", false
	}
}

func (api *API) collectionAuthAction(writer http.ResponseWriter, request *http.Request, requestID, collection, action string) {
	if request.Method != http.MethodPost {
		api.methodNotAllowed(writer, requestID, http.MethodPost)
		return
	}
	switch action {
	case "forgot-password":
		var input struct {
			Email string `json:"email"`
		}
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		if api.config.RequestPasswordReset == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "auth_feature_disabled", Status: 404, Message: "password reset is not enabled"})
			return
		}
		if api.authActionRateLimited(writer, request, requestID, collection+":"+action, input.Email) {
			return
		}
		if err := api.config.RequestPasswordReset(request.Context(), collection, input.Email); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
	case "reset-password":
		var input struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		if api.config.ResetPassword == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "auth_feature_disabled", Status: 404, Message: "password reset is not enabled"})
			return
		}
		if api.authActionRateLimited(writer, request, requestID, collection+":"+action, "") {
			return
		}
		if err := api.config.ResetPassword(request.Context(), collection, input.Token, input.Password); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
	case "request-verification":
		var input struct {
			Email string `json:"email"`
		}
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		if api.config.RequestVerification == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "auth_feature_disabled", Status: 404, Message: "email verification is not enabled"})
			return
		}
		if api.authActionRateLimited(writer, request, requestID, collection+":"+action, input.Email) {
			return
		}
		if err := api.config.RequestVerification(request.Context(), collection, input.Email); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
	case "verify":
		var input struct {
			Token string `json:"token"`
		}
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		if api.config.VerifyEmail == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "auth_feature_disabled", Status: 404, Message: "email verification is not enabled"})
			return
		}
		if api.authActionRateLimited(writer, request, requestID, collection+":"+action, "") {
			return
		}
		if err := api.config.VerifyEmail(request.Context(), collection, input.Token); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
	}
	writeJSON(writer, http.StatusOK, protocol.AuthActionEnvelope{Success: true})
	api.audit(request, requestID, nil, action, collection, "")
}

func (api *API) authActionRateLimited(writer http.ResponseWriter, request *http.Request, requestID, collection, identity string) bool {
	err := api.admitAuthAttempt(request.Context(), api.clientIP(request), collection, identity)
	if err != nil {
		api.writeError(writer, requestID, err)
		return true
	}
	return false
}

func sessionEnvelope(session AuthSession) protocol.SessionEnvelope[json.RawMessage] {
	return protocol.SessionEnvelope[json.RawMessage]{Session: protocol.AuthSession[json.RawMessage]{
		ID: session.ID, Collection: string(session.Collection), User: documentJSON(session.User),
		ExpiresAt: session.ExpiresAt.Format(time.RFC3339Nano),
	}}
}

// writeIssuedSession delivers a newly issued or rotated token through the
// selected transport. A token response is never cached and never sets the
// cookie, so a same-site admin cookie is not replaced by a frontend login.
func (api *API) writeIssuedSession(writer http.ResponseWriter, session AuthSession, transport sessionTransport) {
	writer.Header().Set("Cache-Control", "no-store")
	if transport == transportHeader {
		writeJSON(writer, http.StatusOK, protocol.SessionTokenEnvelope[json.RawMessage]{
			Session: sessionEnvelope(session).Session, Token: session.Token,
		})
		return
	}
	http.SetCookie(writer, &http.Cookie{Name: sessionCookie, Value: session.Token, Path: "/", HttpOnly: true, Secure: api.config.SecureCookies, SameSite: http.SameSiteLaxMode, Expires: session.ExpiresAt})
	writeJSON(writer, http.StatusOK, sessionEnvelope(session))
}

func (api *API) clearSessionCookie(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: api.config.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

func (api *API) optionalActor(request *http.Request) *store.Document {
	return identityActor(api.optionalIdentity(request))
}

func identityActor(identity *AuthIdentity) *store.Document {
	if identity == nil {
		return nil
	}
	return &identity.Actor
}

func identityCollection(identity *AuthIdentity) schema.CollectionSlug {
	if identity == nil {
		return ""
	}
	return identity.Collection
}

// optionalIdentity returns the request's single principal. See
// credentialState for precedence; an explicit credential never falls back.
func (api *API) optionalIdentity(request *http.Request) *AuthIdentity {
	return api.credentialState(request).identity
}

func (api *API) cors(writer http.ResponseWriter, request *http.Request, requestID string) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		if request.Method != http.MethodGet && request.Method != http.MethodHead && strings.EqualFold(strings.TrimSpace(request.Header.Get("Sec-Fetch-Site")), "cross-site") {
			api.writeError(writer, requestID, &operationengine.Error{Code: "origin_denied", Status: 403, Message: "cross-site request is not allowed"})
			return true
		}
		return false
	}
	allowed := api.originAllowed(request, origin)
	if allowed {
		writer.Header().Set("Access-Control-Allow-Origin", origin)
		writer.Header().Set("Access-Control-Allow-Credentials", "true")
		writer.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, X-Request-ID")
		writer.Header().Add("Vary", "Origin")
	}
	if request.Method == http.MethodOptions && strings.TrimSpace(request.Header.Get("Access-Control-Request-Method")) != "" {
		if !allowed {
			api.writeError(writer, requestID, &operationengine.Error{Code: "origin_denied", Status: 403, Message: "request origin is not allowed"})
			return true
		}
		if requestedMethod := strings.ToUpper(strings.TrimSpace(request.Header.Get("Access-Control-Request-Method"))); requestedMethod != "" && !corsMethodAllowed(requestedMethod) {
			api.writeError(writer, requestID, &operationengine.Error{Code: "cors_method_denied", Status: 403, Message: "requested CORS method is not allowed"})
			return true
		}
		if !api.corsHeadersAllowed(request.Header.Get("Access-Control-Request-Headers")) {
			api.writeError(writer, requestID, &operationengine.Error{Code: "cors_header_denied", Status: 403, Message: "requested CORS header is not allowed"})
			return true
		}
		writer.Header().Add("Vary", "Access-Control-Request-Method")
		writer.Header().Add("Vary", "Access-Control-Request-Headers")
		writer.Header().Set("Access-Control-Allow-Headers", strings.Join(api.allowedHeaders, ", "))
		writer.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
		// Direct token clients preflight every mutation; caching the answer keeps
		// ordinary writes to one round trip.
		writer.Header().Set("Access-Control-Max-Age", "600")
		if api.hasMatchingCustomEndpoint(request) {
			return false
		}
		writer.WriteHeader(http.StatusNoContent)
		return true
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead && !allowed {
		api.writeError(writer, requestID, &operationengine.Error{Code: "origin_denied", Status: 403, Message: "request origin is not allowed"})
		return true
	}
	return false
}

func (api *API) originAllowed(request *http.Request, origin string) bool {
	originCanonical, err := canonicalOrigin(origin)
	if err != nil {
		return false
	}
	for _, allowed := range api.config.AllowedOrigins {
		allowedCanonical, allowedError := canonicalOrigin(allowed)
		if allowedError == nil && originCanonical == allowedCanonical {
			return true
		}
	}
	requestCanonical, requestError := canonicalRequestOrigin(request, api.config.TrustedProxies)
	return requestError == nil && originCanonical == requestCanonical
}

func canonicalOrigin(encoded string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(encoded))
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", errors.New("invalid HTTP origin")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("invalid HTTP origin scheme")
	}
	return canonicalSchemeHost(scheme, parsed.Host)
}

func canonicalRequestOrigin(request *http.Request, trusted []netip.Prefix) (string, error) {
	return canonicalSchemeHost(effectiveRequestScheme(request, trusted), request.Host)
}

func canonicalSchemeHost(scheme, encodedHost string) (string, error) {
	host, port, _, err := canonicalHost(encodedHost)
	if err != nil {
		return "", err
	}
	if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		port = ""
	}
	canonical := host
	if strings.Contains(host, ":") {
		canonical = "[" + host + "]"
	}
	if port != "" {
		canonical += ":" + port
	}
	return scheme + "://" + canonical, nil
}

func effectiveRequestScheme(request *http.Request, trusted []netip.Prefix) string {
	if request.TLS != nil {
		return "https"
	}
	if !remoteRequestTrusted(request, trusted) {
		return "http"
	}
	if forwarded := request.Header.Get("Forwarded"); forwarded != "" {
		if scheme, ok := forwardedProto(forwarded); ok {
			return scheme
		}
		return "http"
	}
	if forwarded := request.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		scheme := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
		if scheme == "http" || scheme == "https" {
			return scheme
		}
	}
	return "http"
}

func forwardedProto(header string) (string, bool) {
	elements := strings.Split(header, ",")
	parameters := strings.Split(elements[len(elements)-1], ";")
	for _, parameter := range parameters {
		key, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
		if !found || !strings.EqualFold(key, "proto") {
			continue
		}
		value = strings.ToLower(strings.Trim(strings.TrimSpace(value), `"`))
		return value, value == "http" || value == "https"
	}
	return "", false
}

func remoteRequestTrusted(request *http.Request, trusted []netip.Prefix) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	address, err := netip.ParseAddr(host)
	return err == nil && addressTrusted(address.Unmap(), trusted)
}

func addressTrusted(address netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(address) || prefix.Contains(address.Unmap()) {
			return true
		}
		if prefix.Addr().Is4In6() && address.Is4() {
			bits := prefix.Bits() - 96
			if bits >= 0 && netip.PrefixFrom(prefix.Addr().Unmap(), bits).Contains(address) {
				return true
			}
		}
	}
	return false
}

func corsMethodAllowed(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

func (api *API) corsHeadersAllowed(encoded string) bool {
	if strings.TrimSpace(encoded) == "" {
		return true
	}
	for _, header := range strings.Split(encoded, ",") {
		header = strings.ToLower(strings.TrimSpace(header))
		if !validHeaderName(header) {
			return false
		}
		if _, allowed := api.allowedHeaderSet[header]; !allowed {
			return false
		}
	}
	return true
}

func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range []byte(value) {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		switch character {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func (api *API) clientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	address, addressError := netip.ParseAddr(host)
	if addressError == nil {
		address = address.Unmap()
	}
	if addressError == nil && addressTrusted(address, api.config.TrustedProxies) {
		current := address
		chain := strings.Split(request.Header.Get("X-Forwarded-For"), ",")
		for index := len(chain) - 1; index >= 0; index-- {
			forwarded, err := netip.ParseAddr(strings.TrimSpace(chain[index]))
			if err != nil {
				return current.String()
			}
			forwarded = forwarded.Unmap()
			current = forwarded
			if !addressTrusted(forwarded, api.config.TrustedProxies) {
				return forwarded.String()
			}
		}
		return current.String()
	}
	return host
}

type allowedHost struct {
	host string
	port string
}

func parseAllowedHost(value string) (allowedHost, error) {
	host, port, _, err := canonicalHost(value)
	if err != nil {
		return allowedHost{}, err
	}
	return allowedHost{host: host, port: port}, nil
}

func canonicalHost(value string) (string, string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 320 || strings.ContainsAny(value, "@/?#\\\t\r\n ") {
		return "", "", "", errors.New("invalid HTTP host")
	}
	host, port := value, ""
	if parsedHost, parsedPort, err := net.SplitHostPort(value); err == nil {
		host, port = parsedHost, parsedPort
	} else if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	} else if strings.Count(value, ":") > 1 {
		if _, err := netip.ParseAddr(value); err != nil {
			return "", "", "", errors.New("invalid HTTP host")
		}
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || len(host) > 253 || !validHostName(host) {
		return "", "", "", errors.New("invalid HTTP host")
	}
	if port != "" {
		parsed, err := strconv.Atoi(port)
		if err != nil || parsed < 1 || parsed > 65535 {
			return "", "", "", errors.New("invalid HTTP host port")
		}
		port = strconv.Itoa(parsed)
	}
	canonical := host
	if strings.Contains(host, ":") {
		canonical = "[" + host + "]"
	}
	if port != "" {
		canonical += ":" + port
	}
	return host, port, canonical, nil
}

func validHostName(host string) bool {
	if address, err := netip.ParseAddr(host); err == nil {
		return address.IsValid()
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range []byte(label) {
			if character < 'a' || character > 'z' {
				if character < '0' || character > '9' {
					if character != '-' {
						return false
					}
				}
			}
		}
	}
	return true
}

func (api *API) hostAllowed(raw string) bool {
	host, port, _, err := canonicalHost(raw)
	if err != nil {
		return false
	}
	if !api.restrictHosts {
		return true
	}
	for _, allowed := range api.allowedHosts {
		if allowed.host == host && (allowed.port == "" || allowed.port == port) {
			return true
		}
	}
	return false
}

func (api *API) securityHeaders(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	if validHeaderValue(api.config.StrictTransportSecurity) && api.config.StrictTransportSecurity != "" {
		writer.Header().Set("Strict-Transport-Security", api.config.StrictTransportSecurity)
	}
	if strings.HasPrefix(request.URL.Path, "/admin") {
		writer.Header().Set("X-Frame-Options", "DENY")
		if !api.config.DisableContentSecurityPolicy && validHeaderValue(api.config.ContentSecurityPolicy) {
			writer.Header().Set("Content-Security-Policy", api.config.ContentSecurityPolicy)
		}
	} else if request.URL.Path == "/api" || strings.HasPrefix(request.URL.Path, "/api/") || request.URL.Path == "/healthz" || request.URL.Path == "/readyz" {
		writer.Header().Set("Cache-Control", "private, no-store")
	}
}

func validHeaderValue(value string) bool {
	return !strings.ContainsAny(value, "\r\n")
}

// failureCause explains a failure for server logs: every wrapped cause after
// the top-level message, and any field issues. Responses keep them hidden, so
// without this a hook's real error, such as a reference it could not validate,
// never reached the log.
func failureCause(err error) string {
	if err == nil {
		return ""
	}
	top := err.Error()
	var parts []string
	for current := err; current != nil; current = errors.Unwrap(current) {
		if message := current.Error(); message != top && (len(parts) == 0 || !strings.Contains(parts[len(parts)-1], message)) {
			parts = append(parts, message)
		}
		if operationError, ok := current.(*operationengine.Error); ok {
			for _, issue := range operationError.Issues {
				detail := issue.Message
				if issue.Path != "" {
					detail = issue.Path + ": " + detail
				}
				parts = append(parts, detail)
			}
		}
	}
	return strings.Join(parts, "; ")
}

func (api *API) reportRequestError(request *http.Request, requestID string, err error, recovered bool, stack string) {
	api.reportRequestErrorDetails(request.Method, request.URL.Path, requestID, err, recovered, stack)
}

func (api *API) reportRequestErrorDetails(method, path, requestID string, err error, recovered bool, stack string) {
	if api.config.RequestError == nil {
		slog.Error("Ridu request failed", "request_id", requestID, "method", method, "path", path, "panic", recovered, "error", err, "cause", failureCause(err), "stack", stack)
		return
	}
	defer func() {
		if recover() != nil {
			slog.Error("Ridu RequestError callback panicked", "request_id", requestID, "method", method, "path", path, "panic", recovered, "error", err, "stack", stack)
		}
	}()
	api.config.RequestError(RequestErrorEvent{
		Time: time.Now().UTC(), RequestID: requestID, Method: method,
		Path: path, Error: err, Panic: recovered, Stack: stack,
	})
}

func (api *API) audit(request *http.Request, requestID string, actor *store.Document, action, collection, documentID string, knownCollection ...schema.CollectionSlug) {
	if api.config.Audit == nil {
		return
	}
	defer func() {
		if recover() != nil {
			api.reportRequestError(request, requestID, errors.New("audit callback panicked"), true, string(debug.Stack()))
		}
	}()
	actorID := ""
	actorCollection := schema.CollectionSlug("")
	if actor != nil {
		actorID = actor.ID
		if len(knownCollection) != 0 {
			actorCollection = knownCollection[len(knownCollection)-1]
		} else if identity := api.optionalIdentity(request); identity != nil && identity.Actor.ID == actor.ID {
			actorCollection = identity.Collection
		}
	}
	api.config.Audit(AuditEvent{
		Time: time.Now().UTC(), RequestID: requestID, ClientIP: api.clientIP(request),
		Action: action, Collection: collection, DocumentID: documentID, ActorID: actorID, ActorCollection: actorCollection,
	})
}

func (api *API) decodeValues(writer http.ResponseWriter, request *http.Request) (store.Values, error) {
	var values store.Values
	if err := api.decodeJSON(writer, request, &values); err != nil {
		return nil, err
	}
	if values == nil {
		return nil, &operationengine.Error{Code: "bad_request", Status: 400, Message: "JSON body must be an object"}
	}
	return values, nil
}

func (api *API) decodeOptionalValues(writer http.ResponseWriter, request *http.Request) (store.Values, error) {
	if request.Body == http.NoBody || request.ContentLength == 0 {
		return nil, nil
	}
	return api.decodeValues(writer, request)
}

func (api *API) decodeJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, api.config.MaxBodyBytes)
	encoded, err := io.ReadAll(request.Body)
	if err != nil {
		var maximum *http.MaxBytesError
		if errors.As(err, &maximum) {
			return &operationengine.Error{Code: "body_too_large", Status: 413, Message: fmt.Sprintf("JSON body exceeds %d bytes", maximum.Limit)}
		}
		return &operationengine.Error{Code: "bad_request", Status: 400, Message: "invalid JSON body", Cause: err}
	}
	if err := jsonlimit.Validate(encoded, jsonlimit.DefaultMaxDepth, jsonlimit.DefaultMaxTokens); err != nil {
		if errors.Is(err, jsonlimit.ErrTooDeep) || errors.Is(err, jsonlimit.ErrTooManyTokens) {
			return &operationengine.Error{Code: "body_too_complex", Status: 400, Message: "JSON body exceeds the structural complexity limit", Cause: err}
		}
		if errors.Is(err, jsonlimit.ErrDuplicateKey) {
			return &operationengine.Error{Code: "bad_request", Status: 400, Message: "JSON body contains a duplicate object key", Cause: err}
		}
		return &operationengine.Error{Code: "bad_request", Status: 400, Message: "invalid JSON body", Cause: err}
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &operationengine.Error{Code: "bad_request", Status: 400, Message: "invalid JSON body", Cause: err}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return &operationengine.Error{Code: "bad_request", Status: 400, Message: "JSON body contains trailing data"}
	}
	return nil
}

func decodeDynamicValues(encoded []byte, target *store.Values) error {
	if err := jsonlimit.Validate(encoded, jsonlimit.DefaultMaxDepth, jsonlimit.DefaultMaxTokens); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if *target == nil {
		return errors.New("JSON value must be an object")
	}
	return nil
}

type listQuery struct {
	draft           *bool
	page, limit     int
	filter          query.Expression
	sort            []query.Sort
	selectFields    []query.Path
	outputFields    []query.Path
	populate        []query.Population
	trashOnly       bool
	locale          string
	fallbackLocales []schema.LocaleCode
	disableFallback bool
	allLocales      bool
	includeAccess   bool
	// skipTotal is set by pagination=false.
	skipTotal bool
}

// decodeListQuery decodes a collection list or count query. pageRead admits
// the parameters that only shape a returned page, include-access and
// pagination; the count endpoint rejects them as unknown.
func decodeListQuery(values url.Values, collection schema.Collection, pageRead bool) (listQuery, error) {
	for key := range values {
		if strings.HasPrefix(key, "where[") {
			return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "bracket-style where[...] parameters are not supported; send the filter as one URL-encoded JSON where parameter, for example where=" + bracketWhereExample(values)}
		}
		if key != "page" && key != "limit" && key != "depth" && key != "where" && key != "select" && key != "populate" && key != "sort" && key != "trash" && key != "locale" && key != "fallback-locale" && key != "fallbackLocale" && key != "draft" && (key != "include-access" && key != "pagination" || !pageRead) {
			return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("unknown query parameter %q", key)}
		}
	}
	draft, err := decodeDraftValues(values)
	if err != nil {
		return listQuery{}, err
	}
	includeAccess, err := decodeBooleanQuery(values, "include-access", false)
	if err != nil {
		return listQuery{}, err
	}
	pagination, err := decodeBooleanQuery(values, "pagination", true)
	if err != nil {
		return listQuery{}, err
	}
	trashOnly := values.Get("trash") == "true"
	if encoded := values.Get("trash"); encoded != "" && encoded != "true" && encoded != "false" {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "trash query parameter must be true or false"}
	}
	if trashOnly && !collection.Capabilities.Trash {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "collection does not support trash"}
	}
	page, err := positiveInteger(values.Get("page"), 1, 1_000_000)
	if err != nil {
		return listQuery{}, err
	}
	limit, err := positiveInteger(values.Get("limit"), 10, 100)
	if err != nil {
		return listQuery{}, err
	}
	var filter query.Expression
	if encoded := values.Get("where"); encoded != "" {
		filter, err = decodeWhere([]byte(encoded), collection)
		if err != nil {
			return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid where query", Cause: err}
		}
	}
	sorts, err := decodeSort(values["sort"])
	if err != nil {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid sort query", Cause: err}
	}
	selection, err := decodeSelection(values.Get("select"), collection)
	if err != nil {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid select query", Cause: err}
	}
	population, err := decodePopulation(values.Get("populate"), collection)
	if err != nil {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid populate query", Cause: err}
	}
	depthPopulation, err := decodeDepthPopulation(values.Get("depth"), collection)
	if err != nil {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid depth query", Cause: err}
	}
	if len(population) != 0 && len(depthPopulation) != 0 {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "depth and populate cannot be combined"}
	}
	if len(depthPopulation) != 0 {
		population = depthPopulation
	}
	localeOptions, err := decodeLocaleQuery(values)
	if err != nil {
		return listQuery{}, err
	}
	return listQuery{draft: draft, page: page, limit: limit, filter: filter, sort: sorts, selectFields: selection.stored, outputFields: selection.output, populate: population, trashOnly: trashOnly, includeAccess: includeAccess, skipTotal: !pagination,
		locale: localeOptions.locale, fallbackLocales: localeOptions.fallbackLocales, disableFallback: localeOptions.disableFallback, allLocales: localeOptions.allLocales}, nil
}

// decodeBooleanQuery reads an optional query flag that may appear once as
// true or false.
func decodeBooleanQuery(values url.Values, name string, fallback bool) (bool, error) {
	encoded, present := values[name]
	if !present {
		return fallback, nil
	}
	if len(encoded) != 1 {
		return false, &operationengine.Error{Code: "bad_query", Status: 400, Message: name + " query parameter must be provided once"}
	}
	if encoded[0] != "true" && encoded[0] != "false" {
		return false, &operationengine.Error{Code: "bad_query", Status: 400, Message: name + " query parameter must be true or false"}
	}
	return encoded[0] == "true", nil
}

type localeQuery struct {
	locale          string
	fallbackLocales []schema.LocaleCode
	disableFallback bool
	allLocales      bool
}

func (options localeQuery) apply(request *operationengine.Request) {
	request.Locale = options.locale
	request.FallbackLocales = append([]schema.LocaleCode(nil), options.fallbackLocales...)
	request.DisableFallback = options.disableFallback
	request.AllLocales = options.allLocales
}

func (options localeQuery) public() LocaleOptions {
	return LocaleOptions{
		Locale: options.locale, FallbackLocales: append([]schema.LocaleCode(nil), options.fallbackLocales...),
		DisableFallback: options.disableFallback, AllLocales: options.allLocales,
	}
}

func decodeLocaleQuery(values url.Values) (localeQuery, error) {
	locale := strings.TrimSpace(values.Get("locale"))
	all := locale == "all" || locale == "*"
	fallbackValue := values.Get("fallback-locale")
	if alias := values.Get("fallbackLocale"); alias != "" {
		if fallbackValue != "" && fallbackValue != alias {
			return localeQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "fallback-locale and fallbackLocale must not conflict"}
		}
		fallbackValue = alias
	}
	fallbacks, disabled, err := localization.ParseFallbackQuery(fallbackValue)
	if err != nil {
		return localeQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid fallback locale query", Cause: err}
	}
	return localeQuery{locale: locale, fallbackLocales: fallbacks, disableFallback: disabled, allLocales: all}, nil
}

func decodeDraftQuery(request *http.Request) (*bool, error) {
	return decodeDraftValues(request.URL.Query())
}

func decodeDraftValues(queryValues url.Values) (*bool, error) {
	values, present := queryValues["draft"]
	if !present {
		return nil, nil
	}
	if len(values) != 1 {
		return nil, &operationengine.Error{Code: "bad_query", Status: 400, Message: "draft query must be specified once"}
	}
	if values[0] != "true" && values[0] != "false" {
		return nil, &operationengine.Error{Code: "bad_query", Status: 400, Message: "draft query must be true or false"}
	}
	draft := values[0] == "true"
	return &draft, nil
}

func decodeDepthPopulation(encoded string, collection schema.Collection) ([]query.Population, error) {
	if encoded == "" || encoded == "0" {
		return nil, nil
	}
	depth, err := strconv.Atoi(encoded)
	if err != nil || depth < 0 || depth > populationwalk.MaxDepth {
		return nil, fmt.Errorf("depth must be an integer between 0 and %d", populationwalk.MaxDepth)
	}
	if populationwalk.ReferenceFieldCount(collection.Fields) > populationwalk.MaxExplicitPaths {
		return nil, fmt.Errorf("depth population expands more than %d root relationship fields", populationwalk.MaxExplicitPaths)
	}
	fields := populationwalk.ReferenceFields(collection.Fields)
	result := make([]query.Population, len(fields))
	for index, field := range fields {
		result[index] = query.Population{Path: field.Path, Depth: depth}
	}
	return result, nil
}

// decodeSort parses sort terms. Whether a path can be sorted is the operation
// engine's decision, shared by every transport.
func decodeSort(values []string) ([]query.Sort, error) {
	if len(values) > maxSortFields {
		return nil, fmt.Errorf("sort supports at most %d fields", maxSortFields)
	}
	result := make([]query.Sort, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		direction := query.Ascending
		name := value
		if strings.HasPrefix(name, "-") {
			direction, name = query.Descending, strings.TrimPrefix(name, "-")
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("sort field %q is repeated", name)
		}
		seen[name] = struct{}{}
		path, err := query.ParsePath(name)
		if err != nil {
			return nil, err
		}
		result[index], err = query.NewSort(path, direction)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

type fieldSelection struct {
	stored []query.Path
	output []query.Path
}

func decodeSelection(encoded string, collection schema.Collection) (fieldSelection, error) {
	if encoded == "" {
		return fieldSelection{}, nil
	}
	if err := jsonlimit.Validate([]byte(encoded), 4, maxSelectionFields*2+2); err != nil {
		return fieldSelection{}, err
	}
	var selected map[string]bool
	if err := json.Unmarshal([]byte(encoded), &selected); err != nil {
		return fieldSelection{}, err
	}
	if len(selected) > maxSelectionFields {
		return fieldSelection{}, fmt.Errorf("select supports at most %d fields", maxSelectionFields)
	}
	// Preserve the difference between no select query (nil: all fields) and an
	// explicit metadata-only selection (non-nil empty: no authored fields or
	// computed output). Stored and output fields remain separate because only
	// stored paths are valid adapter projections.
	selection := fieldSelection{
		stored: make([]query.Path, 0, len(selected)),
		output: make([]query.Path, 0, len(selected)),
	}
	for name, include := range selected {
		if !include {
			continue
		}
		if selectableSystemField(collection, name) {
			continue
		}
		field, exists := topLevelField(collection, name)
		if !exists || field.Category == schema.FieldCategoryPresentation && field.Type != schema.FieldTypeJoin && field.Type != schema.FieldTypeVirtual {
			return fieldSelection{}, fmt.Errorf("select field %q is not defined", name)
		}
		path, _ := query.NewPath(name)
		if field.Type == schema.FieldTypeJoin || field.Type == schema.FieldTypeVirtual {
			selection.output = append(selection.output, path)
			continue
		}
		selection.stored = append(selection.stored, path)
	}
	return selection, nil
}

func topLevelField(collection schema.Collection, name string) (schema.Field, bool) {
	for _, field := range collection.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return schema.Field{}, false
}

func selectableSystemField(collection schema.Collection, name string) bool {
	// Document metadata is always returned by the projection stores. Accept the
	// matching generated select keys without forwarding them as value paths.
	switch name {
	case "id", "createdAt", "updatedAt":
		return true
	case "deletedAt":
		return collection.Capabilities.Trash
	case "_status":
		return collection.Versions != nil
	case "_revision":
		return collection.Versions != nil || collection.Upload != nil
	default:
		return false
	}
}

func decodePopulation(encoded string, collection schema.Collection) ([]query.Population, error) {
	if encoded == "" {
		return nil, nil
	}
	if err := jsonlimit.Validate([]byte(encoded), 16, 4096); err != nil {
		return nil, err
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return nil, err
	}
	if len(values) > populationwalk.MaxExplicitPaths {
		return nil, fmt.Errorf("populate supports at most %d fields", populationwalk.MaxExplicitPaths)
	}
	var result []query.Population
	for name, encodedValue := range values {
		path, pathError := query.ParsePath(name)
		if pathError != nil {
			return nil, pathError
		}
		field, found := populationwalk.FieldAtPath(collection.Fields, path)
		if !found || populationwalk.RelationshipDetails(field) == nil {
			return nil, fmt.Errorf("populate field %q is not a relationship", name)
		}
		if bytes.Equal(bytes.TrimSpace(encodedValue), []byte("false")) {
			continue
		}
		population := query.Population{Path: path, Depth: 1}
		if !bytes.Equal(bytes.TrimSpace(encodedValue), []byte("true")) {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(encodedValue, &object); err != nil || object == nil {
				return nil, fmt.Errorf("populate %q must be true or a select object", name)
			}
			selection := object
			selectionConfigured := true
			if !rawBooleanObject(object) {
				for key := range object {
					if key != "depth" && key != "select" {
						return nil, fmt.Errorf("populate %q has unknown option %q", name, key)
					}
				}
				if depthValue, configured := object["depth"]; configured {
					if err := json.Unmarshal(depthValue, &population.Depth); err != nil || population.Depth < 1 || population.Depth > populationwalk.MaxDepth {
						return nil, fmt.Errorf("populate %q depth must be between 1 and %d", name, populationwalk.MaxDepth)
					}
				}
				if selectValue, configured := object["select"]; configured {
					var decodedSelection map[string]json.RawMessage
					if err := json.Unmarshal(selectValue, &decodedSelection); err != nil || decodedSelection == nil || !rawBooleanObject(decodedSelection) {
						return nil, fmt.Errorf("populate %q select must be an object of booleans", name)
					}
					selection = decodedSelection
				} else {
					// A depth-only object populates the complete target. An explicit
					// empty select is metadata-only and remains distinguishable below.
					selectionConfigured = false
				}
			}
			if selectionConfigured {
				if len(selection) > maxSelectionFields {
					return nil, fmt.Errorf("populate %q select supports at most %d fields", name, maxSelectionFields)
				}
				population.Select = make([]query.Path, 0, len(selection))
				for targetName, encodedInclude := range selection {
					if !bytes.Equal(bytes.TrimSpace(encodedInclude), []byte("true")) {
						continue
					}
					targetPath, err := query.NewPath(targetName)
					if err != nil {
						return nil, err
					}
					population.Select = append(population.Select, targetPath)
				}
			}
		}
		result = append(result, population)
	}
	return result, nil
}

func rawBooleanObject(values map[string]json.RawMessage) bool {
	for _, value := range values {
		trimmed := bytes.TrimSpace(value)
		if !bytes.Equal(trimmed, []byte("true")) && !bytes.Equal(trimmed, []byte("false")) {
			return false
		}
	}
	return true
}

func positiveInteger(value string, fallback, maximum int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > maximum {
		return 0, &operationengine.Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("pagination value must be between 1 and %d", maximum)}
	}
	return parsed, nil
}

func (api *API) methodNotAllowed(writer http.ResponseWriter, requestID string, methods ...string) {
	writer.Header().Set("Allow", strings.Join(methods, ", "))
	api.writeError(writer, requestID, &operationengine.Error{Code: "method_not_allowed", Status: 405, Message: "HTTP method is not allowed"})
}

func publicErrorPayload(requestID string, err error) protocol.ErrorPayload {
	status, code, message := http.StatusInternalServerError, "internal", "internal server error"
	issues := []protocol.ValidationIssue{}
	var operationError *operationengine.Error
	if errors.As(err, &operationError) {
		status, code, message = operationError.Status, operationError.Code, operationError.Message
		issues = make([]protocol.ValidationIssue, len(operationError.Issues))
		for index, issue := range operationError.Issues {
			issues[index] = protocol.ValidationIssue{Code: issue.Code, Path: issue.Path, Message: issue.Message, Target: issue.Target, FieldID: issue.FieldID, CollectionID: issue.CollectionID, GlobalID: issue.GlobalID, Locale: issue.Locale}
		}
	} else if errors.Is(err, context.DeadlineExceeded) {
		status, code, message = http.StatusRequestTimeout, "request_timeout", "request deadline was exceeded"
	} else if errors.Is(err, context.Canceled) {
		status, code, message = http.StatusRequestTimeout, "request_canceled", "request was canceled"
	}

	if status >= http.StatusInternalServerError {
		code, message, issues = "internal", "internal server error", []protocol.ValidationIssue{}
	}
	return protocol.ErrorPayload{Code: wireErrorCode(code, status), Status: status, Message: message, RequestID: requestID, Issues: issues}
}

func (api *API) writeError(writer http.ResponseWriter, requestID string, err error) {
	payload := publicErrorPayload(requestID, err)
	if payload.Status >= http.StatusInternalServerError {
		if tracked, ok := writer.(*statusWriter); ok {
			api.reportRequestErrorDetails(tracked.method, tracked.path, requestID, err, false, "")
		}
	}
	if tracked, ok := writer.(*statusWriter); ok {
		tracked.errorCode = string(payload.Code)
		tracked.errorReason = errorReason(err)
	}
	writeJSON(writer, payload.Status, protocol.ErrorEnvelope{Error: payload})
}

// errorReason is the specific code behind a public error code, such as
// origin_denied behind access_denied. Observations report it to operators;
// responses carry only the public code.
func errorReason(err error) string {
	var operationError *operationengine.Error
	switch {
	case errors.As(err, &operationError) && operationError.Code != "":
		return operationError.Code
	case errors.Is(err, context.DeadlineExceeded):
		return "request_timeout"
	case errors.Is(err, context.Canceled):
		return "request_canceled"
	}
	return string(protocol.ErrorInternal)
}

// reason reports the specific failure code, or the public code when no more
// specific one was recorded.
func (writer *statusWriter) reason() string {
	if writer.errorReason != "" {
		return writer.errorReason
	}
	return writer.errorCode
}

func wireErrorCode(code string, status int) protocol.ErrorCode {
	switch code {
	case "validation":
		return protocol.ErrorValidation
	case "access_denied", "field_access_denied":
		return protocol.ErrorAccess
	case "not_found":
		return protocol.ErrorNotFound
	case "conflict", "block_recovery_required":
		return protocol.ErrorConflict
	case "delete_restricted":
		return protocol.ErrorDeleteRestricted
	case "rate_limited":
		return protocol.ErrorRateLimited
	case "email_not_verified":
		return protocol.ErrorEmailNotVerified
	case "auth_feature_disabled":
		return protocol.ErrorAuthFeatureDisabled
	case "invalid_auth_token":
		return protocol.ErrorInvalidAuthToken
	case "invalid_credential":
		return protocol.ErrorInvalidCredential
	case "rejected":
		return protocol.ErrorRejected
	case "invalid_preview_token":
		return protocol.ErrorInvalidPreviewToken
	case "selection_too_large":
		return protocol.ErrorSelectionTooLarge
	case "bad_query":
		return protocol.ErrorBadQuery
	case "publish_required":
		return protocol.ErrorPublishRequired
	}
	// Other codes fall back to the category their status names, so the code
	// never contradicts the status. ErrorReason keeps the specific code.
	switch {
	case status == http.StatusForbidden:
		return protocol.ErrorAccess
	case status == http.StatusNotFound:
		return protocol.ErrorNotFound
	case status == http.StatusConflict:
		return protocol.ErrorConflict
	case status >= 400 && status < 500:
		return protocol.ErrorBadRequest
	}
	return protocol.ErrorInternal
}

func versionJSON(version store.Version) protocol.DocumentVersion[json.RawMessage] {
	return protocol.DocumentVersion[json.RawMessage]{
		ID: version.ID, DocumentID: version.DocumentID, Revision: version.Revision,
		Status: string(version.Status), Snapshot: documentJSON(version.Snapshot),
		CreatedAt: version.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func versionsJSON(versions []store.Version) []protocol.DocumentVersion[json.RawMessage] {
	result := make([]protocol.DocumentVersion[json.RawMessage], len(versions))
	for index, version := range versions {
		result[index] = versionJSON(version)
	}
	return result
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func newRequestID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(value[:])
}
