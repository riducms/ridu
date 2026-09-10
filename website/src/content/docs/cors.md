---
title: 'CORS'
description: 'Allow cross-origin browser clients without weakening Ridu’s HTTP boundary.'
product: data
eyebrow: 'Data and APIs'
order: 105
aliases:
  [
    'cross origin',
    'AllowedOrigins',
    'AllowedRequestHeaders',
    'RIDU_ALLOWED_ORIGINS',
    'preflight'
  ]
navigation:
  section: 'Work with data'
  parent: 'data-access'
  order: 60
  title: 'CORS'
---

Ridu accepts same-origin browser requests without extra configuration. Configure CORS only when
JavaScript running on one origin—such as `https://app.example.com`—calls a Ridu API on another,
such as `https://cms.example.com`.

CORS is a browser boundary, not authentication or authorization. Ridu still authenticates the
caller, applies access rules, validates input, and redacts fields after the browser is allowed to
send the request. Server-to-server calls do not need CORS configuration.

## Do I need CORS? {#when-needed}

An origin is the exact combination of scheme, hostname, and port.

| Client arrangement                                                 | Configuration                                            |
| ------------------------------------------------------------------ | -------------------------------------------------------- |
| Embedded Ridu admin calling its own API                            | None; it is same-origin                                  |
| Frontend and API on the same scheme, hostname, and port            | None                                                     |
| Browser frontend on a different subdomain, domain, scheme, or port | Add the frontend's exact origin                          |
| Server-rendered backend or worker calling Ridu                     | None; authenticate the request instead                   |
| Live-preview page on another origin                                | Configure CORS and the separate preview message boundary |

Do not add the CMS origin merely because it is the request destination. `AllowedOrigins` contains
the origins where calling browser code runs. Ridu also accepts a request whose `Origin` exactly
matches the API's effective request origin.

## Configure a generated application {#generated-application}

Generated servers read a comma-separated `RIDU_ALLOWED_ORIGINS` value:

```sh title="terminal"
export RIDU_ALLOWED_ORIGINS='https://app.example.com,https://staff.example.com'
```

Use complete origins with no path, query, fragment, credentials, or trailing slash. Ridu
canonicalizes host casing and default ports, but it does not support `*` or wildcard subdomains.
List each trusted browser origin.

For local frontend development, add the exact dev-server port:

```sh title="terminal"
RIDU_ALLOWED_ORIGINS='http://localhost:5173' ridu dev
```

`ridu dev` automatically allows its admin dev server. A separate application dev
server still needs its own origin. In production, set `RIDU_ALLOWED_ORIGINS` in the environment
that starts the Ridu binary—for example in your container, service manager, or hosting provider.

## Where `WithHandlerOptions` goes {#handler-options}

### Projects created by `ridu new` {#generated-handler-options}

Set `RIDU_ALLOWED_ORIGINS`; the generated `cmd/server/main.go` already passes its comma-separated
values to `HandlerOptions.AllowedOrigins`. You do not need to edit the Go entrypoint for ordinary
origin configuration.

### Custom `ridu.Execute` entrypoints {#custom-execute}

If the application did not come from `ridu new`, pass `WithHandlerOptions` directly to
`ridu.Execute`, beside `WithStore` and `WithAddress`:

```go title="cmd/server/main.go" add={8-12}
func main() {
	err := ridu.Execute(
		content.Config(),
		ridu.WithStore(func(ctx context.Context) (store.Store, error) {
			return postgres.Open(ctx, os.Getenv("DATABASE_URL"))
		}),
		ridu.WithAddress(":8080"),
		ridu.WithHandlerOptions(ridu.HandlerOptions{
			AllowedOrigins: []string{
				"https://app.example.com",
			},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
}
```

`WithHandlerOptions` is an execute option, not a top-level statement or collection setting.

### Application-owned HTTP servers {#custom-handler}

If you create the application with `ridu.New` and own `http.Server` yourself, do not use
`WithHandlerOptions`. Pass the value to `app.Handler` where you construct the HTTP handler:

```go title="cmd/server/main.go"
app, err := ridu.New(content.Config(), backend)
if err != nil {
	log.Fatal(err)
}

handler := app.Handler(ridu.HandlerOptions{
	AllowedOrigins: []string{"https://app.example.com"},
	AllowedRequestHeaders: []string{
		"X-Workspace-ID",
	},
})

log.Fatal(http.ListenAndServe(":8080", handler))
```

When you construct `http.Server` yourself, check application readiness before accepting traffic;
`ridu.Execute` performs that check before binding automatically.

