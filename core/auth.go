package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

// AuthSession is one authenticated identity and its session metadata.
type AuthSession struct {
	// ID is the non-secret identifier used to manage this session.
	ID string
	// Token is the opaque credential accepted by session-aware transports.
	Token string
	// Collection is the auth-enabled collection that owns the identity.
	Collection schema.CollectionSlug
	// User is the current document from Collection.
	User store.Document
	// ExpiresAt is the absolute UTC expiry time.
	ExpiresAt time.Time
}

// AuthIdentity identifies one authenticated document without relying on a
// document ID being globally unique across auth-enabled collections.
type AuthIdentity struct {
	Collection schema.CollectionSlug
	Actor      store.Document
	// PreviewEpoch is an opaque lifecycle snapshot captured before transport
	// authentication. Zero lets direct Go callers snapshot at mint entry.
	PreviewEpoch uint64
}

// AuthSessionInfo is safe session metadata shown to an authenticated user.
// It never contains the bearer token or its digest.
type AuthSessionInfo struct {
	ID         string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	IPAddress  string
	UserAgent  string
	Current    bool
}

// APIKey is returned only when a new key is created. Key is the bearer secret
// and cannot be recovered later.
type APIKey struct {
	ID        string
	Name      string
	Key       string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// APIKeyInfo is safe API-key metadata suitable for account settings UIs.
type APIKeyInfo struct {
	ID         string
	Name       string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
}

// LoginOptions carries transport metadata recorded with a new session.
type LoginOptions struct {
	// IPAddress is the direct or trusted-forwarded client address.
	IPAddress string
	// UserAgent is the untrusted client user-agent string.
	UserAgent string
}

// AuthInitialized reports whether an active document exists in an auth
// collection. It intentionally bypasses collection read access so first-run
// setup can decide whether bootstrap UI is still appropriate without exposing
// any document data.
func (application *App) AuthInitialized(ctx context.Context, collection string) (initialized bool, err error) {
	resolved, exists := application.authBySlug[collection]
	if !exists {
		return false, unknownAuthCollection(collection)
	}
	transaction, err := application.beginAuthReadSnapshot(ctx)
	if err != nil {
		return false, fmt.Errorf("begin auth initialization read: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			err = errors.Join(err, rollbackTransaction(ctx, transaction))
		}
	}()
	page, err := transaction.List(ctx, store.Request{Collection: resolved, Collections: map[schema.StableID]schema.Collection{resolved.ID: resolved}, Page: 1, Limit: 1})
	if err != nil {
		return false, fmt.Errorf("read auth initialization state: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit auth initialization read: %w", err)
	}
	committed = true
	return page.Total > 0, nil
}

// AuthBootstrapAvailable reports whether the configured admin-user collection
// still permits Ridu's omitted-policy, one-time anonymous bootstrap. Explicit
// Create policies and secondary auth collections never use this setup path.
func (application *App) AuthBootstrapAvailable(ctx context.Context, collection string) (bool, error) {
	resolved, exists := application.authBySlug[collection]
	if !exists {
		return false, unknownAuthCollection(collection)
	}
	if resolved.ID != application.adminUserCollection || application.authCreatePolicySet[collection] {
		return false, nil
	}
	initialized, err := application.AuthInitialized(ctx, collection)
	if err != nil {
		return false, err
	}
	return !initialized, nil
}

// CreateAuthUser creates an auth document and its private password credential
// atomically through the ordinary create operation pipeline.
func (application *App) CreateAuthUser(ctx context.Context, collection string, values store.Values, password string, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return application.CreateAuthUserWithOptions(ctx, collection, values, password, mutationOptions(actor, 0, localeOptions))
}

// CreateAuthUserWithOptions creates an auth document and credential atomically
// while allowing a transport to populate the returned user safely.
func (application *App) CreateAuthUserWithOptions(ctx context.Context, collection string, values store.Values, password string, options MutationOptions) (store.Document, error) {
	return application.createAuthUserWithOptions(ctx, collection, values, password, options, false)
}

// CreateAuthUserForTransport applies the safe anonymous first-user default
// used by framework transports. When Create access is omitted, exactly one
// anonymous caller may initialize the configured admin-user collection; other
// auth collections remain closed. Authenticated callers and collections with
// an explicit Create rule retain ordinary authored access behavior, including
// intentional public registration.
func (application *App) CreateAuthUserForTransport(ctx context.Context, collection string, values store.Values, password string, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return application.CreateAuthUserForTransportWithOptions(ctx, collection, values, password, mutationOptions(actor, 0, localeOptions))
}

// CreateAuthUserForTransportWithOptions is the selection-aware transport form
// of CreateAuthUserForTransport.
func (application *App) CreateAuthUserForTransportWithOptions(ctx context.Context, collection string, values store.Values, password string, options MutationOptions) (store.Document, error) {
	authCollection, exists := application.authBySlug[collection]
	if !exists {
		return store.Document{}, unknownAuthCollection(collection)
	}
	bootstrap := options.Actor == nil && !application.authCreatePolicySet[collection]
	if bootstrap && authCollection.ID != application.adminUserCollection {
		return store.Document{}, &operationengine.Error{Code: "access_denied", Status: 403, Message: "anonymous auth user creation is not permitted"}
	}
	return application.createAuthUserWithOptions(ctx, collection, values, password, options, bootstrap)
}

func (application *App) createAuthUserWithOptions(ctx context.Context, collection string, values store.Values, password string, options MutationOptions, bootstrap bool) (store.Document, error) {
	authCollection, exists := application.authBySlug[collection]
	if !exists {
		return store.Document{}, unknownAuthCollection(collection)
	}
	if bootstrap {
		initialized, err := application.AuthInitialized(ctx, collection)
		if err != nil {
			return store.Document{}, err
		}
		if initialized {
			return store.Document{}, &operationengine.Error{Code: "access_denied", Status: 403, Message: "auth collection is already initialized", Cause: store.ErrAuthInitialized}
		}
	}
	if err := application.validatePassword(collection, password); err != nil {
		return store.Document{}, err
	}
	hash, err := application.generatePasswordHash(ctx, password, authCollection.Auth.PasswordBcryptCost)
	if err != nil {
		return store.Document{}, fmt.Errorf("hash password: %w", err)
	}
	user, err := application.local.createWithTransactionMutationAndFieldAccess(ctx, collection, values, options, func(ctx context.Context, transaction store.Transaction, collection schema.Collection, document store.Document) error {
		if bootstrap {
			authBootstrap, supported := transaction.(store.AuthBootstrapTransaction)
			if !supported {
				return fmt.Errorf("anonymous auth initialization requires store.AuthBootstrapTransaction")
			}
			// The first operator must not be locked out behind an email delivery
			// system that cannot be configured until after admin access exists.
			return authBootstrap.CreateFirstAuthCredential(ctx, collection, document.ID, hash, true)
		}
		authTransaction, supported := transaction.(store.AuthTransaction)
		if !supported {
			return fmt.Errorf("auth user creation requires store.AuthTransaction")
		}
		return authTransaction.CreateAuthCredential(ctx, collection, document.ID, hash, !collection.Auth.VerifyEmail)
	}, bootstrap)
	if err != nil {
		return store.Document{}, err
	}
	if authCollection.Auth.VerifyEmail && !bootstrap {
		self := &store.Document{ID: user.ID, Values: store.Values{}}
		unredacted, findError := application.local.Find(ctx, collection, user.ID, self, LocaleOptions{
			Locale: options.Locale, FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
			DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
		})
		if findError != nil {
			return store.Document{}, findError
		}
		if err := application.issueVerification(ctx, authCollection, unredacted); err != nil {
			return store.Document{}, err
		}
	}
	return user, nil
}

// SetPassword validates and replaces a local password. Every existing session
// is revoked after the new hash is durably stored.
func (application *App) SetPassword(ctx context.Context, collection, userID, password string) error {
	authCollection, exists := application.authBySlug[collection]
	if !exists {
		return unknownAuthCollection(collection)
	}
	if err := application.validatePassword(collection, password); err != nil {
		return err
	}
	actor := &store.Document{ID: userID, Values: store.Values{}}
	user, err := application.local.Find(ctx, string(authCollection.Slug), userID, actor)
	if err != nil {
		return err
	}
	hash, err := application.generatePasswordHash(ctx, password, authCollection.Auth.PasswordBcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := application.auth.SetPasswordHash(ctx, authCollection, userID, hash, !authCollection.Auth.VerifyEmail); err != nil {
		return err
	}
	if authCollection.Auth.VerifyEmail {
		identity, _ := user.Values[authCollection.Auth.IdentityField].StringValue()
		credential, credentialError := application.auth.FindAuthCredential(ctx, authCollection, identity)
		if credentialError != nil {
			return credentialError
		}
		if !credential.Verified {
			return application.issueVerification(ctx, authCollection, user)
		}
	}
	return nil
}

// Login authenticates with the built-in local strategy.
func (application *App) Login(ctx context.Context, collection, email, password string) (AuthSession, error) {
	return application.LoginWithOptions(ctx, collection, email, password, LoginOptions{})
}

// LoginWithOptions authenticates and records safe client metadata with the
// resulting session. Credential failures intentionally share one response.
func (application *App) LoginWithOptions(ctx context.Context, collection, email, password string, options LoginOptions) (AuthSession, error) {
	authCollection, exists := application.authBySlug[collection]
	if !exists {
		return AuthSession{}, unknownAuthCollection(collection)
	}
	options = normalizeLoginOptions(options)
	now := time.Now().UTC()
	credential, credentialError := application.auth.FindAuthCredential(ctx, authCollection, store.CanonicalAuthIdentity(email))
	hash := credential.PasswordHash
	if errors.Is(credentialError, store.ErrNotFound) {
		hash = application.dummyPasswordHashes[collection]
	} else if credentialError != nil {
		return AuthSession{}, credentialError
	}
	passwordMatches, passwordError := application.passwordMatches(ctx, hash, password)
	if passwordError != nil {
		return AuthSession{}, passwordError
	}
	if credentialError != nil || !passwordMatches {
		if credentialError == nil && authCollection.Auth.MaxLoginAttempts > 0 {
			_, err := application.auth.RecordFailedLogin(
				ctx, authCollection.ID, credential.User.ID, now,
				authCollection.Auth.MaxLoginAttempts,
				time.Duration(authCollection.Auth.LockDurationSeconds)*time.Second,
			)
			if err != nil {
				return AuthSession{}, err
			}
		}
		return AuthSession{}, invalidCredentials()
	}
	if credential.LockedUntil.After(now) {
		return AuthSession{}, invalidCredentials()
	}
	if authCollection.Auth.VerifyEmail && !credential.Verified {
		return AuthSession{}, &operationengine.Error{Code: "email_not_verified", Status: 403, Message: "verify your email before logging in"}
	}
	user, err := application.authUser(ctx, authCollection, credential.User.ID)
	if err != nil {
		return AuthSession{}, invalidCredentials()
	}
	authContext := application.newAuthContext(ctx, AuthOperationLogin, authCollection, &user, email, options)
	config := application.authConfigBySlug[collection]
	if err := application.authorizeAuth(config.Access.Login, authContext); err != nil {
		return AuthSession{}, err
	}
	if err := application.runAuthHooks(config.Hooks.BeforeLogin, authContext); err != nil {
		return AuthSession{}, err
	}
	if authCollection.Auth.MaxLoginAttempts > 0 {
		accepted, err := application.auth.ResetLoginAttempts(ctx, authCollection.ID, credential.User.ID, now)
		if err != nil {
			return AuthSession{}, err
		}
		if !accepted {
			return AuthSession{}, invalidCredentials()
		}
	}
	expectedPasswordHash := credential.PasswordHash
	if cost, err := bcrypt.Cost(credential.PasswordHash); err == nil && cost < authCollection.Auth.PasswordBcryptCost {
		upgraded, hashError := application.generatePasswordHash(ctx, password, authCollection.Auth.PasswordBcryptCost)
		if hashError != nil {
			return AuthSession{}, fmt.Errorf("upgrade password hash: %w", hashError)
		}
		if err := application.auth.UpgradePasswordHash(ctx, authCollection, credential.User.ID, expectedPasswordHash, upgraded); err != nil {
			if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
				return AuthSession{}, invalidCredentials()
			}
			return AuthSession{}, err
		}
		expectedPasswordHash = upgraded
	}
	session, err := application.createSession(ctx, authCollection, user, expectedPasswordHash, now, options)
	if err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			return AuthSession{}, invalidCredentials()
		}
		return AuthSession{}, err
	}
	if err := application.runAuthHooks(config.Hooks.AfterLogin, authContext); err != nil {
		_ = application.auth.DeleteSession(ctx, tokenDigest(session.Token))
		return AuthSession{}, err
	}
	return session, nil
}

