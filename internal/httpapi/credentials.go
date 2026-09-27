package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	operationengine "github.com/riducms/ridu/internal/operation"
)

// credentialScheme is the kind of explicit Ridu credential carried by the
// Authorization header. Other Authorization values belong to applications.
type credentialScheme uint8

const (
	credentialNone credentialScheme = iota
	credentialSession
	credentialAPIKey
)

// sessionTransport records how the current session token reached Ridu, so a
// response only changes the cookie when the cookie was the credential.
type sessionTransport uint8

const (
	transportCookie sessionTransport = iota + 1
	transportHeader
)

type credentialStateKey struct{}

// credentialState memoizes one request's principal. Handlers ask for the
// identity repeatedly; the session store is consulted at most once.
type credentialState struct {
	// mu serializes lazy resolution; application endpoints may read the
	// identity from several goroutines.
	mu       sync.Mutex
	scheme   credentialScheme
	value    string
	resolved bool
	identity *AuthIdentity
	// session is the authenticated session when a session token, from either
	// transport, established the identity.
	session   *AuthSession
	transport sessionTransport
	// err is the terminal failure of an explicit credential.
	err error
	// cookieErr is a non-authentication failure while resolving the cookie.
	cookieErr error
}

// explicitCredential recognizes only Ridu-owned Authorization forms: the
// Session scheme and API keys in their ridu_<id>_<secret> form. A non-Ridu
// Bearer value, such as an application webhook secret or a preview token, is
// not a Ridu session credential.
func explicitCredential(request *http.Request) (credentialScheme, string) {
	scheme, value, found := strings.Cut(request.Header.Get("Authorization"), " ")
	if !found {
		if strings.EqualFold(strings.TrimSpace(scheme), "Session") {
			return credentialSession, ""
		}
		return credentialNone, ""
	}
	value = strings.TrimSpace(value)
	switch {
	case strings.EqualFold(scheme, "Session"):
		return credentialSession, value
	case strings.EqualFold(scheme, "Bearer") && strings.HasPrefix(value, "ridu_"):
		return credentialAPIKey, value
	default:
		return credentialNone, ""
	}
}

// credentialIndependent reports routes that never consume the request
// identity. A stale explicit credential cannot block signing in again,
// recovery, or an idempotent logout.
func (api *API) credentialIndependent(request *http.Request) bool {
	path := request.URL.Path
	if path == "/healthz" || path == "/readyz" || path == "/api/auth/logout" {
		return true
	}
	remainder, auth := strings.CutPrefix(path, "/api/auth/")
	if !auth {
		return false
	}
	segments := strings.Split(remainder, "/")
	if len(segments) != 2 || segments[0] == "" {
		return false
	}
	switch segments[1] {
	case "login", "bootstrap", "forgot-password", "reset-password", "request-verification", "verify":
		return true
	default:
		return false
	}
}

// attachCredentialState parses the request's explicit credential and, unless
// the route is credential-independent, authenticates it before routing. An
// invalid explicit credential ends the request; it never falls back to the
// cookie or anonymous access.
func (api *API) attachCredentialState(request *http.Request) (*http.Request, error) {
	state := &credentialState{}
	state.scheme, state.value = explicitCredential(request)
	request = request.WithContext(context.WithValue(request.Context(), credentialStateKey{}, state))
	if state.scheme == credentialNone || api.credentialIndependent(request) {
		return request, nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	api.resolveCredential(request.Context(), request, state)
	return request, state.err
}

func requestCredentialState(request *http.Request) *credentialState {
	state, _ := request.Context().Value(credentialStateKey{}).(*credentialState)
	return state
}

func (api *API) credentialState(request *http.Request) *credentialState {
	state := requestCredentialState(request)
	if state == nil {
		state = &credentialState{}
		state.scheme, state.value = explicitCredential(request)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.resolved {
		api.resolveCredential(request.Context(), request, state)
	}
	return state
}

func (api *API) resolveCredential(ctx context.Context, request *http.Request, state *credentialState) {
	state.resolved = true
	switch state.scheme {
	case credentialSession:
		if state.value == "" || api.config.Session == nil {
			state.err = invalidCredential(nil)
			return
		}
		session, err := api.config.Session(ctx, state.value)
		if err != nil {
			state.err = credentialFailure(err)
			return
		}
		state.session, state.transport = &session, transportHeader
		state.identity = &AuthIdentity{Collection: session.Collection, Actor: session.User}
		return
	case credentialAPIKey:
		if api.config.AuthenticateAPIKey == nil {
			state.err = invalidCredential(nil)
			return
		}
		identity, err := api.config.AuthenticateAPIKey(ctx, state.value)
		if err != nil {
			state.err = credentialFailure(err)
			return
		}
		state.identity = &identity
		return
	}
	if cookie, err := request.Cookie(sessionCookie); err == nil && api.config.Session != nil {
		session, sessionError := api.config.Session(ctx, cookie.Value)
		if sessionError == nil {
			state.session, state.transport = &session, transportCookie
			state.identity = &AuthIdentity{Collection: session.Collection, Actor: session.User}
			return
		}
		// An invalid cookie is ambient state and resolves to no session. A store
		// or cancellation failure is retained for session reads, which must not
		// report an outage as a logout.
		if credentialFailure(sessionError) == sessionError {
			state.cookieErr = sessionError
		}
	}
	if api.config.AuthenticateExternal != nil {
		identity, err := api.config.AuthenticateExternal(ctx, map[string][]string(request.Header.Clone()))
		if err == nil {
			state.identity = &identity
		}
	}
}

// sessionCredential returns the session token presented by the request. An
// explicit Session header takes precedence over the cookie; an API key is not
// a session and cannot manage one.
func sessionCredential(request *http.Request) (string, sessionTransport, bool) {
	switch scheme, value := explicitCredential(request); scheme {
	case credentialSession:
		return value, transportHeader, value != ""
	case credentialAPIKey:
		return "", 0, false
	}
	cookie, err := request.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return "", 0, false
	}
	return cookie.Value, transportCookie, true
}

// credentialFailure converts an authentication failure into the explicit
// credential contract while preserving store, cancellation, and hook errors.
func credentialFailure(err error) error {
	var operationError *operationengine.Error
	if errors.As(err, &operationError) && operationError.Status == http.StatusUnauthorized {
		return invalidCredential(err)
	}
	return err
}

func invalidCredential(cause error) error {
	return &operationengine.Error{Code: "invalid_credential", Status: http.StatusUnauthorized, Message: "the Authorization credential is invalid or expired", Cause: cause}
}