`AllowedOrigins` and `AllowedHosts` solve different problems. Origins identify browser callers;
hosts restrict the public hostnames accepted by the API. Do not add a hostname to one list merely
because it appears in the other.

See the complete [`HandlerOptions` reference](/reference/ridu/handler-options/) for request,
readiness, proxy, cookie, and worker controls.

## Allow a custom request header {#custom-request-headers}

Skip this section unless your browser request sends a header outside Ridu's defaults. Ridu already
allows `Accept`, `Authorization`, `Content-Type`, and `If-Match`.

For example, if the browser sends `X-Workspace-ID`, add `AllowedRequestHeaders` inside the existing
`HandlerOptions` block in `cmd/server/main.go`:

```go title="cmd/server/main.go" add={4-6}
ridu.WithHandlerOptions(ridu.HandlerOptions{
	AdminAssets:    adminassets.FS(),
	AllowedOrigins: envList("RIDU_ALLOWED_ORIGINS"),
	AllowedRequestHeaders: []string{
		"X-Workspace-ID",
	},
	AllowedHosts: envList("RIDU_ALLOWED_HOSTS"),
})
```

Add only headers that your frontend actually sends. Invalid HTTP header names are ignored, and a
preflight asking for an unlisted header fails closed with `cors_header_denied`.

## Cookies and the TypeScript SDK {#credentials}

`@riducms/sdk` defaults to Fetch credentials mode `include`, so allowed cross-origin responses
carry `Access-Control-Allow-Credentials: true` and echo the exact allowed origin. Plain Fetch calls
must opt into credentials when they use Ridu's session cookie:

```ts title="browser.ts"
const response = await fetch('https://cms.example.com/api/auth/me', {
	credentials: 'include'
});
```

Set `SecureCookies` in production; `ridu.Execute` does so unless disabled. The session
cookie is `SameSite=Lax`. Separate origins that remain on the same site—for example two HTTPS
subdomains—can use the cookie flow. A genuinely cross-site embedded application should put the API
on a same-site origin or use an application-owned bearer flow; CORS alone cannot make the browser
send an ineligible cookie.

## Preflights and allowed methods {#preflight}

Ridu answers a valid preflight with status `204`, credentialed origin headers, and the allowed
methods and headers. You can test the boundary independently of application code:

```sh title="terminal"
curl -i -X OPTIONS 'https://cms.example.com/api/schema' \
  -H 'Origin: https://app.example.com' \
  -H 'Access-Control-Request-Method: GET' \
  -H 'Access-Control-Request-Headers: Authorization, Content-Type'
```

The browser allow-list is `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, and `OPTIONS`. That
includes cross-origin preference writes at `PUT /api/preferences/{key}` when the origin, requested
headers, authentication cookie, and ordinary route authorization all pass.

## Proxies and HTTPS {#proxies}

Same-origin comparison uses the API's effective scheme and host. When TLS terminates at a reverse
proxy, configure only that proxy's immediate network in `TrustedProxyCIDRs` (or
`RIDU_TRUSTED_PROXY_CIDRS`). Ridu trusts forwarded scheme and client-address headers only from
those networks.

Without correct proxy trust, a browser may send `Origin: https://cms.example.com` while the Go
process sees an untrusted HTTP request. Ridu treats those as different origins instead of trusting
a spoofable forwarded header. Keep `AllowedHosts` aligned with the public API
hostname as a separate host-header defense.

## Diagnose a blocked request {#troubleshooting}

Use the browser network panel to inspect the preflight and the API response, then match the stable
Ridu error:

| Error                | Meaning                                                                        |
| -------------------- | ------------------------------------------------------------------------------ |
| `origin_denied`      | The origin is malformed, not listed, or differs from the effective scheme/host |
| `cors_method_denied` | The preflight requested a method outside the browser allow-list                |
| `cors_header_denied` | The preflight requested an unlisted or invalid header                          |

Common mistakes are including a trailing slash, allowing the API rather than the browser origin,
forgetting a local dev-server port, adding a custom header only on the frontend, or terminating
HTTPS at an untrusted proxy. A failed `GET` or `HEAD` may appear only as a browser CORS error because
Ridu withholds the allow-origin response header; mutating requests and rejected preflights also
return a structured `403`.

For session-specific failures, continue with [Authentication](/docs/authentication/). For preview
iframes and `postMessage`, also follow [Live preview](/guides/live-preview/#cross-origin): its
origin, source-window, and channel checks are separate from CORS.
