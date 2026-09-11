package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/adminassets"
	"github.com/riducms/ridu/internal/httpapi"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

// HandlerOptions configures the HTTP API, embedded admin, and background job runner.
type HandlerOptions struct {
	developmentReadiness       bool
	allowUnverifiableReadiness bool
	// AdminAssets overrides the framework's embedded admin asset filesystem.
	AdminAssets fs.FS
	// MaxBodyBytes limits decoded request bodies. Zero uses the framework default.
	MaxBodyBytes int64
	// SecureCookies restricts auth cookies to HTTPS requests.
	SecureCookies bool
	// AllowedOrigins lists browser origins permitted by CORS.
	AllowedOrigins []string
	// AllowedRequestHeaders appends application-owned CORS request headers to
	// Ridu's SDK headers. Invalid HTTP token names are ignored.
	AllowedRequestHeaders []string
	// AllowedHosts restricts the HTTP Host header. Entries are exact hostnames
	// with an optional port; an entry without a port accepts any port. Empty
	// accepts every syntactically valid host for development. Production
	// deployments should set their public hostnames.
	AllowedHosts []string
	// TrustedProxyCIDRs lists proxies whose forwarded client addresses are trusted.
	TrustedProxyCIDRs []string
	// AuthRateLimit is the maximum auth attempts per identity and client window.
	AuthRateLimit int
	// AuthRateWindow is the duration over which AuthRateLimit is enforced.
	AuthRateWindow time.Duration
	// Audit receives security-relevant application events.
	Audit func(AuditEvent)
	// Observe receives timing and status metadata for completed requests.
	Observe func(RequestObservation)
	// RequestError receives trusted diagnostic detail for internal failures and
	// recovered panics. The HTTP response remains redacted.
	RequestError func(RequestErrorEvent)
	// RequestTimeout limits request execution. Zero uses the framework default;
	// a negative duration disables the handler deadline for streaming plugins.
	RequestTimeout time.Duration
	// ReadinessChecks add application/plugin dependencies to /readyz. Checks
	// must be read-only, repeatable, and honor Context cancellation.
	ReadinessChecks []ReadinessCheck
	// ReadinessTimeout bounds the complete database, storage, and custom
	// readiness probe. Zero uses five seconds; a negative duration disables it.
	ReadinessTimeout time.Duration
	// ContentSecurityPolicy overrides the framework admin policy. Empty uses a
	// conservative default compatible with live-preview frames.
	ContentSecurityPolicy string
	// DisableContentSecurityPolicy explicitly disables the admin CSP when an
	// upstream gateway owns it.
	DisableContentSecurityPolicy bool
	// StrictTransportSecurity is emitted verbatim when non-empty. Configure it
	// only when every public request is HTTPS, normally at the TLS terminator.
	StrictTransportSecurity string
	// TaskInterval controls how often the durable task queues are polled. Zero
	// uses the framework default.
	TaskInterval time.Duration
	// TaskBatch limits durable task leases claimed during one polling cycle.
	TaskBatch int
	// TaskQueues optionally restricts this process to named queues. An empty
	// list consumes every queue, including unknown task slugs so they can be
	// moved to a stable terminal failure.
	TaskQueues []string
	// TaskLeaseDuration is extended by heartbeats while a handler is running.
	TaskLeaseDuration time.Duration
	// TaskHeartbeatInterval must remain shorter than TaskLeaseDuration.
	TaskHeartbeatInterval time.Duration
	// TaskPruneBatch bounds terminal records removed after their retention.
	TaskPruneBatch int
	// AuthPruneBatch bounds expired sessions and API keys removed from each
	// durable credential family during one background maintenance cycle.
	AuthPruneBatch int
	// JobError receives failures from scheduled background work.
	JobError func(error)
}

// ReadinessCheck verifies one required production dependency.
type ReadinessCheck func(context.Context) error