// Session resolves an opaque token to its current identity.
func (application *App) Session(ctx context.Context, token string) (AuthSession, error) {
	session, err := application.resolveSession(ctx, token, time.Now().UTC())
	if err != nil {
		return AuthSession{}, err
	}
	collection := application.authBySlug[string(session.Collection)]
	authContext := application.newAuthContext(ctx, AuthOperationMe, collection, &session.User, "", LoginOptions{})
	if err := application.runAuthHooks(application.authConfigBySlug[string(session.Collection)].Hooks.AfterMe, authContext); err != nil {
		return AuthSession{}, err
	}
	return session, nil
}

// RotateSession atomically replaces a session bearer token without extending
// its absolute lifetime. The old token is invalid as soon as this returns.
func (application *App) RotateSession(ctx context.Context, token string) (AuthSession, error) {
	now := time.Now().UTC()
	current, err := application.auth.FindSession(ctx, tokenDigest(token), now)
	if err != nil {
		return AuthSession{}, authenticationRequired()
	}
	collection, exists := application.authByID[current.CollectionID]
	if !exists {
		return AuthSession{}, authenticationRequired()
	}
	user, err := application.authUser(ctx, collection, current.UserID)
	if err != nil {
		return AuthSession{}, authenticationRequired()
	}
	config := application.authConfigBySlug[string(collection.Slug)]
	authContext := application.newAuthContext(ctx, AuthOperationRefresh, collection, &user, "", LoginOptions{IPAddress: current.IPAddress, UserAgent: current.UserAgent})
	if err := application.runAuthHooks(config.Hooks.BeforeRefresh, authContext); err != nil {
		return AuthSession{}, err
	}
	rotated, err := newOpaqueToken(32)
	if err != nil {
		return AuthSession{}, err
	}
	replacement := current
	replacement.TokenHash = tokenDigest(rotated)
	replacement.LastSeenAt = now
	if err := application.auth.RotateSession(ctx, tokenDigest(token), replacement, now); err != nil {
		return AuthSession{}, authenticationRequired()
	}
	if err := application.runAuthHooks(config.Hooks.AfterRefresh, authContext); err != nil {
		_ = application.auth.DeleteSession(ctx, replacement.TokenHash)
		return AuthSession{}, err
	}
	return AuthSession{ID: current.ID, Token: rotated, Collection: collection.Slug, User: user, ExpiresAt: current.ExpiresAt}, nil
}

