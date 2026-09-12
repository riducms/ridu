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
	Manifest                     schema.Manifest
	ManifestForRequest           func(context.Context, *AuthIdentity) (schema.Snapshot, error)
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
	CreateAuthUser               func(context.Context, string, store.Values, string, *AuthIdentity) (store.Document, error)
	CreateAuthUserLocalized      func(context.Context, string, store.Values, string, *AuthIdentity, LocaleOptions) (store.Document, error)
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
	SchedulePublish              func(context.Context, string, string, time.Time, int, *AuthIdentity) (store.ScheduledPublish, error)
	ScheduledPublishes           func(context.Context, string, string, *AuthIdentity) ([]store.ScheduledPublish, error)
	CancelScheduledPublish       func(context.Context, string, string, string, *AuthIdentity) error
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
	Upload                       func(context.Context, string, string, io.Reader, store.Values, *AuthIdentity, bool) (store.Document, error)
	RemoteUpload                 func(context.Context, string, string, store.Values, *AuthIdentity) (store.Document, error)
	UploadLocalized              func(context.Context, string, string, io.Reader, store.Values, *AuthIdentity, LocaleOptions, bool) (store.Document, error)
	RemoteUploadLocalized        func(context.Context, string, string, store.Values, *AuthIdentity, LocaleOptions) (store.Document, error)
	UpdateUploadImage            func(context.Context, string, string, float64, float64, float64, float64, float64, float64, int, *AuthIdentity) (store.Document, error)
	Duplicate                    func(context.Context, string, string, store.Values, *AuthIdentity, LocaleOptions) (store.Document, error)
	OpenUpload                   func(context.Context, string, string, *AuthIdentity) (io.ReadCloser, storage.Object, error)
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
	config           Config
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
	snapshot := config.Manifest.Snapshot()
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
				api.config.Observe(RequestObservation{Time: started.UTC(), RequestID: requestID, Method: request.Method, Path: request.URL.Path, Status: tracked.status, ErrorCode: tracked.errorCode, ResponseBytes: tracked.bytes, Duration: time.Since(started)})
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
	if strings.HasPrefix(request.URL.Path, "/admin") && api.config.AdminAssets != nil {
		api.admin(writer, request)
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
		snapshot := api.config.Manifest.Snapshot()
		if api.config.ManifestForRequest != nil {
			var err error
			snapshot, err = api.config.ManifestForRequest(request.Context(), api.optionalIdentity(request))
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
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
	writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(document)})
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
	localeOptions, err := decodeLocaleQuery(request)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	identity := api.optionalIdentity(request)
	capabilities, err := api.config.Engine.Capabilities(request.Context(), operationengine.CapabilitiesRequest{
		Collection: collectionKey, ID: input.ID, Data: input.Data,
		Actor: identityActor(identity), ActorCollection: identityCollection(identity), TrashOnly: input.Trash,
		Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
		DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales,
	})
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusOK, accessCapabilitiesJSON(capabilities))
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
	localeOptions, err := decodeLocaleQuery(request)
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
		Fields: fields,
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
			options, err := decodeListQuery(request, global)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			result, err := api.config.Engine.Execute(request.Context(), operationengine.Request{
				Operation: operation.Read, Collection: key, ID: slug, Actor: actor, ActorCollection: actorCollection,
				Select: options.selectFields, OutputFields: options.outputFields, Populate: options.populate,
				Locale: options.locale, FallbackLocales: options.fallbackLocales,
				DisableFallback: options.disableFallback, AllLocales: options.allLocales,
			})
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(*result.Document)})
		case http.MethodPatch:
			values, err := api.decodeValues(writer, request)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			localeOptions, err := decodeLocaleQuery(request)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			operationRequest := operationengine.Request{Operation: operation.Update, Collection: key, ID: slug, Data: values, Actor: actor, ActorCollection: actorCollection, ExpectedRevision: revisionHeader(request)}
			localeOptions.apply(&operationRequest)
			result, err := api.config.Engine.Execute(request.Context(), operationRequest)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(*result.Document)})
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
		localeOptions, err := decodeLocaleQuery(request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		versionLocaleOptions := operationengine.LocalizationOptions{
			Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
			DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales, ActorCollection: actorCollection,
		}
		if len(segments) == 3 {
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
	case "publish", "unpublish":
		if request.Method != http.MethodPost || len(segments) != 2 {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		kind := operation.Publish
		if action == "unpublish" {
			kind = operation.Unpublish
		}
		values, err := api.decodeOptionalValues(writer, request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		localeOptions, err := decodeLocaleQuery(request)
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
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(*result.Document)})
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
		localeOptions, localeError := decodeLocaleQuery(request)
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
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(*result.Document)})
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