// AuditEvent describes one security-relevant action handled by the API.
type AuditEvent struct {
	// Time is when the event occurred.
	Time time.Time
	// RequestID correlates the event with logs and observations.
	RequestID string
	// ClientIP is the resolved direct or trusted-forwarded client address.
	ClientIP string
	// Action names the operation, such as login, create, or delete.
	Action string
	// Collection is the affected collection slug when applicable.
	Collection string
	// DocumentID is the affected document identity when applicable.
	DocumentID string
	// ActorID is the authenticated document identity when available.
	ActorID string
	// ActorCollection disambiguates ActorID across auth collections.
	ActorCollection schema.CollectionSlug
}

// RequestObservation contains transport-level timing and response metadata.
type RequestObservation struct {
	// Time is when the observation was recorded.
	Time time.Time
	// RequestID correlates the observation with audit events and logs.
	RequestID string
	// Method is the HTTP request method.
	Method string
	// Path is the requested URL path.
	Path string
	// Status is the HTTP response status code.
	Status int
	// ErrorCode is the stable public Ridu code for a failed request.
	ErrorCode string
	// ResponseBytes is the number of response-body bytes written.
	ResponseBytes int64
	// Duration is the total handler execution time.
	Duration time.Duration
}

// RequestErrorEvent carries trusted internal diagnostics. Error may contain
// dependency detail and must not be forwarded to an untrusted client.
type RequestErrorEvent struct {
	Time      time.Time
	RequestID string
	Method    string
	Path      string
	Error     error
	Panic     bool
	Stack     string
}