// Sessions lists every unexpired session owned by the current identity.
func (application *App) Sessions(ctx context.Context, token string) ([]AuthSessionInfo, error) {
	now := time.Now().UTC()
	current, err := application.auth.FindSession(ctx, tokenDigest(token), now)
	if err != nil {
		return nil, authenticationRequired()
	}
	collection, exists := application.authByID[current.CollectionID]
	if !exists {
		return nil, authenticationRequired()
	}
	user, err := application.authUser(ctx, collection, current.UserID)
	if err != nil {
		return nil, authenticationRequired()
	}
	if err := application.authorizeAuth(application.authConfigBySlug[string(collection.Slug)].Access.Session, application.newAuthContext(ctx, AuthOperationRefresh, collection, &user, "", LoginOptions{IPAddress: current.IPAddress, UserAgent: current.UserAgent})); err != nil {
		return nil, err
	}
	records, err := application.auth.ListSessions(ctx, current.CollectionID, current.UserID, now)
	if err != nil {
		return nil, err
	}
	result := make([]AuthSessionInfo, len(records))
	for index, record := range records {
		result[index] = AuthSessionInfo{
			ID: record.ID, CreatedAt: record.CreatedAt, LastSeenAt: record.LastSeenAt,
			ExpiresAt: record.ExpiresAt, IPAddress: record.IPAddress,
			UserAgent: record.UserAgent, Current: record.ID == current.ID,
		}
	}
	return result, nil
}

// RevokeSession revokes one session only when it belongs to the current user.
func (application *App) RevokeSession(ctx context.Context, token, sessionID string) error {
	current, err := application.auth.FindSession(ctx, tokenDigest(token), time.Now().UTC())
	if err != nil {
		return authenticationRequired()
	}
	collection, exists := application.authByID[current.CollectionID]
	if !exists {
		return authenticationRequired()
	}
	user, err := application.authUser(ctx, collection, current.UserID)
	if err != nil {
		return authenticationRequired()
	}
	if err := application.authorizeAuth(application.authConfigBySlug[string(collection.Slug)].Access.Session, application.newAuthContext(ctx, AuthOperationRefresh, collection, &user, "", LoginOptions{IPAddress: current.IPAddress, UserAgent: current.UserAgent})); err != nil {
		return err
	}
	return application.auth.DeleteUserSession(ctx, current.CollectionID, current.UserID, sessionID)
}