func (api *API) admin(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/admin")
	if path == "" || path == "/" {
		writer.Header().Set("Cache-Control", "private, no-store")
		request.URL.Path = "/"
		http.FileServer(http.FS(api.config.AdminAssets)).ServeHTTP(writer, request)
		return
	}
	assetPath := strings.TrimPrefix(path, "/")
	if info, err := fs.Stat(api.config.AdminAssets, assetPath); err == nil && !info.IsDir() {
		if strings.HasPrefix(assetPath, "assets/") {
			writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			writer.Header().Set("Cache-Control", "public, max-age=3600")
		}
		request.URL.Path = path
		http.FileServer(http.FS(api.config.AdminAssets)).ServeHTTP(writer, request)
		return
	}
	index, err := fs.ReadFile(api.config.AdminAssets, "index.html")
	if err != nil {
		http.Error(writer, "admin assets are unavailable", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(index)
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
			options, err := decodeListQuery(request, collection)
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			result, err := api.config.Engine.Execute(request.Context(), operationengine.Request{
				Operation: operation.Read, Collection: segments[0], Filter: options.filter,
				Page: options.page, Limit: options.limit, Actor: actor, ActorCollection: actorCollection, Sort: options.sort,
				Select: options.selectFields, OutputFields: options.outputFields, Populate: options.populate, TrashOnly: options.trashOnly,
				Locale: options.locale, FallbackLocales: options.fallbackLocales,
				DisableFallback: options.disableFallback, AllLocales: options.allLocales,
			})
			if err != nil {
				api.writeError(writer, requestID, err)
				return
			}
			page := result.Page
			totalPages := 0
			if page.Total > 0 {
				totalPages = (page.Total + page.Limit - 1) / page.Limit
			}
			documents := make([]map[string]any, len(page.Documents))
			for index, document := range page.Documents {
				documents[index] = documentJSON(document)
			}
			writeJSON(writer, http.StatusOK, protocol.PageEnvelope[map[string]any]{
				Docs: documents,
				Pagination: protocol.Pagination{
					Page: page.Page, Limit: page.Limit, TotalDocs: page.Total, TotalPages: totalPages,
					HasNextPage: page.Page < totalPages, HasPrevPage: page.Page > 1,
				},
			})
		case http.MethodPost:
			if collection.Auth != nil {
				api.authCollectionCreateError(writer, requestID, collection)
				return
			}
			if collection.Upload != nil && strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/form-data") {
				api.createUpload(writer, request, requestID, collection, identity)
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
			localeOptions, err := decodeLocaleQuery(request)
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
			writeJSON(writer, http.StatusCreated, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(*result.Document)})
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
		options, err := decodeListQuery(request, collection)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		result, err := api.config.Engine.Execute(request.Context(), operationengine.Request{
			Operation: operation.Read, Collection: segments[0], Filter: options.filter,
			Page: 1, Limit: 1, Actor: actor, ActorCollection: actorCollection, TrashOnly: options.trashOnly,
			Locale: options.locale, FallbackLocales: options.fallbackLocales,
			DisableFallback: options.disableFallback, AllLocales: options.allLocales,
		})
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.CountEnvelope{TotalDocs: result.Page.Total})
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
	id := segments[1]
	switch request.Method {
	case http.MethodGet:
		options, err := decodeListQuery(request, collection)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		result, err := api.config.Engine.Execute(request.Context(), operationengine.Request{
			Operation: operation.Read, Collection: segments[0], ID: id, Actor: actor, ActorCollection: actorCollection,
			Select: options.selectFields, OutputFields: options.outputFields, Populate: options.populate,
			Locale: options.locale, FallbackLocales: options.fallbackLocales,
			DisableFallback: options.disableFallback, AllLocales: options.allLocales,
		})
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(*result.Document)})
		api.audit(request, requestID, actor, "read", segments[0], result.Document.ID)
	case http.MethodPatch:
		values, err := api.decodeValues(writer, request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		localeOptions, err := decodeLocaleQuery(request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		operationRequest := operationengine.Request{Operation: operation.Update, Collection: segments[0], ID: id, Data: values, Actor: actor, ActorCollection: actorCollection, ExpectedRevision: revisionHeader(request)}
		localeOptions.apply(&operationRequest)
		result, err := api.config.Engine.Execute(request.Context(), operationRequest)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(*result.Document)})
	case http.MethodDelete:
		localeOptions, err := decodeLocaleQuery(request)
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
	localeOptions, err := decodeLocaleQuery(request)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	result, err := api.config.Engine.Execute(request.Context(), operationengine.Request{
		Operation: operation.Read, Collection: string(collection.Slug), Page: 1, Limit: 100,
		Actor: actor, ActorCollection: actorCollection, TrashOnly: true, Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
		DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales,
	})
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	if result.Page.Total > 100 {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "empty trash supports at most 100 documents per atomic operation"})
		return
	}
	if result.Page.Total == 0 {
		writeJSON(writer, http.StatusOK, protocol.BulkEnvelope[map[string]any]{Docs: []map[string]any{}})
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
	documents := make([]map[string]any, len(results))
	for index, item := range results {
		documents[index] = documentJSON(*item.Document)
		api.audit(request, requestID, actor, "empty-trash", string(collection.Slug), item.Document.ID)
	}
	writeJSON(writer, http.StatusOK, protocol.BulkEnvelope[map[string]any]{Docs: documents})
}

