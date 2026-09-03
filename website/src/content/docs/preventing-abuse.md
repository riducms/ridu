---
title: 'Prevent abuse'
description: 'Protect a public Ridu API with access rules, authentication limits, request bounds, safe uploads, and edge rate limiting.'
product: core
eyebrow: 'Ship'
order: 230
aliases:
  [
    'preventing abuse',
    'rate limiting',
    'API abuse',
    'login attempts',
    'CSRF',
    'GraphQL complexity',
    'malicious uploads'
  ]
navigation:
  section: 'Develop & operate'
  order: 65
  title: 'Prevent abuse'
---

Use this page before exposing a Ridu application to public traffic. Ridu bounds the work one
request can ask the framework to perform, but it cannot decide who should write your content or how
much traffic one customer is allowed to send. Configure both the application and its public edge.

## Before you go public {#checklist}

1. Add explicit collection and field access rules. In particular, do not leave public create,
   update, delete, upload, or administrative operations open.
2. Keep account lockout enabled and configure HTTP authentication throttling for the traffic you
   expect.
3. Set the public host, exact browser origins, trusted proxy networks, request size, and request
   deadline.
4. If GraphQL is enabled, keep introspection off and choose limits from representative queries.
5. Give every upload collection a narrow MIME allow-list, useful size limit, and appropriate access
   rules.
6. Put tenant-, actor-, route-, and IP-aware rate limiting at the load balancer, gateway, or CDN.
7. Monitor Ridu's stable rejection codes so a limit protects the service without silently breaking
   legitimate clients.

The following sections provide a usable starting point for each item.

## Restrict writes first {#access}

Access rules are the most important abuse control. A hidden admin action or a TypeScript type does
not protect the corresponding API route. Collection and field rules run on REST, GraphQL, the
generated SDK, and local operations.

For a typical editorial collection, public users may read published content while authenticated
authors create content and can only update or delete documents they own. Implement that policy in
Go before adding a public frontend. The [Access control guide](/docs/access-control/) includes the
complete `signedIn` and ownership-filter examples; filtered update and delete decisions remain part
of the database operation rather than becoming a post-query check.

Review special operations too. Draft publication, trash, version restore, document unlock, upload
delivery, auth user creation, API keys, plugin endpoints, and custom endpoints each need the
smallest access surface the application requires.

## Limit login and recovery attempts {#authentication}

An auth collection has two complementary defenses:

- `MaxLoginAttempts` locks one account after consecutive credential failures. It defaults to `5`.
- `HandlerOptions.AuthRateLimit` limits HTTP login, recovery, and verification attempts by client
  and identity. It defaults to `10` attempts per minute and is stored through the selected official
  adapter, so restarting or changing replicas does not clear it.

Keep the account lockout explicit when defining users:

```go title="content/users.go"
package content

import (
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Users = ridu.Collection{
	Slug: "users",
	Auth: true,
	AuthConfig: ridu.AuthConfig{
		MaxLoginAttempts: 5,
		LockDuration:    15 * time.Minute,
		Password: ridu.PasswordPolicy{
			MinLength: 12,
			MaxBytes:  72,
		},
	},
	Fields: []field.Definition{
		field.Email("email", field.Required(), field.Unique()),
	},
}
```