// LogoutAll revokes every session owned by the current identity.
func (application *App) LogoutAll(ctx context.Context, token string) error {
	current, err := application.auth.FindSession(ctx, tokenDigest(token), time.Now().UTC())
	if err != nil {
		return authenticationRequired()
	}
	collection, exists := application.authByID[current.CollectionID]
	if !exists {
		return authenticationRequired()
	}
	user, err := application.authUser(ctx, collection, current.UserID)
	if err != nil {
		return authenticationRequired()
	}
	authContext := application.newAuthContext(ctx, AuthOperationLogout, collection, &user, "", LoginOptions{IPAddress: current.IPAddress, UserAgent: current.UserAgent})
	config := application.authConfigBySlug[string(collection.Slug)]
	if err := application.authorizeAuth(config.Access.Session, authContext); err != nil {
		return err
	}
	if err := application.runAuthHooks(config.Hooks.BeforeLogout, authContext); err != nil {
		return err
	}
	if err := application.auth.DeleteUserSessions(ctx, current.CollectionID, current.UserID); err != nil {
		return err
	}
	return application.runAuthHooks(config.Hooks.AfterLogout, authContext)
}

// Logout revokes the current token. Repeating logout is safe.
func (application *App) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	digest := tokenDigest(token)
	record, findError := application.auth.FindSession(ctx, digest, time.Now().UTC())
	var authContext *AuthContext
	var config AuthConfig
	if findError == nil {
		if collection, exists := application.authByID[record.CollectionID]; exists {
			if user, userError := application.authUser(ctx, collection, record.UserID); userError == nil {
				contextValue := application.newAuthContext(ctx, AuthOperationLogout, collection, &user, "", LoginOptions{IPAddress: record.IPAddress, UserAgent: record.UserAgent})
				authContext = &contextValue
				config = application.authConfigBySlug[string(collection.Slug)]
				if err := application.runAuthHooks(config.Hooks.BeforeLogout, contextValue); err != nil {
					return err
				}
			}
		}
	}
	if err := application.auth.DeleteSession(ctx, digest); err != nil {
		return err
	}
	if authContext != nil {
		return application.runAuthHooks(config.Hooks.AfterLogout, *authContext)
	}
	return nil
}

// ChangePassword verifies the current password, validates the replacement,
// then atomically replaces the hash and revokes every session and API key.
func (application *App) ChangePassword(ctx context.Context, sessionToken, currentPassword, nextPassword string) error {
	now := time.Now().UTC()
	session, err := application.auth.FindSession(ctx, tokenDigest(sessionToken), now)
	if err != nil {
		return authenticationRequired()
	}
	collection, exists := application.authByID[session.CollectionID]
	if !exists {
		return authenticationRequired()
	}
	user, err := application.authUser(ctx, collection, session.UserID)
	if err != nil {
		return authenticationRequired()
	}
	identity, _ := user.Values[collection.Auth.IdentityField].StringValue()
	credential, err := application.auth.FindAuthCredential(ctx, collection, identity)
	if err != nil {
		return invalidCredentials()
	}
	matches, compareError := application.passwordMatches(ctx, credential.PasswordHash, currentPassword)
	if compareError != nil {
		return compareError
	}
	if !matches {
		return invalidCredentials()
	}
	authContext := application.newAuthContext(ctx, AuthOperationPasswordReset, collection, &user, identity, LoginOptions{IPAddress: session.IPAddress, UserAgent: session.UserAgent})
	config := application.authConfigBySlug[string(collection.Slug)]
	if err := application.authorizeAuth(config.Access.PasswordReset, authContext); err != nil {
		return err
	}
	if err := application.runAuthHooks(config.Hooks.BeforePasswordReset, authContext); err != nil {
		return err
	}
	if err := application.validatePassword(string(collection.Slug), nextPassword); err != nil {
		return err
	}
	hash, err := application.generatePasswordHash(ctx, nextPassword, collection.Auth.PasswordBcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := application.auth.ChangePasswordHash(ctx, collection, session.UserID, credential.PasswordHash, hash); err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			return invalidCredentials()
		}
		return err
	}
	return application.runAuthHooks(config.Hooks.AfterPasswordReset, authContext)
}

// CreateAPIKey mints a high-entropy bearer secret for the current identity.
// The returned Key is available only from this call.
func (application *App) CreateAPIKey(ctx context.Context, sessionToken, name string, expiresAt time.Time) (APIKey, error) {
	now := time.Now().UTC()
	current, err := application.auth.FindSession(ctx, tokenDigest(sessionToken), now)
	if err != nil {
		return APIKey{}, authenticationRequired()
	}
	collection, exists := application.authByID[current.CollectionID]
	if !exists || !collection.Auth.APIKeys {
		return APIKey{}, authFeatureDisabled("API keys")
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return APIKey{}, &operationengine.Error{Code: "validation", Status: 422, Message: "API key name must contain between 1 and 100 characters"}
	}
	if !expiresAt.IsZero() {
		expiresAt = expiresAt.UTC()
		if !expiresAt.After(now) {
			return APIKey{}, &operationengine.Error{Code: "validation", Status: 422, Message: "API key expiry must be in the future"}
		}
	}
	user, err := application.authUser(ctx, collection, current.UserID)
	if err != nil {
		return APIKey{}, authenticationRequired()
	}
	authContext := application.newAuthContext(ctx, AuthOperationAPIKey, collection, &user, "", LoginOptions{})
	config := application.authConfigBySlug[string(collection.Slug)]
	if err := application.authorizeAuth(config.Access.APIKey, authContext); err != nil {
		return APIKey{}, err
	}
	if err := application.runAuthHooks(config.Hooks.BeforeAPIKey, authContext); err != nil {
		return APIKey{}, err
	}
	id, err := newOpaqueToken(16)
	if err != nil {
		return APIKey{}, err
	}
	secret, err := newOpaqueToken(32)
	if err != nil {
		return APIKey{}, err
	}
	raw := "ridu_" + id + "_" + secret
	record := store.AuthAPIKey{ID: id, TokenHash: tokenDigest(raw), CollectionID: current.CollectionID, UserID: current.UserID, Name: name, CreatedAt: now, ExpiresAt: expiresAt}
	if err := application.auth.CreateAPIKey(ctx, record, tokenDigest(sessionToken), now); err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
			return APIKey{}, authenticationRequired()
		}
		return APIKey{}, err
	}
	result := APIKey{ID: id, Name: name, Key: raw, CreatedAt: now, ExpiresAt: expiresAt}
	if err := application.runAuthHooks(config.Hooks.AfterAPIKey, authContext); err != nil {
		_ = application.auth.DeleteAPIKey(ctx, current.CollectionID, current.UserID, id)
		return APIKey{}, err
	}
	return result, nil
}