func (api *API) createRemoteUpload(writer http.ResponseWriter, request *http.Request, requestID string, collection schema.Collection, identity *AuthIdentity) {
	actor := identityActor(identity)
	if request.Method != http.MethodPost {
		api.methodNotAllowed(writer, requestID, http.MethodPost)
		return
	}
	if collection.Upload == nil || api.config.RemoteUpload == nil && api.config.RemoteUploadLocalized == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "remote upload was not found"})
		return
	}
	if collection.Auth != nil {
		api.authCollectionCreateError(writer, requestID, collection)
		return
	}
	var input struct {
		URL  string       `json:"url"`
		Data store.Values `json:"data"`
	}
	if err := api.decodeJSON(writer, request, &input); err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	localeOptions, err := decodeLocaleQuery(request)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	var document store.Document
	if api.config.RemoteUploadLocalized != nil {
		document, err = api.config.RemoteUploadLocalized(request.Context(), string(collection.Slug), input.URL, input.Data, identity, localeOptions.public())
	} else {
		document, err = api.config.RemoteUpload(request.Context(), string(collection.Slug), input.URL, input.Data, identity)
	}
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusCreated, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(document)})
	api.audit(request, requestID, actor, "remote-upload", string(collection.Slug), document.ID)
}

func (api *API) documentAction(writer http.ResponseWriter, request *http.Request, requestID string, collection schema.Collection, segments []string, actor *store.Document, identity *AuthIdentity) {
	collectionName, id, action := segments[0], segments[1], segments[2]
	actorCollection := identityCollection(identity)
	if len(segments) == 3 && action == "copy-locale" {
		api.copyLocale(writer, request, requestID, collectionName, id, actor, actorCollection, "copy-locale")
		return
	}
	if len(segments) == 3 && action == "image" {
		if collection.Upload == nil || api.config.UpdateUploadImage == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "image workflow was not found"})
			return
		}
		if request.Method != http.MethodPatch {
			api.methodNotAllowed(writer, requestID, http.MethodPatch)
			return
		}
		var input struct {
			FocalX     float64 `json:"focalX"`
			FocalY     float64 `json:"focalY"`
			CropX      float64 `json:"cropX"`
			CropY      float64 `json:"cropY"`
			CropWidth  float64 `json:"cropWidth"`
			CropHeight float64 `json:"cropHeight"`
		}
		if err := api.decodeJSON(writer, request, &input); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		if input.FocalX < 0 || input.FocalX > 100 || input.FocalY < 0 || input.FocalY > 100 {
			api.writeError(writer, requestID, &operationengine.Error{Code: "validation", Status: 422, Message: "focal coordinates must be between 0 and 100"})
			return
		}
		document, err := api.config.UpdateUploadImage(request.Context(), collectionName, id, input.FocalX, input.FocalY, input.CropX, input.CropY, input.CropWidth, input.CropHeight, revisionHeader(request), identity)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(document)})
		api.audit(request, requestID, actor, "update-upload-image", collectionName, id)
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
		localeOptions, err := decodeLocaleQuery(request)
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
		writeJSON(writer, http.StatusCreated, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(duplicated)})
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
		localeOptions, err := decodeLocaleQuery(request)
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
			writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(*result.Document)})
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
		localeOptions, err := decodeLocaleQuery(request)
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
		writeJSON(writer, http.StatusOK, protocol.JoinMutationEnvelope[map[string]any]{
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
		localeOptions, err := decodeLocaleQuery(request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		versionLocaleOptions := operationengine.LocalizationOptions{
			Locale: localeOptions.locale, FallbackLocales: localeOptions.fallbackLocales,
			DisableFallback: localeOptions.disableFallback, AllLocales: localeOptions.allLocales, ActorCollection: actorCollection,
		}
		if len(segments) == 4 {
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
		api.scheduledPublish(writer, request, requestID, collectionName, id, segments, actor, identity)
	case "publish", "unpublish":
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		kind := operation.Publish
		if action == "unpublish" {
			kind = operation.Unpublish
		}
		values, err := api.decodeOptionalValues(writer, request)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		localeOptions, err := decodeLocaleQuery(request)
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
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(*result.Document)})
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
		localeOptions, localeError := decodeLocaleQuery(request)
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
		writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(*result.Document)})
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
	writeJSON(writer, http.StatusOK, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(document)})
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