// Handler returns the application HTTP API and embedded admin handler.
func (application *App) Handler(options HandlerOptions) http.Handler {
	assets := options.AdminAssets
	if assets == nil {
		assets = adminassets.FS()
	}
	trusted := make([]netip.Prefix, 0, len(options.TrustedProxyCIDRs))
	for _, encoded := range options.TrustedProxyCIDRs {
		if prefix, err := netip.ParsePrefix(encoded); err == nil {
			trusted = append(trusted, prefix)
		}
	}
	pluginEndpoints := make([]httpapi.PluginEndpoint, 0)
	for _, binding := range application.pluginEndpoints {
		current := binding.endpoint
		pluginEndpoints = append(pluginEndpoints, httpapi.PluginEndpoint{
			Method: strings.ToUpper(strings.TrimSpace(current.Method)), Path: "/api/plugins/" + binding.pluginKey + "/" + current.Path,
			MaxBodyBytes: current.MaxBodyBytes,
			Handler:      application.endpointHandler(current.Handler, endpointScopeRoot, ""),
		})
	}
	customEndpoints := make([]httpapi.CustomEndpoint, 0, len(application.customEndpoints))
	for _, binding := range application.customEndpoints {
		current := binding.definition
		scope := httpapi.CustomEndpointRoot
		mount := "/api"
		switch binding.scope {
		case endpointScopeCollection:
			scope = httpapi.CustomEndpointCollection
			mount = "/api/collections/" + string(binding.resource)
		case endpointScopeGlobal:
			scope = httpapi.CustomEndpointGlobal
			mount = "/api/globals/" + string(binding.resource)
		}
		pattern := mount + current.Path
		if current.Path == "/" {
			pattern = mount
		}
		customEndpoints = append(customEndpoints, httpapi.CustomEndpoint{
			Method: strings.ToUpper(strings.TrimSpace(current.Method)), Pattern: pattern,
			Scope: scope, Resource: binding.resource, MaxBodyBytes: current.MaxBodyBytes,
			Handler: application.endpointHandler(current.Handler, binding.scope, binding.resource),
		})
	}
	pluginTransports := make([]httpapi.PluginEndpoint, 0, len(application.pluginTransports))
	for _, binding := range application.pluginTransports {
		current := binding.transport
		pluginTransports = append(pluginTransports, httpapi.PluginEndpoint{
			Method: current.Method, Path: current.Path, MaxBodyBytes: current.MaxBodyBytes,
			Handler: application.endpointHandler(current.Handler, endpointScopeRoot, ""),
		})
	}
	var requestError func(httpapi.RequestErrorEvent)
	if options.RequestError != nil {
		requestError = func(event httpapi.RequestErrorEvent) {
			options.RequestError(RequestErrorEvent(event))
		}
	}
	return httpapi.New(httpapi.Config{
		Manifest: application.manifest, Engine: application.local.engine,
		ManifestForRequest: func(ctx context.Context, identity *httpapi.AuthIdentity) (schema.Snapshot, error) {
			return application.manifestForIdentity(ctx, httpIdentityActor(identity), httpIdentityCollection(identity))
		},
		AdminAssets:  assets,
		MaxBodyBytes: options.MaxBodyBytes, SecureCookies: options.SecureCookies,
		AllowedOrigins: append([]string(nil), options.AllowedOrigins...), AllowedRequestHeaders: append([]string(nil), options.AllowedRequestHeaders...), AllowedHosts: append([]string(nil), options.AllowedHosts...), TrustedProxies: trusted,
		AuthRateLimit: options.AuthRateLimit, AuthRateWindow: options.AuthRateWindow,
		Login: func(ctx context.Context, collection, email, password string, metadata httpapi.LoginMetadata) (httpapi.AuthSession, error) {
			session, err := application.LoginWithOptions(ctx, collection, email, password, LoginOptions{IPAddress: metadata.IPAddress, UserAgent: metadata.UserAgent})
			return httpapi.AuthSession{ID: session.ID, Token: session.Token, Collection: session.Collection, User: session.User, ExpiresAt: session.ExpiresAt}, err
		},
		Session: func(ctx context.Context, token string) (httpapi.AuthSession, error) {
			session, err := application.Session(ctx, token)
			return httpapi.AuthSession{ID: session.ID, Token: session.Token, Collection: session.Collection, User: session.User, ExpiresAt: session.ExpiresAt}, err
		},
		RotateSession: func(ctx context.Context, token string) (httpapi.AuthSession, error) {
			session, err := application.RotateSession(ctx, token)
			return httpapi.AuthSession{ID: session.ID, Token: session.Token, Collection: session.Collection, User: session.User, ExpiresAt: session.ExpiresAt}, err
		},
		Logout: application.Logout, LogoutAll: application.LogoutAll,
		Sessions: func(ctx context.Context, token string) ([]httpapi.AuthSessionInfo, error) {
			sessions, err := application.Sessions(ctx, token)
			result := make([]httpapi.AuthSessionInfo, len(sessions))
			for index, session := range sessions {
				result[index] = httpapi.AuthSessionInfo(session)
			}
			return result, err
		},
		RevokeSession:          application.RevokeSession,
		RequestPasswordReset:   application.RequestPasswordReset,
		ResetPassword:          application.ResetPassword,
		RequestVerification:    application.RequestVerification,
		VerifyEmail:            application.VerifyEmail,
		ChangePassword:         application.ChangePassword,
		AuthBootstrapAvailable: application.AuthBootstrapAvailable,
		CreateAuthUser: func(ctx context.Context, collection string, values store.Values, password string, identity *httpapi.AuthIdentity) (store.Document, error) {
			return application.CreateAuthUserForTransport(ctx, collection, values, password, MutationOptions{Actor: httpIdentityActor(identity), ActorCollection: httpIdentityCollection(identity)})
		},
		CreateAuthUserLocalized: func(ctx context.Context, collection string, values store.Values, password string, identity *httpapi.AuthIdentity, options httpapi.LocaleOptions) (store.Document, error) {
			return application.CreateAuthUserForTransport(ctx, collection, values, password, MutationOptions{
				Actor: httpIdentityActor(identity), ActorCollection: httpIdentityCollection(identity),
				Locale: schema.LocaleCode(options.Locale), FallbackLocales: options.FallbackLocales,
				DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
			})
		},
		CreateAPIKey: func(ctx context.Context, token, name string, expiresAt time.Time) (httpapi.APIKey, error) {
			key, err := application.CreateAPIKey(ctx, token, name, expiresAt)
			return httpapi.APIKey(key), err
		},
		APIKeys: func(ctx context.Context, token string) ([]httpapi.APIKeyInfo, error) {
			keys, err := application.APIKeys(ctx, token)
			result := make([]httpapi.APIKeyInfo, len(keys))
			for index, key := range keys {
				result[index] = httpapi.APIKeyInfo(key)
			}
			return result, err
		},
		RevokeAPIKey: application.RevokeAPIKey,
		AuthenticateAPIKey: func(ctx context.Context, raw string) (httpapi.AuthIdentity, error) {
			identity, err := application.AuthenticateAPIKeyIdentity(ctx, raw)
			return httpapi.AuthIdentity{Collection: identity.Collection, Actor: identity.Actor}, err
		},
		AuthenticateExternal: func(ctx context.Context, headers map[string][]string) (httpapi.AuthIdentity, error) {
			identity, err := application.AuthenticateExternalIdentity(ctx, headers)
			return httpapi.AuthIdentity{Collection: identity.Collection, Actor: identity.Actor}, err
		},
		PreviewLifecycleEpoch: application.previewIdentityEpoch,
		CreateCollectionPreviewToken: func(ctx context.Context, collection, documentID string, identity *httpapi.AuthIdentity) (httpapi.PreviewToken, error) {
			token, err := application.CreateCollectionPreviewToken(ctx, collection, documentID, authIdentity(identity))
			return httpapi.PreviewToken{Token: token.Token, Resource: token.Resource, Slug: token.Slug, DocumentID: token.DocumentID, ExpiresAt: token.ExpiresAt}, err
		},
		CreateGlobalPreviewToken: func(ctx context.Context, slug string, identity *httpapi.AuthIdentity) (httpapi.PreviewToken, error) {
			token, err := application.CreateGlobalPreviewToken(ctx, slug, authIdentity(identity))
			return httpapi.PreviewToken{Token: token.Token, Resource: token.Resource, Slug: token.Slug, DocumentID: token.DocumentID, ExpiresAt: token.ExpiresAt}, err
		},
		RevokePreviewToken: func(ctx context.Context, raw string, identity *httpapi.AuthIdentity) error {
			return application.RevokePreviewToken(ctx, raw, authIdentity(identity))
		},
		FindCollectionPreview: application.FindCollectionPreview,
		FindGlobalPreview:     application.FindGlobalPreview,
		GetPreference: func(ctx context.Context, identity *httpapi.AuthIdentity, key string) (json.RawMessage, error) {
			return application.Preference(ctx, authIdentity(identity), key)
		},
		SetPreference: func(ctx context.Context, identity *httpapi.AuthIdentity, key string, value json.RawMessage) (json.RawMessage, error) {
			return application.SetPreference(ctx, authIdentity(identity), key, value)
		},
		DeletePreference: func(ctx context.Context, identity *httpapi.AuthIdentity, key string) error {
			return application.DeletePreference(ctx, authIdentity(identity), key)
		},
		ResetPreferences: func(ctx context.Context, identity *httpapi.AuthIdentity) error {
			return application.ResetPreferences(ctx, authIdentity(identity))
		},
		ForceUnlock: func(ctx context.Context, collection, id string, identity *httpapi.AuthIdentity) error {
			return application.ForceUnlock(ctx, collection, id, authIdentity(identity))
		},
		DocumentLock: func(ctx context.Context, collection, id string, identity *httpapi.AuthIdentity) (protocol.DocumentLockEnvelope, error) {
			state, err := application.DocumentLock(ctx, collection, id, authIdentity(identity))
			return documentLockEnvelope(state), err
		},
		AcquireDocumentLock: func(ctx context.Context, collection, id string, takeover bool, identity *httpapi.AuthIdentity) (protocol.DocumentLockEnvelope, error) {
			state, err := application.AcquireDocumentLock(ctx, collection, id, takeover, authIdentity(identity))
			return documentLockEnvelope(state), err
		},
		ReleaseDocumentLock: func(ctx context.Context, collection, id string, identity *httpapi.AuthIdentity) error {
			return application.ReleaseDocumentLock(ctx, collection, id, authIdentity(identity))
		},
		SchedulePublish: func(ctx context.Context, collection, documentID string, runAt time.Time, expectedRevision int, identity *httpapi.AuthIdentity) (store.ScheduledPublish, error) {
			return application.SchedulePublish(ctx, collection, documentID, runAt, expectedRevision, authIdentity(identity))
		},
		ScheduledPublishes: func(ctx context.Context, collection, documentID string, identity *httpapi.AuthIdentity) ([]store.ScheduledPublish, error) {
			return application.ScheduledPublishes(ctx, collection, documentID, authIdentity(identity))
		},
		CancelScheduledPublish: func(ctx context.Context, collection, documentID, jobID string, identity *httpapi.AuthIdentity) error {
			return application.CancelScheduledPublish(ctx, collection, documentID, jobID, authIdentity(identity))
		},
		AllowAuthIPAttempt: func(ctx context.Context, clientIP, collection string, maximum int, window time.Duration) (bool, error) {
			key := tokenDigest("ip\x00" + clientIP + "\x00" + collection)
			return application.auth.AllowAuthAttempt(ctx, key, time.Now().UTC(), window, maximum)
		},
		AllowAuthAttempt: func(ctx context.Context, clientIP, collection, identity string, maximum int, window time.Duration) (bool, error) {
			key := tokenDigest("identity\x00" + clientIP + "\x00" + collection + "\x00" + store.CanonicalAuthIdentity(identity))
			return application.auth.AllowAuthAttempt(ctx, key, time.Now().UTC(), window, maximum)
		},
		AcquireUpload: func(ctx context.Context, collection string) (func(), error) {
			resolved, exists := application.bySlug[collection]
			if !exists || resolved.Upload == nil {
				return nil, &operationengine.Error{Code: "unknown_upload_collection", Status: 404, Message: "upload collection was not found"}
			}
			release, err := application.uploads.AcquireFile(ctx, resolved.Upload.MaxFileSize)
			if err != nil {
				return nil, uploadPreparationError(err, "upload could not be admitted")
			}
			return release, nil
		},
		Upload: func(ctx context.Context, collection, filename string, reader io.Reader, values store.Values, identity *httpapi.AuthIdentity, admissionHeld bool) (store.Document, error) {
			return application.uploadForIdentity(ctx, collection, UploadInput{Filename: filename, Reader: reader, Data: values}, authIdentity(identity), admissionHeld)
		},
		UploadLocalized: func(ctx context.Context, collection, filename string, reader io.Reader, values store.Values, identity *httpapi.AuthIdentity, options httpapi.LocaleOptions, admissionHeld bool) (store.Document, error) {
			return application.uploadForIdentity(ctx, collection, UploadInput{Filename: filename, Reader: reader, Data: values, Locale: LocaleOptions{Locale: schema.LocaleCode(options.Locale), FallbackLocales: options.FallbackLocales, DisableFallback: options.DisableFallback, AllLocales: options.AllLocales}}, authIdentity(identity), admissionHeld)
		},
		RemoteUpload: func(ctx context.Context, collection, remoteURL string, values store.Values, identity *httpapi.AuthIdentity) (store.Document, error) {
			return application.UploadFromURLForIdentity(ctx, collection, RemoteUploadInput{URL: remoteURL, Data: values}, authIdentity(identity))
		},
		RemoteUploadLocalized: func(ctx context.Context, collection, remoteURL string, values store.Values, identity *httpapi.AuthIdentity, options httpapi.LocaleOptions) (store.Document, error) {
			return application.UploadFromURLForIdentity(ctx, collection, RemoteUploadInput{URL: remoteURL, Data: values, Locale: LocaleOptions{Locale: schema.LocaleCode(options.Locale), FallbackLocales: options.FallbackLocales, DisableFallback: options.DisableFallback, AllLocales: options.AllLocales}}, authIdentity(identity))
		},
		UpdateUploadImage: func(ctx context.Context, collection, id string, focalX, focalY, cropX, cropY, cropWidth, cropHeight float64, expectedRevision int, identity *httpapi.AuthIdentity) (store.Document, error) {
			return application.UpdateUploadImageForIdentity(ctx, collection, id, UpdateUploadImageInput{FocalX: focalX, FocalY: focalY, CropX: cropX, CropY: cropY, CropWidth: cropWidth, CropHeight: cropHeight, ExpectedRevision: expectedRevision}, authIdentity(identity))
		},
		Duplicate: func(ctx context.Context, collection, id string, values store.Values, identity *httpapi.AuthIdentity, options httpapi.LocaleOptions) (store.Document, error) {
			return application.DuplicateForIdentity(ctx, collection, id, values, authIdentity(identity), LocaleOptions{
				Locale: schema.LocaleCode(options.Locale), FallbackLocales: options.FallbackLocales,
				DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
			})
		},
		OpenUpload: func(ctx context.Context, collection, key string, identity *httpapi.AuthIdentity) (io.ReadCloser, storage.Object, error) {
			return application.OpenUploadForIdentity(ctx, collection, key, authIdentity(identity))
		},
		Audit: func(event httpapi.AuditEvent) {
			if options.Audit != nil {
				options.Audit(AuditEvent(event))
			}
		},
		Ready: application.checkReadiness(options),
		Observe: func(observation httpapi.RequestObservation) {
			if options.Observe != nil {
				options.Observe(RequestObservation(observation))
			}
		},
		RequestError:          requestError,
		RequestTimeout:        options.RequestTimeout,
		ContentSecurityPolicy: options.ContentSecurityPolicy, DisableContentSecurityPolicy: options.DisableContentSecurityPolicy,
		StrictTransportSecurity: options.StrictTransportSecurity,
		CustomEndpoints:         customEndpoints,
		PluginEndpoints:         pluginEndpoints, PluginTransports: pluginTransports,
	})
}