// APIKeys lists safe metadata for the current identity's active API keys.
func (application *App) APIKeys(ctx context.Context, sessionToken string) ([]APIKeyInfo, error) {
	now := time.Now().UTC()
	current, err := application.auth.FindSession(ctx, tokenDigest(sessionToken), now)
	if err != nil {
		return nil, authenticationRequired()
	}
	collection, exists := application.authByID[current.CollectionID]
	if !exists || !collection.Auth.APIKeys {
		return nil, authFeatureDisabled("API keys")
	}
	user, err := application.authUser(ctx, collection, current.UserID)
	if err != nil {
		return nil, authenticationRequired()
	}
	authContext := application.newAuthContext(ctx, AuthOperationAPIKey, collection, &user, "", LoginOptions{})
	config := application.authConfigBySlug[string(collection.Slug)]
	if err := application.authorizeAuth(config.Access.APIKey, authContext); err != nil {
		return nil, err
	}
	if err := application.runAuthHooks(config.Hooks.BeforeAPIKey, authContext); err != nil {
		return nil, err
	}
	records, err := application.auth.ListAPIKeys(ctx, current.CollectionID, current.UserID, now)
	if err != nil {
		return nil, err
	}
	result := make([]APIKeyInfo, len(records))
	for index, record := range records {
		result[index] = APIKeyInfo{ID: record.ID, Name: record.Name, CreatedAt: record.CreatedAt, LastUsedAt: record.LastUsedAt, ExpiresAt: record.ExpiresAt}
	}
	if err := application.runAuthHooks(config.Hooks.AfterAPIKey, authContext); err != nil {
		return nil, err
	}
	return result, nil
}

// RevokeAPIKey deletes one key only when it belongs to the current identity.
func (application *App) RevokeAPIKey(ctx context.Context, sessionToken, id string) error {
	current, err := application.auth.FindSession(ctx, tokenDigest(sessionToken), time.Now().UTC())
	if err != nil {
		return authenticationRequired()
	}
	collection, exists := application.authByID[current.CollectionID]
	if !exists || !collection.Auth.APIKeys {
		return authFeatureDisabled("API keys")
	}
	user, err := application.authUser(ctx, collection, current.UserID)
	if err != nil {
		return authenticationRequired()
	}
	authContext := application.newAuthContext(ctx, AuthOperationAPIKey, collection, &user, "", LoginOptions{})
	config := application.authConfigBySlug[string(collection.Slug)]
	if err := application.authorizeAuth(config.Access.APIKey, authContext); err != nil {
		return err
	}
	if err := application.runAuthHooks(config.Hooks.BeforeAPIKey, authContext); err != nil {
		return err
	}
	if err := application.auth.DeleteAPIKey(ctx, current.CollectionID, current.UserID, id); err != nil {
		return err
	}
	return application.runAuthHooks(config.Hooks.AfterAPIKey, authContext)
}

// AuthenticateAPIKey resolves a bearer API key to its user without creating a
// browser session. Every failure intentionally shares one response.
func (application *App) AuthenticateAPIKey(ctx context.Context, raw string) (store.Document, error) {
	identity, err := application.AuthenticateAPIKeyIdentity(ctx, raw)
	return identity.Actor, err
}

// AuthenticateAPIKeyIdentity resolves a bearer API key to its exact auth
// collection and current actor document.
func (application *App) AuthenticateAPIKeyIdentity(ctx context.Context, raw string) (AuthIdentity, error) {
	parts := strings.Split(raw, "_")
	if len(parts) != 3 || parts[0] != "ridu" || parts[1] == "" || parts[2] == "" {
		return AuthIdentity{}, authenticationRequired()
	}
	now := time.Now().UTC()
	record, err := application.auth.FindAPIKey(ctx, parts[1], now)
	if err != nil {
		return AuthIdentity{}, authenticationRequired()
	}
	expected := []byte(record.TokenHash)
	actual := []byte(tokenDigest(raw))
	if len(expected) != len(actual) || subtle.ConstantTimeCompare(expected, actual) != 1 {
		return AuthIdentity{}, authenticationRequired()
	}
	collection, exists := application.authByID[record.CollectionID]
	if !exists || !collection.Auth.APIKeys {
		return AuthIdentity{}, authenticationRequired()
	}
	user, err := application.authUser(ctx, collection, record.UserID)
	if err != nil {
		// Authentication may fail because the request was canceled or the document
		// store had a transient failure. Neither condition proves that this durable
		// credential is orphaned. An authored Read predicate can also deliberately
		// hide an existing owner with the same not-found result, so revoke only after
		// an internal, access-free physical existence probe confirms absence.
		if errors.Is(err, store.ErrNotFound) && application.authOwnerPhysicallyMissing(ctx, collection, record.UserID) {
			application.deleteOrphanedAPIKey(ctx, record)
		}
		return AuthIdentity{}, authenticationRequired()
	}
	if err := application.auth.TouchAPIKey(ctx, record.ID, now); err != nil {
		return AuthIdentity{}, authenticationRequired()
	}
	return AuthIdentity{Collection: collection.Slug, Actor: user}, nil
}