func (api *API) scheduledPublish(writer http.ResponseWriter, request *http.Request, requestID, collection, documentID string, segments []string, actor *store.Document, identity *AuthIdentity) {
	if api.config.SchedulePublish == nil || api.config.ScheduledPublishes == nil || api.config.CancelScheduledPublish == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publishing is unavailable"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		if len(segments) != 3 {
			api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publish was not found"})
			return
		}
		jobs, err := api.config.ScheduledPublishes(request.Context(), collection, documentID, identity)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		result := make([]protocol.ScheduledPublish, len(jobs))
		for index, job := range jobs {
			result[index] = scheduledPublishJSON(job)
		}
		writeJSON(writer, http.StatusOK, protocol.ScheduledPublishesEnvelope{ScheduledPublishes: result})
	case http.MethodPost:
		if len(segments) != 3 {
			api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publish route was not found"})
			return
		}
		var input struct {
			RunAt string `json:"runAt"`
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
		job, err := api.config.SchedulePublish(request.Context(), collection, documentID, runAt, revisionHeader(request), identity)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusCreated, protocol.ScheduledPublishEnvelope{ScheduledPublish: scheduledPublishJSON(job)})
		api.audit(request, requestID, actor, "schedule-publish", collection, documentID)
	case http.MethodDelete:
		if len(segments) != 4 {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "scheduled publish id is required"})
			return
		}
		if err := api.config.CancelScheduledPublish(request.Context(), collection, documentID, segments[3], identity); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DeleteEnvelope{ID: segments[3], Deleted: true})
		api.audit(request, requestID, actor, "cancel-scheduled-publish", collection, documentID)
	default:
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodPost, http.MethodDelete)
	}
}