Ridu returns the same credential error for an unknown account, wrong password, and locked account.
Do not replace that response with application code that reveals whether an email address exists.
If users can register themselves, enable email verification and apply bot protection at the public
registration boundary. See [Authentication](/docs/authentication/#password-policy) for recovery,
verification, password validation, and administrative unlocks.

## Bound the HTTP server {#http-boundary}

Generated projects already contain the following `HandlerOptions` block in
`cmd/server/main.go`. Add the highlighted request and authentication limits alongside its existing
host, origin, proxy, readiness, and HSTS settings:

```go title="cmd/server/main.go" add={6-9}
ridu.WithHandlerOptions(ridu.HandlerOptions{
	AdminAssets:             adminassets.FS(),
	AllowedOrigins:          envList("RIDU_ALLOWED_ORIGINS"),
	AllowedHosts:            envList("RIDU_ALLOWED_HOSTS"),
	TrustedProxyCIDRs:       envList("RIDU_TRUSTED_PROXY_CIDRS"),
	MaxBodyBytes:            1 << 20, // 1 × 2²⁰ = 1,048,576 bytes (1 MiB)
	AuthRateLimit:           5,
	AuthRateWindow:          5 * time.Minute,
	RequestTimeout:          20 * time.Second,
	ReadinessTimeout:        envDuration("RIDU_READINESS_TIMEOUT"),
	StrictTransportSecurity: os.Getenv("RIDU_STRICT_TRANSPORT_SECURITY"),
})
```

This is a starting point, not a universal production value. Confirm that normal imports, rich-text
documents, hooks, and custom endpoints finish within the chosen body and time limits. A custom or
plugin endpoint can declare a smaller `MaxBodyBytes`; a negative endpoint value opts that trusted
handler out of the shared body limit and therefore needs its own bound.

Set the deployment environment as narrowly as possible:

```sh title=".env"
RIDU_ALLOWED_HOSTS=cms.example.com
RIDU_ALLOWED_ORIGINS=https://app.example.com
RIDU_TRUSTED_PROXY_CIDRS=10.20.0.0/24
RIDU_STRICT_TRANSPORT_SECURITY=max-age=31536000
```

Leave `RIDU_ALLOWED_ORIGINS` empty when every browser client is same-origin. Only list the origins
where trusted browser code runs. Configure `RIDU_TRUSTED_PROXY_CIDRS` with the immediate proxy
networks you operate, not every possible client address. Otherwise an attacker can influence the
scheme or client IP Ridu uses for origin checks and throttling.

The production executor enables secure cookies. If your application constructs `app.Handler`
directly, set `SecureCookies: true` yourself whenever the public URL uses HTTPS.

## Keep browser requests same-origin {#csrf-and-cors}

Ridu's session cookie is HTTP-only and `SameSite=Lax`. For state-changing requests, Ridu also
checks the `Origin` header and rejects cross-site browser requests without an allowed origin. When
an `Origin` header is absent, a mutating request identified by `Sec-Fetch-Site: cross-site` is
rejected as well.

This protection depends on an accurate public scheme and host. If TLS terminates before the Go
process, trust only that proxy and make sure it replaces untrusted forwarded headers. Test a denied
origin before launch:

```sh title="terminal"
curl -i -X POST 'https://cms.example.com/api/collections/posts' \
	-H 'Content-Type: application/json' \
	-H 'Origin: https://untrusted.example' \
	--data '{"title":"Should not be accepted"}'
```

The response should be `403` with `origin_denied`. Then exercise the same route from each intended
browser origin. CORS only permits the browser to send a request; authentication and access rules
still decide whether it succeeds. The [CORS guide](/docs/cors/) covers credentialed SDK requests,
custom headers, preflights, and proxy diagnosis.

## Keep queries finite {#query-limits}

REST list requests default to 10 documents and reject a `limit` above 100. Relationship population
has a hard maximum depth of 5 and a response may materialize at most 4,096 related documents.
Filters, selected fields, sort fields, JSON nesting, body size, and validation output are also
bounded. Clients should request the smallest page and explicit population paths they actually use.

GraphQL is absent unless the application imports and registers the GraphQL plugin. If you do not
need it, remove `graphqlplugin.New()` and its import. If you do need it, start below the framework
defaults and raise a limit only after measuring a legitimate operation:

```go title="content/config.go" add={12-19}
package content

import (
	"github.com/riducms/ridu"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
)

func Config() ridu.Config {
	return ridu.Config{
		Name: "Acme Editorial",
		Plugins: []ridu.Plugin{
			graphqlplugin.New(graphqlplugin.Options{
				MaxBodyBytes:     512 << 10, // 512 × 2¹⁰ = 524,288 bytes (512 KiB)
				MaxVariableBytes: 128 << 10, // 128 × 2¹⁰ = 131,072 bytes (128 KiB)
				MaxDepth:         8,
				MaxAliases:       30,
				MaxComplexity:    500,
				MaxListLimit:     50,
			}),
		},
		Collections: []ridu.Collection{Users, Posts},
	}
}
```

Introspection remains disabled unless `AllowIntrospection` is explicitly set. Relationship and
upload selections cost more than scalar reads, and custom root fields must declare a realistic
`Cost`. Read [GraphQL safeguards](/docs/graphql/#limits) before changing these values.

## Treat uploads as untrusted {#uploads}

Do not make an upload collection writable merely because its files are intended to become public.
Restrict who can create and replace media, then specify the file types and size the application
actually needs:

```go title="content/media.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

func authenticatedOnly(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor == nil {
		return ridu.Deny(), nil
	}
	return ridu.Allow(), nil
}

var Media = ridu.Collection{
	Slug:   "media",
	Upload: true,
	UploadConfig: ridu.UploadConfig{
		MaxFileSize: 10 << 20, // 10 × 2²⁰ = 10,485,760 bytes (10 MiB)
		MimeTypes:   []string{"image/jpeg", "image/png"},
		Private:     true,
	},
	Access: ridu.CollectionAccess{
		Create: authenticatedOnly,
		Read:   authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	},
	Fields: []field.Definition{
		field.Text("alt", field.Required()),
	},
}
```

Ridu enforces the collection's byte limit, detects MIME type from the file contents, bounds image
dimensions and generated-image work, normalizes names and paths, and sends active content as a
download with restrictive response headers. Remote imports also reject private network targets,
unsafe redirects, and oversized responses.

The zero-value upload size is 10 MiB, while an empty MIME list accepts images and generic binary
content. Prefer an explicit allow-list for an internet-facing collection rather than relying on
that general-purpose default.

Those controls do not scan for malware or moderate what an image depicts. If the application
accepts files from untrusted public users, use a trusted pre-ingestion or quarantine service before
publishing them. Ridu does not currently provide built-in malware quarantine. See
[Uploads and media](/docs/uploads/#limits) for the complete boundary.

## Rate-limit public traffic at the edge {#edge-rate-limits}

Ridu's distributed limiter protects authentication operations; it is not a general API quota.
Apply rate limits before traffic reaches the Go process, especially for:

- login, recovery, verification, and public registration routes;
- collection writes, bulk actions, uploads, remote imports, and custom endpoints;
- GraphQL and expensive read routes that populate relationships;
- any route billed to a tenant or integrated with a paid external service.

Prefer an authenticated actor or tenant key when one exists, with a client-IP fallback for
anonymous traffic. Add separate burst and sustained limits instead of one very low global limit,
and count all application replicas together. If the edge forwards client addresses, configure the
exact immediate proxy CIDRs in Ridu and strip client-supplied forwarding headers there.

Do not rely on an IP limit as authorization, and do not return a successful cached response for a
write. A rejected request should use `429 Too Many Requests` and a useful retry interval.

## Monitor rejections {#monitoring}

Set `HandlerOptions.Observe` to export request duration, status, response size, and Ridu's stable
error code. Alert on sustained changes rather than individual bad requests:

| Error code                                 | Usually means                                                             |
| ------------------------------------------ | ------------------------------------------------------------------------- |
| `rate_limited`                             | Ridu rejected an authentication or bounded-work admission                 |
| `origin_denied`                            | A browser origin or cross-site mutation did not pass policy               |
| `host_denied`                              | The request used a host outside `AllowedHosts`                            |
| `body_too_large`                           | A REST JSON or multipart request exceeded its byte limit                  |
| `body_too_complex`                         | JSON exceeded its structural budget                                       |
| `bad_query`                                | A list, filter, selection, or population exceeded query rules             |
| `request_too_large`, `variables_too_large` | A GraphQL body or encoded variables exceeded the plugin limit             |
| `graphql_*_exceeded`                       | A GraphQL query exceeded its depth, alias, fragment, or complexity budget |
| `request_timeout`                          | Work outlived the configured handler deadline                             |
| `access_denied`                            | Authentication or an application access rule rejected the action          |

Never record credentials, cookies, reset tokens, API keys, complete request bodies, or private
upload URLs in an abuse log. Use the request ID to correlate the public response, observation,
audit event, and trusted internal error report.

## Troubleshooting {#troubleshooting}

| Symptom                                        | What to check                                                                                                                                          |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Every browser write returns `origin_denied`    | Compare the exact scheme, host, and port; then check whether the TLS proxy is inside `TrustedProxyCIDRs`.                                              |
| Real users receive `rate_limited` too quickly  | Distinguish HTTP auth throttling from per-account lockout, inspect the resolved client IP, and tune from measured traffic.                             |
| Changing the email still produces `429`        | Auth throttling includes a client-wide bucket so attackers cannot bypass it by rotating identities.                                                    |
| A legitimate document returns `body_too_large` | Measure its encoded request, then raise only the relevant shared or endpoint limit; do not disable every body limit.                                   |
| A valid upload reports the wrong allowed type  | Ridu uses detected bytes, not the browser filename or claimed `Content-Type`; add only the detected MIME type if the application genuinely accepts it. |
| GraphQL rejects a normal application query     | Remove unused selections, lower list sizes, avoid repeated aliases, and then adjust the measured complexity budget if necessary.                       |

Continue with [Production](/docs/production/), [Security model](/docs/security/),
[Authentication](/docs/authentication/), [Access control](/docs/access-control/), and
[Uploads and media](/docs/uploads/).