// AuthenticateExternal runs configured request strategies in deterministic
// collection and declaration order.
func (application *App) AuthenticateExternal(ctx context.Context, headers map[string][]string) (store.Document, error) {
	identity, err := application.AuthenticateExternalIdentity(ctx, headers)
	return identity.Actor, err
}

// AuthenticateExternalIdentity runs configured request strategies and retains
// the exact auth collection that recognized the request.
func (application *App) AuthenticateExternalIdentity(ctx context.Context, headers map[string][]string) (AuthIdentity, error) {
	for _, collection := range application.authOrder {
		config := application.authConfigBySlug[string(collection.Slug)]
		for _, strategy := range config.Strategies {
			result, err := strategy.Authenticate(AuthStrategyContext{Context: ctx, CollectionID: collection.ID, Headers: cloneHeaders(headers), Local: application.local})
			if err != nil {
				return AuthIdentity{}, fmt.Errorf("auth strategy %q failed: %w", strategy.Name, err)
			}
			if !result.Authenticated {
				continue
			}
			if result.UserID == "" {
				return AuthIdentity{}, fmt.Errorf("auth strategy %q returned an empty user ID", strategy.Name)
			}
			user, err := application.authUser(ctx, collection, result.UserID)
			if err != nil {
				return AuthIdentity{}, authenticationRequired()
			}
			authContext := application.newAuthContext(ctx, AuthOperationExternalStrategy, collection, &user, "", LoginOptions{})
			if err := application.authorizeAuth(config.Access.Login, authContext); err != nil {
				return AuthIdentity{}, err
			}
			if err := application.runAuthHooks(config.Hooks.BeforeLogin, authContext); err != nil {
				return AuthIdentity{}, err
			}
			if err := application.runAuthHooks(config.Hooks.AfterLogin, authContext); err != nil {
				return AuthIdentity{}, err
			}
			return AuthIdentity{Collection: collection.Slug, Actor: user}, nil
		}
	}
	return AuthIdentity{}, authenticationRequired()
}

// RequestPasswordReset issues a single-use token and passes it to the
// application-owned delivery callback. Unknown identities are intentionally a
// successful no-op so callers cannot enumerate accounts.
func (application *App) RequestPasswordReset(ctx context.Context, collection, identity string) error {
	authCollection, exists := application.authBySlug[collection]
	if !exists {
		return unknownAuthCollection(collection)
	}
	config := application.authConfigBySlug[collection].PasswordReset
	if !authCollection.Auth.PasswordReset || config.Send == nil {
		return authFeatureDisabled("password reset")
	}
	authContext := application.newAuthContext(ctx, AuthOperationForgotPassword, authCollection, nil, identity, LoginOptions{})
	authConfig := application.authConfigBySlug[collection]
	if err := application.authorizeAuth(authConfig.Access.PasswordReset, authContext); err != nil {
		return err
	}
	if err := application.runAuthHooks(authConfig.Hooks.BeforeForgotPassword, authContext); err != nil {
		return err
	}
	credential, err := application.auth.FindAuthCredential(ctx, authCollection, store.CanonicalAuthIdentity(identity))
	if errors.Is(err, store.ErrNotFound) {
		return application.runAuthHooks(authConfig.Hooks.AfterForgotPassword, authContext)
	}
	if err != nil {
		return err
	}
	token, expiresAt, err := application.issueAuthToken(ctx, authCollection, credential.User.ID, store.AuthTokenPasswordReset, time.Duration(authCollection.Auth.PasswordResetTokenDurationSeconds)*time.Second)
	if err != nil {
		return err
	}
	authContext.User = &credential.User
	if err := config.Send(ctx, PasswordResetNotification{Collection: authCollection.Slug, User: credential.User, Token: token, ExpiresAt: expiresAt}); err != nil {
		return err
	}
	return application.runAuthHooks(authConfig.Hooks.AfterForgotPassword, authContext)
}

// ResetPassword consumes a password-reset token exactly once, replaces the
// hash, clears lockout state, and revokes every session and API key in one
// store transaction.
func (application *App) ResetPassword(ctx context.Context, collection, token, password string) error {
	authCollection, exists := application.authBySlug[collection]
	if !exists {
		return unknownAuthCollection(collection)
	}
	if !authCollection.Auth.PasswordReset {
		return authFeatureDisabled("password reset")
	}
	if err := application.validatePassword(collection, password); err != nil {
		return err
	}
	authContext := application.newAuthContext(ctx, AuthOperationPasswordReset, authCollection, nil, "", LoginOptions{})
	config := application.authConfigBySlug[collection]
	if err := application.authorizeAuth(config.Access.PasswordReset, authContext); err != nil {
		return err
	}
	if err := application.runAuthHooks(config.Hooks.BeforePasswordReset, authContext); err != nil {
		return err
	}
	hash, err := application.generatePasswordHash(ctx, password, authCollection.Auth.PasswordBcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	userID, err := application.auth.ResetPasswordWithToken(ctx, authCollection.ID, tokenDigest(token), hash, time.Now().UTC())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return invalidAuthToken()
		}
		return err
	}
	if user, err := application.authUser(ctx, authCollection, userID); err == nil {
		authContext.User = &user
	}
	return application.runAuthHooks(config.Hooks.AfterPasswordReset, authContext)
}

// RequestVerification replaces any outstanding verification token and sends a
// new one. Unknown and already-verified identities are successful no-ops.
func (application *App) RequestVerification(ctx context.Context, collection, identity string) error {
	authCollection, exists := application.authBySlug[collection]
	if !exists {
		return unknownAuthCollection(collection)
	}
	if !authCollection.Auth.VerifyEmail {
		return authFeatureDisabled("email verification")
	}
	authContext := application.newAuthContext(ctx, AuthOperationEmailVerification, authCollection, nil, identity, LoginOptions{})
	authConfig := application.authConfigBySlug[collection]
	if err := application.authorizeAuth(authConfig.Access.Verification, authContext); err != nil {
		return err
	}
	if err := application.runAuthHooks(authConfig.Hooks.BeforeVerification, authContext); err != nil {
		return err
	}
	credential, err := application.auth.FindAuthCredential(ctx, authCollection, store.CanonicalAuthIdentity(identity))
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if credential.Verified {
		return application.runAuthHooks(authConfig.Hooks.AfterVerification, authContext)
	}
	authContext.User = &credential.User
	if err := application.issueVerification(ctx, authCollection, credential.User); err != nil {
		return err
	}
	return application.runAuthHooks(authConfig.Hooks.AfterVerification, authContext)
}