func scheduledPublishJSON(job store.ScheduledPublish) protocol.ScheduledPublish {
	return protocol.ScheduledPublish{
		ID: job.ID, DocumentID: job.DocumentID, ExpectedRevision: job.ExpectedRevision,
		RunAt: job.RunAt.UTC().Format(time.RFC3339Nano), Attempts: job.Attempts,
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
	localeOptions, err := decodeLocaleQuery(request)
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
	documents := make([]map[string]any, len(results))
	for index, result := range results {
		documents[index] = documentJSON(*result.Document)
		api.audit(request, requestID, actor, "bulk-"+input.Action, string(collection.Slug), result.Document.ID)
	}
	writeJSON(writer, http.StatusOK, protocol.BulkEnvelope[map[string]any]{Docs: documents})
}

func revisionHeader(request *http.Request) int {
	encoded := strings.Trim(request.Header.Get("If-Match"), "\"")
	revision, _ := strconv.Atoi(encoded)
	return revision
}

func (api *API) createUpload(writer http.ResponseWriter, request *http.Request, requestID string, collection schema.Collection, identity *AuthIdentity) {
	actor := identityActor(identity)
	if api.config.Upload == nil && api.config.UploadLocalized == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "upload_unavailable", Status: 503, Message: "upload storage is unavailable"})
		return
	}
	releaseAdmission := func() {}
	if api.config.AcquireUpload != nil {
		var admissionError error
		releaseAdmission, admissionError = api.config.AcquireUpload(request.Context(), string(collection.Slug))
		if admissionError != nil {
			api.writeError(writer, requestID, admissionError)
			return
		}
	}
	defer releaseAdmission()
	multipartLimit := collection.Upload.MaxFileSize + 1<<20
	request.Body = http.MaxBytesReader(writer, request.Body, multipartLimit)
	parseError := request.ParseMultipartForm(maxMultipartMemory)
	if request.MultipartForm != nil {
		defer request.MultipartForm.RemoveAll()
	}
	if parseError != nil {
		var maximum *http.MaxBytesError
		if errors.As(parseError, &maximum) {
			api.writeError(writer, requestID, &operationengine.Error{Code: "body_too_large", Status: 413, Message: fmt.Sprintf("multipart upload exceeds %d bytes", maximum.Limit)})
			return
		}
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "invalid multipart upload", Cause: parseError})
		return
	}
	file, header, err := request.FormFile("file")
	if err != nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "validation", Status: 422, Message: "multipart field \"file\" is required", Cause: err})
		return
	}
	defer file.Close()
	values := store.Values{}
	if encoded := request.FormValue("data"); encoded != "" {
		if err := decodeDynamicValues([]byte(encoded), &values); err != nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "multipart field \"data\" must be a JSON object", Cause: err})
			return
		}
	}
	localeOptions, err := decodeLocaleQuery(request)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	var document store.Document
	if api.config.UploadLocalized != nil {
		document, err = api.config.UploadLocalized(request.Context(), string(collection.Slug), header.Filename, file, values, identity, localeOptions.public(), api.config.AcquireUpload != nil)
	} else {
		document, err = api.config.Upload(request.Context(), string(collection.Slug), header.Filename, file, values, identity, api.config.AcquireUpload != nil)
	}
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusCreated, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(document)})
	api.audit(request, requestID, actor, "upload", string(collection.Slug), document.ID)
}