func (application *App) checkReadiness(options HandlerOptions) ReadinessCheck {
	checks := append([]ReadinessCheck(nil), options.ReadinessChecks...)
	timeout := options.ReadinessTimeout
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("readiness check: %w", err)
		}
		if application.draining.Load() {
			return errors.New("application is draining")
		}
		readinessContext := ctx
		cancel := func() {}
		readinessTimeout := timeout
		if readinessTimeout == 0 {
			readinessTimeout = 5 * time.Second
		}
		if readinessTimeout > 0 {
			readinessContext, cancel = context.WithTimeout(ctx, readinessTimeout)
		}
		defer cancel()
		select {
		case application.readinessAdmission <- struct{}{}:
		case <-readinessContext.Done():
			return fmt.Errorf("readiness check: %w", readinessContext.Err())
		default:
			return errors.New("readiness check is already in progress")
		}

		completed := make(chan error, 1)
		go func() {
			var result error
			defer func() {
				if recover() != nil {
					result = errors.New("readiness probe panicked")
				}
				<-application.readinessAdmission
				completed <- result
			}()
			if application.draining.Load() {
				result = errors.New("application is draining")
				return
			}
			if options.developmentReadiness {
				switch {
				case application.health != nil:
					result = runReadinessCheck(readinessContext, "database", application.health.Ping)
				case application.readiness != nil:
					result = runReadinessCheck(readinessContext, "database", func(ctx context.Context) error {
						return application.readiness.Ready(ctx, application.manifest)
					})
				default:
					result = errors.New("database development readiness is unavailable")
				}
				if result != nil {
					return
				}
			} else if application.migrationReadiness != nil && application.migrationHistoryDigest != "" {
				if result = runReadinessCheck(readinessContext, "database", func(ctx context.Context) error {
					return application.migrationReadiness.ReadyWithMigrationHistory(ctx, application.manifest, application.migrationHistoryDigest)
				}); result != nil {
					return
				}
			} else if application.migrationReadiness != nil && !options.allowUnverifiableReadiness {
				result = errors.New("database executable migration history is unavailable; build with ridu build")
				return
			} else if application.readiness != nil {
				if result = runReadinessCheck(readinessContext, "database", func(ctx context.Context) error {
					return application.readiness.Ready(ctx, application.manifest)
				}); result != nil {
					return
				}
			} else if application.health != nil {
				if result = runReadinessCheck(readinessContext, "database", application.health.Ping); result != nil {
					return
				}
			}
			if application.storageHealth != nil {
				if result = runReadinessCheck(readinessContext, "upload storage", application.storageHealth.Ping); result != nil {
					return
				}
			}
			for index, check := range checks {
				if check != nil {
					if result = runReadinessCheck(readinessContext, fmt.Sprintf("custom %d", index+1), check); result != nil {
						return
					}
				}
			}
		}()
		select {
		case result := <-completed:
			return result
		case <-readinessContext.Done():
			return fmt.Errorf("readiness check: %w", readinessContext.Err())
		}
	}
}