// VerifyEmail consumes a verification token exactly once.
func (application *App) VerifyEmail(ctx context.Context, collection, token string) error {
	authCollection, exists := application.authBySlug[collection]
	if !exists {
		return unknownAuthCollection(collection)
	}
	if !authCollection.Auth.VerifyEmail {
		return authFeatureDisabled("email verification")
	}
	authContext := application.newAuthContext(ctx, AuthOperationEmailVerification, authCollection, nil, "", LoginOptions{})
	authConfig := application.authConfigBySlug[collection]
	if err := application.authorizeAuth(authConfig.Access.Verification, authContext); err != nil {
		return err
	}
	if err := application.runAuthHooks(authConfig.Hooks.BeforeVerification, authContext); err != nil {
		return err
	}
	userID, err := application.auth.VerifyEmailWithToken(ctx, authCollection.ID, tokenDigest(token), time.Now().UTC())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return invalidAuthToken()
		}
		return err
	}
	if user, err := application.authUser(ctx, authCollection, userID); err == nil {
		authContext.User = &user
	}
	return application.runAuthHooks(authConfig.Hooks.AfterVerification, authContext)
}

func (application *App) issueVerification(ctx context.Context, collection schema.Collection, user store.Document) error {
	config := application.authConfigBySlug[string(collection.Slug)].Verify
	if config == nil || config.Send == nil {
		return authFeatureDisabled("email verification")
	}
	token, expiresAt, err := application.issueAuthToken(ctx, collection, user.ID, store.AuthTokenVerifyEmail, time.Duration(collection.Auth.VerificationTokenDurationSeconds)*time.Second)
	if err != nil {
		return err
	}
	return config.Send(ctx, VerifyEmailNotification{Collection: collection.Slug, User: user, Token: token, ExpiresAt: expiresAt})
}

func (application *App) issueAuthToken(ctx context.Context, collection schema.Collection, userID string, purpose store.AuthTokenPurpose, duration time.Duration) (string, time.Time, error) {
	token, err := newOpaqueToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(duration)
	if err := application.auth.CreateAuthToken(ctx, store.AuthToken{TokenHash: tokenDigest(token), Purpose: purpose, CollectionID: collection.ID, UserID: userID, ExpiresAt: expiresAt, CreatedAt: now}); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (application *App) newAuthContext(ctx context.Context, operation AuthOperation, collection schema.Collection, user *store.Document, identity string, options LoginOptions) AuthContext {
	var cloned *store.Document
	if user != nil {
		value := store.CloneDocument(*user)
		cloned = &value
	}
	return AuthContext{Context: ctx, Operation: operation, CollectionID: collection.ID, User: cloned, Identity: store.CanonicalAuthIdentity(identity), IPAddress: options.IPAddress, UserAgent: options.UserAgent, Local: application.local}
}

func (application *App) authorizeAuth(rule AuthAccessRule, authContext AuthContext) error {
	if rule == nil {
		return nil
	}
	allowed, err := rule(authContext)
	if err != nil {
		return &operationengine.Error{Code: "auth_access_failed", Status: 500, Message: "authentication access rule failed", Cause: err}
	}
	if !allowed {
		return &operationengine.Error{Code: "access_denied", Status: 403, Message: "authentication operation is not permitted"}
	}
	return nil
}

func (application *App) runAuthHooks(hooks []AuthHook, authContext AuthContext) error {
	for _, hook := range hooks {
		if err := hook(authContext); err != nil {
			return &operationengine.Error{Code: "auth_hook_failed", Status: 500, Message: "authentication hook failed", Cause: err}
		}
	}
	return nil
}

func cloneHeaders(headers map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(headers))
	for name, values := range headers {
		cloned[name] = append([]string(nil), values...)
	}
	return cloned
}

func (application *App) createSession(ctx context.Context, collection schema.Collection, user store.Document, expectedPasswordHash []byte, now time.Time, options LoginOptions) (AuthSession, error) {
	token, err := newOpaqueToken(32)
	if err != nil {
		return AuthSession{}, err
	}
	id, err := newOpaqueToken(16)
	if err != nil {
		return AuthSession{}, err
	}
	duration := time.Duration(collection.Auth.SessionDurationSeconds) * time.Second
	expiresAt := now.Add(duration)
	record := store.AuthSession{
		ID: id, TokenHash: tokenDigest(token), CollectionID: collection.ID,
		UserID: user.ID, ExpiresAt: expiresAt, CreatedAt: now, LastSeenAt: now,
		IPAddress: options.IPAddress, UserAgent: options.UserAgent,
	}
	if err := application.auth.CreateSession(ctx, record, expectedPasswordHash); err != nil {
		return AuthSession{}, err
	}
	return AuthSession{ID: id, Token: token, Collection: collection.Slug, User: user, ExpiresAt: expiresAt}, nil
}