func (api *API) upload(writer http.ResponseWriter, request *http.Request, requestID string) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodHead)
		return
	}
	remainder := strings.TrimPrefix(request.URL.Path, "/api/uploads/")
	segments := strings.SplitN(remainder, "/", 2)
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" || api.config.OpenUpload == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "upload was not found"})
		return
	}
	key := remainder
	if strings.HasPrefix(segments[1], "ridu/") {
		key = segments[1]
	}
	reader, object, err := api.config.OpenUpload(request.Context(), segments[0], key, api.optionalIdentity(request))
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
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
		var credentials struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := api.decodeJSON(writer, request, &credentials); err != nil {
			api.writeError(writer, requestID, err)
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
		http.SetCookie(writer, &http.Cookie{Name: sessionCookie, Value: session.Token, Path: "/", HttpOnly: true, Secure: api.config.SecureCookies, SameSite: http.SameSiteLaxMode, Expires: session.ExpiresAt})
		writeJSON(writer, http.StatusOK, sessionEnvelope(session))
		api.audit(request, requestID, &session.User, "login", collection, session.User.ID, session.Collection)
		return
	}
	if path == "logout-all" {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		cookie, err := request.Cookie(sessionCookie)
		if err != nil || api.config.LogoutAll == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		identity := api.optionalIdentity(request)
		if err := api.config.LogoutAll(request.Context(), cookie.Value); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		api.clearSessionCookie(writer)
		writeJSON(writer, http.StatusOK, protocol.LogoutEnvelope{LoggedOut: true})
		api.audit(request, requestID, identityActor(identity), "logout_all", "", "", identityCollection(identity))
		return
	}
	if path == "change-password" {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		cookie, err := request.Cookie(sessionCookie)
		if err != nil || api.config.ChangePassword == nil {
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
		if err := api.config.ChangePassword(request.Context(), cookie.Value, input.CurrentPassword, input.Password); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		api.clearSessionCookie(writer)
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
		cookie, _ := request.Cookie(sessionCookie)
		if cookie != nil && api.config.Logout != nil {
			if err := api.config.Logout(request.Context(), cookie.Value); err != nil {
				api.writeError(writer, requestID, err)
				return
			}
		}
		api.clearSessionCookie(writer)
		writeJSON(writer, http.StatusOK, protocol.LogoutEnvelope{LoggedOut: true})
		api.audit(request, requestID, identityActor(identity), "logout", "", "", identityCollection(identity))
		return
	}
	if path == "sessions" {
		if request.Method != http.MethodGet {
			api.methodNotAllowed(writer, requestID, http.MethodGet)
			return
		}
		cookie, err := request.Cookie(sessionCookie)
		if err != nil || api.config.Sessions == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		sessions, err := api.config.Sessions(request.Context(), cookie.Value)
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
		cookie, err := request.Cookie(sessionCookie)
		if err != nil || api.config.RevokeSession == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		identity := api.optionalIdentity(request)
		if err := api.config.RevokeSession(request.Context(), cookie.Value, sessionID); err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, protocol.DeleteEnvelope{ID: sessionID, Deleted: true})
		api.audit(request, requestID, identityActor(identity), "session_revoke", "", sessionID, identityCollection(identity))
		return
	}
	if path == "refresh" {
		if request.Method != http.MethodPost {
			api.methodNotAllowed(writer, requestID, http.MethodPost)
			return
		}
		cookie, err := request.Cookie(sessionCookie)
		if err != nil || api.config.RotateSession == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		session, err := api.config.RotateSession(request.Context(), cookie.Value)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		http.SetCookie(writer, &http.Cookie{Name: sessionCookie, Value: session.Token, Path: "/", HttpOnly: true, Secure: api.config.SecureCookies, SameSite: http.SameSiteLaxMode, Expires: session.ExpiresAt})
		writeJSON(writer, http.StatusOK, sessionEnvelope(session))
		return
	}
	if path == "me" {
		if request.Method != http.MethodGet {
			api.methodNotAllowed(writer, requestID, http.MethodGet)
			return
		}
		cookie, err := request.Cookie(sessionCookie)
		if err != nil || api.config.Session == nil {
			api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
			return
		}
		session, err := api.config.Session(request.Context(), cookie.Value)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
		writeJSON(writer, http.StatusOK, sessionEnvelope(session))
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
	if api.config.CreateAuthUser == nil && api.config.CreateAuthUserLocalized == nil {
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
	localeOptions, err := decodeLocaleQuery(request)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	var document store.Document
	if api.config.CreateAuthUserLocalized != nil {
		document, err = api.config.CreateAuthUserLocalized(request.Context(), collection, input.Data, input.Password, identity, localeOptions.public())
	} else {
		document, err = api.config.CreateAuthUser(request.Context(), collection, input.Data, input.Password, identity)
	}
	if err != nil {
		api.writeError(writer, requestID, err)
		return
	}
	writeJSON(writer, http.StatusCreated, protocol.DocumentEnvelope[map[string]any]{Doc: documentJSON(document)})
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
	cookie, err := request.Cookie(sessionCookie)
	if err != nil {
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
		keys, err := api.config.APIKeys(request.Context(), cookie.Value)
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
			expiresAt, err = time.Parse(time.RFC3339, input.ExpiresAt)
			if err != nil {
				api.writeError(writer, requestID, &operationengine.Error{Code: "validation", Status: 422, Message: "API key expiry must be an RFC 3339 timestamp"})
				return
			}
		}
		key, err := api.config.CreateAPIKey(request.Context(), cookie.Value, input.Name, expiresAt)
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
	cookie, err := request.Cookie(sessionCookie)
	if err != nil || api.config.RevokeAPIKey == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"})
		return
	}
	identity := api.optionalIdentity(request)
	if err := api.config.RevokeAPIKey(request.Context(), cookie.Value, id); err != nil {
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

func sessionEnvelope(session AuthSession) protocol.SessionEnvelope[map[string]any] {
	return protocol.SessionEnvelope[map[string]any]{Session: protocol.AuthSession[map[string]any]{
		ID: session.ID, Collection: string(session.Collection), User: documentJSON(session.User),
		ExpiresAt: session.ExpiresAt.Format(time.RFC3339Nano),
	}}
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

func (api *API) optionalIdentity(request *http.Request) *AuthIdentity {
	cookie, err := request.Cookie(sessionCookie)
	if err == nil && api.config.Session != nil {
		session, sessionError := api.config.Session(request.Context(), cookie.Value)
		if sessionError == nil {
			return &AuthIdentity{Collection: session.Collection, Actor: session.User}
		}
	}
	if api.config.AuthenticateAPIKey != nil {
		scheme, credential, found := strings.Cut(request.Header.Get("Authorization"), " ")
		if found && strings.EqualFold(scheme, "Bearer") {
			identity, authError := api.config.AuthenticateAPIKey(request.Context(), strings.TrimSpace(credential))
			if authError == nil {
				return &identity
			}
		}
	}
	if api.config.Session != nil {
		scheme, credential, found := strings.Cut(request.Header.Get("Authorization"), " ")
		if found && strings.EqualFold(scheme, "Session") {
			session, sessionError := api.config.Session(request.Context(), strings.TrimSpace(credential))
			if sessionError == nil {
				return &AuthIdentity{Collection: session.Collection, Actor: session.User}
			}
		}
	}
	if api.config.AuthenticateExternal != nil {
		identity, authError := api.config.AuthenticateExternal(request.Context(), map[string][]string(request.Header.Clone()))
		if authError == nil {
			return &identity
		}
	}
	return nil
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

func (api *API) reportRequestError(request *http.Request, requestID string, err error, recovered bool, stack string) {
	api.reportRequestErrorDetails(request.Method, request.URL.Path, requestID, err, recovered, stack)
}

func (api *API) reportRequestErrorDetails(method, path, requestID string, err error, recovered bool, stack string) {
	if api.config.RequestError == nil {
		slog.Error("Ridu request failed", "request_id", requestID, "method", method, "path", path, "panic", recovered, "error", err, "stack", stack)
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
}

func decodeListQuery(request *http.Request, collection schema.Collection) (listQuery, error) {
	for key := range request.URL.Query() {
		if key != "page" && key != "limit" && key != "depth" && key != "where" && key != "select" && key != "populate" && key != "sort" && key != "trash" && key != "locale" && key != "fallback-locale" && key != "fallbackLocale" {
			return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("unknown query parameter %q", key)}
		}
	}
	trashOnly := request.URL.Query().Get("trash") == "true"
	if encoded := request.URL.Query().Get("trash"); encoded != "" && encoded != "true" && encoded != "false" {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "trash query parameter must be true or false"}
	}
	if trashOnly && !collection.Capabilities.Trash {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "collection does not support trash"}
	}
	page, err := positiveInteger(request.URL.Query().Get("page"), 1, 1_000_000)
	if err != nil {
		return listQuery{}, err
	}
	limit, err := positiveInteger(request.URL.Query().Get("limit"), 10, 100)
	if err != nil {
		return listQuery{}, err
	}
	var filter query.Expression
	if encoded := request.URL.Query().Get("where"); encoded != "" {
		filter, err = decodeWhere([]byte(encoded), collection)
		if err != nil {
			return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid where query", Cause: err}
		}
	}
	sorts, err := decodeSort(request.URL.Query()["sort"], collection)
	if err != nil {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid sort query", Cause: err}
	}
	selection, err := decodeSelection(request.URL.Query().Get("select"), collection)
	if err != nil {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid select query", Cause: err}
	}
	population, err := decodePopulation(request.URL.Query().Get("populate"), collection)
	if err != nil {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid populate query", Cause: err}
	}
	depthPopulation, err := decodeDepthPopulation(request.URL.Query().Get("depth"), collection)
	if err != nil {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid depth query", Cause: err}
	}
	if len(population) != 0 && len(depthPopulation) != 0 {
		return listQuery{}, &operationengine.Error{Code: "bad_query", Status: 400, Message: "depth and populate cannot be combined"}
	}
	if len(depthPopulation) != 0 {
		population = depthPopulation
	}
	localeOptions, err := decodeLocaleQuery(request)
	if err != nil {
		return listQuery{}, err
	}
	return listQuery{page: page, limit: limit, filter: filter, sort: sorts, selectFields: selection.stored, outputFields: selection.output, populate: population, trashOnly: trashOnly,
		locale: localeOptions.locale, fallbackLocales: localeOptions.fallbackLocales, disableFallback: localeOptions.disableFallback, allLocales: localeOptions.allLocales}, nil
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

func decodeLocaleQuery(request *http.Request) (localeQuery, error) {
	locale := strings.TrimSpace(request.URL.Query().Get("locale"))
	all := locale == "all" || locale == "*"
	fallbackValue := request.URL.Query().Get("fallback-locale")
	if alias := request.URL.Query().Get("fallbackLocale"); alias != "" {
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
	values, present := request.URL.Query()["draft"]
	if !present {
		return nil, nil
	}
	if len(values) != 1 {
		return nil, &operationengine.Error{Code: "bad_query", Status: 400, Message: "draft query must be specified once"}
	}
	draft, err := strconv.ParseBool(values[0])
	if err != nil {
		return nil, &operationengine.Error{Code: "bad_query", Status: 400, Message: "invalid draft query", Cause: err}
	}
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
	fields := populationwalk.ReferenceFields(collection.Fields)
	if len(fields) > populationwalk.MaxExplicitPaths {
		return nil, fmt.Errorf("depth population expands more than %d root relationship fields", populationwalk.MaxExplicitPaths)
	}
	result := make([]query.Population, len(fields))
	for index, field := range fields {
		result[index] = query.Population{Path: field.Path, Depth: depth}
	}
	return result, nil
}

func decodeSort(values []string, collection schema.Collection) ([]query.Sort, error) {
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
		if name != "id" && name != "createdAt" && name != "updatedAt" {
			field, many, exists := schemaFieldAtPath(collection, path)
			if !exists || many || field.Type == schema.FieldTypeGroup || field.Type == schema.FieldTypeArray || field.Type == schema.FieldTypeBlocks || field.Type == schema.FieldTypeJSON || field.Plugin != nil {
				return nil, fmt.Errorf("sort field %q is not defined or sortable", name)
			}
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
	case "_status", "_revision":
		return collection.Versions != nil
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

func (api *API) writeError(writer http.ResponseWriter, requestID string, err error) {
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
		// RequestError is an explicitly trusted diagnostic sink; response bodies
		// retain only the stable redacted public envelope below.
		if tracked, ok := writer.(*statusWriter); ok {
			api.reportRequestErrorDetails(tracked.method, tracked.path, requestID, err, false, "")
		}
		code, message, issues = "internal", "internal server error", []protocol.ValidationIssue{}
	}
	wireCode := wireErrorCode(code, status)
	if tracked, ok := writer.(*statusWriter); ok {
		tracked.errorCode = string(wireCode)
	}
	writeJSON(writer, status, protocol.ErrorEnvelope{Error: protocol.ErrorPayload{
		Code: wireCode, Status: status, Message: message, RequestID: requestID, Issues: issues,
	}})
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
	case "invalid_preview_token":
		return protocol.ErrorInvalidPreviewToken
	case "selection_too_large":
		return protocol.ErrorSelectionTooLarge
	}
	if status >= 400 && status < 500 {
		return protocol.ErrorBadRequest
	}
	return protocol.ErrorInternal
}

func documentJSON(document store.Document) map[string]any {
	result := make(map[string]any, len(document.Values)+4)
	result["id"] = document.ID
	result["createdAt"] = document.CreatedAt.UTC().Format(time.RFC3339Nano)
	result["updatedAt"] = document.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if document.DeletedAt != nil {
		result["deletedAt"] = document.DeletedAt.UTC().Format(time.RFC3339Nano)
	}
	if document.Status != "" {
		result["_status"] = document.Status
		result["_revision"] = document.Revision
	}
	if len(document.LocalizationSources) > 0 {
		sources := make(map[string]string, len(document.LocalizationSources))
		for path, locale := range document.LocalizationSources {
			sources[path] = string(locale)
		}
		result["_localization"] = map[string]any{"sources": sources}
	}
	for name, value := range document.Values {
		result[name] = valueJSON(value)
	}
	return result
}

func versionJSON(version store.Version) map[string]any {
	return map[string]any{
		"ID": version.ID, "DocumentID": version.DocumentID, "Revision": version.Revision,
		"Status": version.Status, "Snapshot": documentJSON(version.Snapshot),
		"CreatedAt": version.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func versionsJSON(versions []store.Version) []map[string]any {
	result := make([]map[string]any, len(versions))
	for index, version := range versions {
		result[index] = versionJSON(version)
	}
	return result
}

func valueJSON(value store.Value) any {
	switch value.Kind() {
	case store.ValueNull:
		return nil
	case store.ValueString:
		text, _ := value.StringValue()
		return text
	case store.ValueObject:
		result := make(map[string]any, value.Len())
		for name, child := range value.Entries() {
			result[name] = valueJSON(child)
		}
		return result
	case store.ValueDocument:
		document, _ := value.CopyDocument()
		return documentJSON(document)
	case store.ValueNumber:
		number, _ := value.NumberValue()
		return number
	case store.ValueBoolean:
		boolean, _ := value.BooleanValue()
		return boolean
	case store.ValueList:
		result := make([]any, 0, value.Len())
		for child := range value.Elements() {
			result = append(result, valueJSON(child))
		}
		return result
	default:
		return nil
	}
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