func runReadinessCheck(ctx context.Context, name string, check ReadinessCheck) (result error) {
	defer func() {
		if recover() != nil {
			result = fmt.Errorf("%s readiness check panicked", name)
		}
	}()
	if err := check(ctx); err != nil {
		return fmt.Errorf("%s readiness check: %w", name, err)
	}
	return nil
}

func authIdentity(identity *httpapi.AuthIdentity) *AuthIdentity {
	if identity == nil {
		return nil
	}
	return &AuthIdentity{Collection: identity.Collection, Actor: identity.Actor, PreviewEpoch: identity.PreviewEpoch}
}

func httpIdentityActor(identity *httpapi.AuthIdentity) *store.Document {
	if identity == nil {
		return nil
	}
	actor := store.CloneDocument(identity.Actor)
	return &actor
}

func httpIdentityCollection(identity *httpapi.AuthIdentity) schema.CollectionSlug {
	if identity == nil {
		return ""
	}
	return identity.Collection
}

func documentLockEnvelope(state DocumentLockState) protocol.DocumentLockEnvelope {
	result := protocol.DocumentLockEnvelope{Owned: state.Owned, Acquired: state.Acquired, CanTakeOver: state.CanTakeOver}
	if state.Lock != nil {
		result.Lock = &protocol.DocumentLock{
			DocumentID: state.Lock.DocumentID, OwnerID: state.Lock.OwnerID, OwnerLabel: state.Lock.OwnerLabel,
			CreatedAt: state.Lock.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: state.Lock.UpdatedAt.Format(time.RFC3339Nano), ExpiresAt: state.Lock.ExpiresAt.Format(time.RFC3339Nano),
		}
	}
	return result
}

func (application *App) endpointHandler(handler EndpointHandler, scope endpointScope, resource schema.CollectionSlug) httpapi.EndpointHandler {
	return func(ctx httpapi.EndpointContext) {
		endpoint := EndpointContext{Writer: ctx.Writer, Request: ctx.Request, RequestID: ctx.RequestID,
			ClientIP: ctx.ClientIP, RouteParams: ctx.RouteParams, Actor: cloneDocument(ctx.Actor),
			ActorCollection: ctx.ActorCollection, Local: application.local,
			AdmitAuthAttempt: ctx.AdmitAuthAttempt, ReportError: ctx.ReportError}
		switch scope {
		case endpointScopeCollection:
			endpoint.Collection = resource
		case endpointScopeGlobal:
			endpoint.Global = resource
		}
		handler(endpoint)
	}
}
