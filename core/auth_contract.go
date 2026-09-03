package core

import (
	"context"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// AuthOperation identifies an authentication lifecycle boundary.
type AuthOperation string

const (
	AuthOperationLogin             AuthOperation = "login"
	AuthOperationMe                AuthOperation = "me"
	AuthOperationLogout            AuthOperation = "logout"
	AuthOperationRefresh           AuthOperation = "refresh"
	AuthOperationPasswordReset     AuthOperation = "password_reset"
	AuthOperationForgotPassword    AuthOperation = "forgot_password"
	AuthOperationEmailVerification AuthOperation = "email_verification"
	AuthOperationAPIKey            AuthOperation = "api_key"
	AuthOperationExternalStrategy  AuthOperation = "external_strategy"
)

// AuthContext is passed to auth-specific access rules and hooks. Secrets such
// as passwords, session tokens, reset tokens, and API keys are never included.
type AuthContext struct {
	Context      context.Context
	Operation    AuthOperation
	CollectionID schema.StableID
	User         *store.Document
	Identity     string
	IPAddress    string
	UserAgent    string
	Local        *LocalAPI
}

// AuthAccessRule allows or denies one auth operation.
type AuthAccessRule func(AuthContext) (bool, error)

// AuthAccess defines authorization that is specific to authentication rather
// than document CRUD. Nil rules allow the operation.
type AuthAccess struct {
	Login         AuthAccessRule
	PasswordReset AuthAccessRule
	Verification  AuthAccessRule
	APIKey        AuthAccessRule
	Session       AuthAccessRule
}

// AuthHook runs at a documented authentication lifecycle boundary.
type AuthHook func(AuthContext) error

// AuthHooks defines auth-specific lifecycle callbacks. Before hooks can reject
// an operation; if an after-login or after-refresh hook fails, the newly issued
// credential is revoked before the error is returned.
type AuthHooks struct {
	BeforeLogin          []AuthHook
	AfterLogin           []AuthHook
	AfterMe              []AuthHook
	BeforeLogout         []AuthHook
	AfterLogout          []AuthHook
	BeforeRefresh        []AuthHook
	AfterRefresh         []AuthHook
	BeforeForgotPassword []AuthHook
	AfterForgotPassword  []AuthHook
	BeforePasswordReset  []AuthHook
	AfterPasswordReset   []AuthHook
	BeforeVerification   []AuthHook
	AfterVerification    []AuthHook
	BeforeAPIKey         []AuthHook
	AfterAPIKey          []AuthHook
}

// AuthStrategyContext contains normalized request headers for a custom
// authentication strategy. Header names are canonicalized by net/http.
type AuthStrategyContext struct {
	Context      context.Context
	CollectionID schema.StableID
	Headers      map[string][]string
	Local        *LocalAPI
}

// AuthStrategyResult reports whether a strategy recognized the request. A
// matched result must identify a user in the strategy's auth collection.
type AuthStrategyResult struct {
	Authenticated bool
	UserID        string
}

// AuthStrategy integrates an application-owned identity provider. Strategies
// run in declaration order after built-in session and API-key authentication.
type AuthStrategy struct {
	// Name is a stable lowercase kebab-case identifier used in diagnostics.
	Name string
	// Authenticate returns Authenticated false when the request does not belong
	// to this strategy. It must not return raw credentials or untrusted user data.
	Authenticate func(AuthStrategyContext) (AuthStrategyResult, error)
}
