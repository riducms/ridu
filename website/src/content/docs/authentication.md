---
title: 'Authentication'
description: 'Configure identities, safe account provisioning, cookie sessions, password recovery, API keys, and custom request strategies.'
product: data
eyebrow: 'Data and APIs'
order: 110
navigation:
  section: 'Work with data'
  parent: 'data-access'
  order: 50
  title: 'Authentication'
---

Ridu models an identity as a document in an auth-enabled collection. That keeps roles, profiles,
access rules, field redaction, hooks, and generated types in the same content model while private
password hashes, session tokens, API keys, and recovery tokens stay in store-owned credential
records. Secrets never become fields, manifest data, hook input, or ordinary API output.

## Define an auth collection {#auth-collections}

Set `Auth: true` and provide a required, unique, non-localized `email` field. It can be an Email or
Text field; `email` is the built-in password strategy's fixed identity name. `Config.Admin.User`
selects which auth collection is allowed to establish an admin identity.

```go title="content/users.go"
package content

import (
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

func Config() ridu.Config {
	return ridu.Config{
		Name:        "Acme Editorial",
		Admin:       ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{Users},
	}
}

var Users = ridu.Collection{
	Slug: "users",
	Auth: true,
	AuthConfig: ridu.AuthConfig{
		SessionDuration: 7 * 24 * time.Hour,
		MaxLoginAttempts: 5,
		LockDuration:    15 * time.Minute,
		Password: ridu.PasswordPolicy{
			MinLength:  12,
			MaxBytes:   72,
			BcryptCost: 12,
		},
		PasswordReset: ridu.PasswordResetConfig{
			TokenDuration: time.Hour,
			Send:          sendPasswordReset,
		},
		Verify: &ridu.VerifyEmailConfig{
			TokenDuration: 24 * time.Hour,
			Send:          sendVerification,
		},
		APIKeys: true,
	},
	Fields: []field.Definition{
		field.Email("email", field.Required(), field.Unique()),
		field.Select("role",
			field.OneOf("admin", "editor", "author"),
			field.Default("author"),
		),
	},
}
```

An auth collection requires a store implementing `store.AuthStore`. Production execution also
expects `store.AuthMaintenanceStore` so expired sessions and keys are pruned; all three official
database adapters implement both. MongoDB availability remains inside its
[bounded production profile](/docs/mongodb/).

## Provision users safely {#provisioning}

Auth users must be created atomically with a password credential. Generic REST, multipart,
remote-upload, and duplicate creation reject auth collections and direct callers to
`/api/auth/{collection}/create-user`. In TypeScript, use the generated SDK:

```ts
const user = await ridu.createAuthUser(
	'users',
	{ email: 'editor@example.com', role: 'editor' },
	'a long application-chosen password'
);
```

The dynamic Go local API can create or import an auth collection document, but that alone does not
create a password credential and the document cannot log in. Use `CreateAuthUser` for a
login-capable account. When importing existing users, attach passwords with trusted `SetPassword`
provisioning code.

The transport creation path has this first-run rule:

1. If `CollectionAccess.Create` is omitted, one anonymous request may create the first active user
   only in `Config.Admin.User`.
2. After that first user, anonymous creation is denied. Other auth collections are never opened by
   the implicit bootstrap rule.
3. If you define `CollectionAccess.Create`, that rule decides every creation request. Use it to
   provide public registration or restrict provisioning to administrators.

When the configured admin collection is eligible, opening `/admin` redirects to the
`/admin/create-first-user` setup screen. It renders the collection fields, creates the account
through the same atomic operation, and signs the new administrator in. Once an active user exists,
the setup route closes and the normal login screen takes over. Automation can check the boolean
`GET /api/auth/{collection}/bootstrap` response before calling `create-user`; the database
transaction remains the authority if two callers race.

The one-time operation can set fields whose Create access normally requires an authenticated
actor, because no actor exists before the first administrator. Schema validation, hooks, password
policy, and the atomic transaction still run. Every later create uses the authored field access
rules normally.

The first admin bootstrap credential is marked verified so a broken or not-yet-configured delivery
system cannot lock the initial operator out. Later users in a verification-enabled collection must
consume a verification token before login.

Go application code uses `app.CreateAuthUser`/`CreateAuthUserWithOptions`; HTTP adapters use
`CreateAuthUserForTransport` to include the one-user bootstrap behaviour. All forms create the
document and credential atomically and run create access, validation, hooks, and version logic.
`app.AuthInitialized` reveals only whether an active auth document
exists, not any user data. `app.SetPassword` is a trusted application operation for an existing
user; it revokes existing sessions and should not be exposed as an unauthorised arbitrary-user
endpoint.