func (application *App) resolveSession(ctx context.Context, token string, now time.Time) (AuthSession, error) {
	if token == "" {
		return AuthSession{}, authenticationRequired()
	}
	record, err := application.auth.FindSession(ctx, tokenDigest(token), now)
	if err != nil {
		return AuthSession{}, authenticationRequired()
	}
	collection, exists := application.authByID[record.CollectionID]
	if !exists {
		return AuthSession{}, authenticationRequired()
	}
	user, err := application.authUser(ctx, collection, record.UserID)
	if err != nil {
		// A canceled request or transient document-store failure must not revoke a
		// valid browser session. Authored Read predicates also surface as not found,
		// so only a separate physical existence probe can prove that cleanup is safe.
		if errors.Is(err, store.ErrNotFound) && application.authOwnerPhysicallyMissing(ctx, collection, record.UserID) {
			application.deleteOrphanedSession(ctx, record.TokenHash)
		}
		return AuthSession{}, authenticationRequired()
	}
	return AuthSession{ID: record.ID, Token: token, Collection: collection.Slug, User: user, ExpiresAt: record.ExpiresAt}, nil
}

// authOwnerPhysicallyMissing confirms absence inside the exact auth collection
// without running authored access predicates, field redaction, or hooks. The
// probe intentionally includes trashed rows: a restorable identity is still a
// physical owner and its durable credentials must not be destroyed merely
// because ordinary reads currently hide it. Any ambiguous adapter outcome,
// cancellation, timeout, or transaction cleanup failure preserves credentials.
func (application *App) authOwnerPhysicallyMissing(ctx context.Context, collection schema.Collection, userID string) bool {
	probeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), transactionCleanupTimeout)
	defer cancel()
	transaction, err := application.beginAuthReadSnapshot(probeContext)
	if err != nil {
		return false
	}
	_, findError := transaction.Find(probeContext, store.Request{
		Collection:  collection,
		Collections: map[schema.StableID]schema.Collection{collection.ID: collection},
		ID:          userID,
		Deletion:    store.DeletionAll,
	})
	rollbackError := rollbackTransaction(probeContext, transaction)
	return exclusiveStoreNotFound(findError) && rollbackError == nil
}

func (application *App) beginAuthReadSnapshot(ctx context.Context) (store.Transaction, error) {
	if snapshotStore, supported := application.documentStore.(store.SnapshotStore); supported {
		return snapshotStore.BeginSnapshot(ctx)
	}
	return application.documentStore.Begin(ctx)
}

// exclusiveStoreNotFound accepts ordinary single-error wrapping but rejects
// joined or multi-cause failures. A result that contains both NotFound and a
// transient adapter error is not durable proof of physical absence.
func exclusiveStoreNotFound(err error) bool {
	for err != nil {
		if err == store.ErrNotFound {
			return true
		}
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return false
		}
		wrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = wrapped.Unwrap()
	}
	return false
}

func (application *App) deleteOrphanedAPIKey(ctx context.Context, record store.AuthAPIKey) {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), transactionCleanupTimeout)
	defer cancel()
	_ = application.auth.DeleteAPIKey(cleanupContext, record.CollectionID, record.UserID, record.ID)
}

func (application *App) deleteOrphanedSession(ctx context.Context, tokenHash string) {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), transactionCleanupTimeout)
	defer cancel()
	_ = application.auth.DeleteSession(cleanupContext, tokenHash)
}

func (application *App) authUser(ctx context.Context, collection schema.Collection, userID string) (store.Document, error) {
	actor := &store.Document{ID: userID, Values: store.Values{}}
	return application.local.FindWithOptions(ctx, string(collection.Slug), userID, FindOptions{Actor: actor, ActorCollection: collection.Slug})
}

// resolveOptionalAuthIdentity preserves anonymous access while ensuring a
// supplied transport identity is reloaded from its exact auth collection.
// Callers must propagate the returned collection with the actor; dropping it
// would turn a named transport identity into the trusted-local actor form.
func (application *App) resolveOptionalAuthIdentity(ctx context.Context, identity *AuthIdentity) (*store.Document, schema.CollectionSlug, error) {
	if identity == nil {
		return nil, "", nil
	}
	collection, actor, err := application.resolveAuthIdentity(ctx, identity)
	if err != nil {
		return nil, "", err
	}
	return &actor, collection.Slug, nil
}

func (application *App) validatePassword(collection, password string) error {
	resolved := application.authBySlug[collection]
	settings := resolved.Auth
	if utf8.RuneCountInString(password) < settings.PasswordMinLength {
		return &operationengine.Error{Code: "validation", Status: 422, Message: fmt.Sprintf("password must contain at least %d characters", settings.PasswordMinLength)}
	}
	if len([]byte(password)) > settings.PasswordMaxBytes {
		return &operationengine.Error{Code: "validation", Status: 422, Message: fmt.Sprintf("password must not exceed %d bytes", settings.PasswordMaxBytes)}
	}
	if validate := application.authConfigBySlug[collection].Password.Validate; validate != nil {
		if err := validate(password); err != nil {
			return &operationengine.Error{Code: "validation", Status: 422, Message: err.Error(), Cause: err}
		}
	}
	return nil
}

func newOpaqueToken(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func normalizeLoginOptions(options LoginOptions) LoginOptions {
	options.IPAddress = truncateUTF8(strings.TrimSpace(options.IPAddress), 128)
	options.UserAgent = truncateUTF8(strings.TrimSpace(options.UserAgent), 512)
	return options
}

func truncateUTF8(value string, maximumBytes int) string {
	if len(value) <= maximumBytes {
		return value
	}
	value = value[:maximumBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func tokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func unknownAuthCollection(collection string) error {
	return &operationengine.Error{Code: "unknown_auth_collection", Status: 404, Message: fmt.Sprintf("auth collection %q was not found", collection)}
}

func invalidCredentials() error {
	return &operationengine.Error{Code: "access_denied", Status: 401, Message: "invalid email or password"}
}

func authenticationRequired() error {
	return &operationengine.Error{Code: "access_denied", Status: 401, Message: "authentication is required"}
}

func authFeatureDisabled(name string) error {
	return &operationengine.Error{Code: "auth_feature_disabled", Status: 404, Message: name + " is not enabled"}
}

func invalidAuthToken() error {
	return &operationengine.Error{Code: "invalid_auth_token", Status: 400, Message: "the auth token is invalid or expired"}
}