## Exact identity with multiple auth collections {#multiple-auth}

An application can define `users`, `staff`, `customers`, or other auth collections, but document
IDs are not globally unique. Ridu therefore represents a transport identity as both a collection
slug and an actor document:

```go
type AuthIdentity struct {
	Collection schema.CollectionSlug
	Actor      store.Document
}
```

Sessions return the same pair as `AuthSession.Collection` and `AuthSession.User`. Preserve both in
`FindOptions.ActorCollection`, `MutationOptions.ActorCollection`, access/hook context, audit data,
and upload inputs when using the trusted local API. Application services for scheduled work,
preferences, previews, account unlocks, and document locks accept `AuthIdentity` directly and
reload the actor from that exact collection.

`Config.Admin.User` chooses the admin login collection; it does not disable the other collections
for API authentication. `CollectionAccess.Admin` on that collection can still deny a particular
authenticated user from entering the admin.

## Browser sessions {#sessions}

Password login creates an opaque server-stored session and sets a `ridu_session` cookie. The cookie
is `HttpOnly`, `SameSite=Lax`, scoped to `/`, and expires at the session's absolute expiry. Set
`HandlerOptions.SecureCookies` for HTTPS; `ridu.Execute` enables secure cookies unless
`RIDU_SECURE_COOKIES=false` is set.

The generated Fetch client includes credentials. For a separate browser origin, list the exact
origin in `HandlerOptions.AllowedOrigins`; Ridu then emits credentialed CORS responses. SameSite=Lax
still does not make a cross-site embedded authentication design work—deploy the API on a same-site
origin or choose an application-owned bearer flow.

```ts title="src/auth.ts"
await ridu.login('users', {
	email: 'editor@example.com',
	password: 'correct horse battery staple'
});

const current = await ridu.session();
console.log(current.collection, current.user, current.expiresAt);

const sessions = await ridu.sessions();
await ridu.revokeSession(sessions.find((item) => !item.current)!.id);

await ridu.refreshSession();
await ridu.logout();
```

`refreshSession` atomically rotates the bearer token and invalidates the old token; it does not
extend the original absolute expiry. `logout` is idempotent and revokes the current token.
`logoutAll` revokes every session for the identity. Session listings expose only safe metadata—ID,
created/last-seen/expiry times, IP address, user agent, and whether it is current.

For non-cookie clients, send a raw session as `Authorization: Session <token>`. `JWT` remains an
accepted compatibility scheme, but the token is opaque and is not a JWT. Application code can use
`LoginWithOptions` to record a normalized client IP and user agent, and `Session`, `RotateSession`,
`Sessions`, `RevokeSession`, `Logout`, and `LogoutAll` to manage the same lifecycle.

## Password policy and lockout {#password-policy}

Zero-valued auth config resolves to secure defaults:

| Setting               | Default and boundary                                       |
| --------------------- | ---------------------------------------------------------- |
| `SessionDuration`     | 24 hours; configured values must be at least one minute    |
| `Password.MinLength`  | 8 Unicode code points                                      |
| `Password.MaxBytes`   | 72 UTF-8 bytes, bcrypt's safe input ceiling                |
| `Password.BcryptCost` | bcrypt default cost (10); accepted range is 4–16           |
| `MaxLoginAttempts`    | 5; set `-1` to disable account lockout                     |
| `LockDuration`        | 10 minutes; configured lockout must be at least one second |

Ridu uses length rules rather than mandatory character classes. Add application-specific breached
password or product rules with `Password.Validate`; return a user-safe explanation. Raising the
bcrypt cost transparently upgrades an older hash after successful login.

Credential failures use one `access_denied` response—unknown identity, wrong password, lockout,
deleted/inaccessible user, and concurrent credential changes do not reveal which fact was true.
Password hashing is admission-bounded, and HTTP login has an additional identity/client-window
rate limiter configured with `HandlerOptions.AuthRateLimit` and `AuthRateWindow`.
`app.ForceUnlock` and the SDK `forceUnlock` clear failed-attempt state only after collection update
access authorizes the caller.

Changing a password verifies the current password, applies the new policy, and revokes every
session and API key for that user. The browser cookie is cleared by the HTTP endpoint. A trusted
`SetPassword` also revokes sessions; use the reset flow for an untrusted user who has forgotten the
current password.

## Password reset and email verification {#recovery-verification}

Recovery is enabled only when `PasswordReset.Send` is non-nil. Verification is enabled when
`Verify` is non-nil and also needs its `Send` callback. Ridu creates a random, single-use token,
stores only its digest, and gives the raw token once to the trusted callback:

```go
func sendPasswordReset(ctx context.Context, note ridu.PasswordResetNotification) error {
	return mailer.SendReset(ctx, note.User.ID, resetURL(note.Collection, note.Token), note.ExpiresAt)
}
```

The callback owns email/SMS delivery and link construction. Never log the token or place it in a
long-lived job payload without equivalent secret handling. A callback error prevents a success
response, so enqueue durably before returning if delivery must survive process failure.

`RequestPasswordReset` is a successful no-op for an unknown identity. `RequestVerification` is a
successful no-op for unknown or already-verified identities. These behaviours prevent account
enumeration. A reset token is consumed exactly once; resetting replaces the password, clears
lockout, and revokes all sessions and API keys in one auth-store transaction. A verification token
is likewise single-use. Invalid or expired tokens return `invalid_auth_token`.

The SDK exposes `requestPasswordReset`, `resetPassword`, `requestVerification`, and `verifyEmail`;
the framework admin includes the matching account flows.

## API keys {#api-keys}

Set `AuthConfig.APIKeys: true` to allow an authenticated session to mint independent bearer keys.
Creation requires cookie/session authentication and `AuthConfig.Access.APIKey` permission.

```ts
const created = await ridu.createAPIKey({
	name: 'content sync',
	expiresAt: '2026-12-31T23:59:59Z'
});

saveInSecretManager(created.key); // shown once
```

Send the secret as `Authorization: Bearer <key>`. Listing keys returns metadata only; the raw key
cannot be recovered. Keys can be revoked individually, expire automatically, and are all revoked
by password change/reset. API keys authenticate as their current owner document and exact auth
collection; they do not freeze the user's role or bypass later content access checks.

## Custom request strategies {#custom-strategies}

`AuthConfig.Strategies` integrates a proxy assertion, signed header, or application-owned identity
provider without creating a browser session:

```go
Strategies: []ridu.AuthStrategy{{
	Name: "trusted-proxy",
	Authenticate: func(ctx ridu.AuthStrategyContext) (ridu.AuthStrategyResult, error) {
		values := ctx.Headers["X-Authenticated-User"]
		if len(values) != 1 {
			return ridu.AuthStrategyResult{Authenticated: false}, nil
		}
		userID, err := verifyProxyAssertion(values[0])
		if err != nil {
			return ridu.AuthStrategyResult{}, err
		}
		return ridu.AuthStrategyResult{Authenticated: true, UserID: userID}, nil
	},
}},
```

Strategies run after cookie sessions, bearer API keys, and session authorization headers, then in
configured collection/declaration order. Return `Authenticated: false` when the request does not
belong to the strategy; a matched result must return a user ID in that strategy's collection. Ridu
reloads the user through normal read access, then applies login access and before/after-login hooks.

`AuthStrategyContext.Headers` uses canonical HTTP header names. Validate the external credential
completely—Ridu does not know a proxy header is trustworthy. The transport
treats an invalid credential or strategy error as no authenticated actor; protected content must
therefore deny anonymous access. Call `AuthenticateExternalIdentity` directly when application
code needs the exact failure and collection identity.

## Auth access and hooks {#auth-rules-hooks}

Authentication-specific policy lives beside the collection:

| Contract                   | Operations                                                                                                  |
| -------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `AuthAccess.Login`         | Password and matched external-strategy login                                                                |
| `AuthAccess.PasswordReset` | Forgot/reset-password lifecycle                                                                             |
| `AuthAccess.Verification`  | Request/consume verification lifecycle                                                                      |
| `AuthAccess.APIKey`        | Create, list, and revoke API keys                                                                           |
| `AuthAccess.Session`       | List/revoke sessions and logout-all management                                                              |
| `AuthHooks`                | Before/after login, logout, refresh, forgot password, password reset, verification, API key, plus `AfterMe` |

Nil auth rules allow their operation. Auth hooks receive `AuthContext` with operation, collection
stable ID, safe user/identity metadata, IP/user agent, and a local API—but never passwords or raw
tokens. Before hooks can reject. If an after-login or after-refresh hook fails, Ridu revokes the
newly issued credential before returning `auth_hook_failed`.

Authentication only establishes an actor. The collection and field access rules for the next
document operation still run. An auth access callback error becomes `auth_access_failed` (500); a
false decision becomes `access_denied` (403). Disabled optional flows return
`auth_feature_disabled` (404), invalid credentials/authentication return a generic
`access_denied` (401), and invalid recovery tokens return `invalid_auth_token` (400). Treat
messages as human diagnostics and branch on stable codes.

For host/origin/proxy hardening, request audit events, and production cookie settings, continue
with [Security and trust boundaries](/docs/security/) and [REST API](/docs/rest-api/). Exact Go
methods and structures are in the [Go API reference](/reference/ridu/).
