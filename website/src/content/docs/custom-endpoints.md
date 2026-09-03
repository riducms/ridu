---
title: 'Custom endpoints'
description: 'Add root, collection, and global HTTP handlers with generated OpenAPI metadata.'
product: data
eyebrow: 'Data and APIs'
order: 102
aliases: ['endpoint', 'endpoints', 'custom routes', 'Payload endpoints', 'EndpointContext']
navigation:
  section: 'Extend Ridu'
  order: 30
  title: 'Custom endpoints'
---

Custom endpoints add application-specific HTTP behavior beside Ridu’s generated REST API. Declare
them on `ridu.Config`, a `ridu.Collection`, or a `ridu.Global`; Ridu keeps their public method, path,
and summary in the v1 manifest and generated OpenAPI document while the compiled Go handler stays
runtime-only.

<aside class="callout" data-variant="warning">
<strong>Custom endpoints are anonymous by default</strong>
<p>Ridu resolves a valid session, API key, or external identity into <code>EndpointContext.Actor</code>, but it does not require one or invent endpoint-specific authorization. Check the actor and apply your policy inside the handler.</p>
</aside>

## Mounting and paths {#mounting}

The config location determines the route prefix:

| Declaration                 | Mounted route                              |
| --------------------------- | ------------------------------------------ |
| `ridu.Config.Endpoints`     | `/api<path>`                               |
| `ridu.Collection.Endpoints` | `/api/collections/<collection-slug><path>` |
| `ridu.Global.Endpoints`     | `/api/globals/<global-slug><path>`         |

`Path` starts with `/`. A complete segment beginning with `:` captures one decoded value, so
`/:id/tracking` exposes `id`. Configured static segments and parameter names cannot contain
whitespace, percent escapes, traversal, query strings, fragments, partial parameters, or empty
segments. A path of `/` targets the scope root, and requests may include one trailing slash. Root
endpoints require a static first segment and cannot enter the reserved `/collections/…` or
`/globals/…` namespaces; declare those handlers on the resource.
Captured values must be non-empty and cannot decode to `/` or `\`. When multiple methods share a
path shape, use the same parameter name in each declaration so the generated OpenAPI path remains
unambiguous. Supported methods are `CONNECT`, `DELETE`, `GET`, `HEAD`, `OPTIONS`, `PATCH`, `POST`,
and `PUT`; config resolution accepts any case and stores uppercase metadata.

Collection and global endpoint matching stays inside its known resource scope. Within one scope,
custom endpoints run before a built-in route for the same method. This supports a wrapper or
replacement such as `GET /count`; a custom endpoint registered for a different method
does not hide the built-in method.

## Add a collection endpoint {#collection-endpoint}

This Payload-familiar tracking route is available at
`GET /api/collections/orders/<id>/tracking`:

```go title="content/orders.go"
package content

import (
	"encoding/json"
	"net/http"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Orders = ridu.Collection{
	Slug: "orders",
	Fields: []field.Definition{
		field.Text("reference", field.Required()),
	},
	Endpoints: []ridu.Endpoint{{
		Method:  http.MethodGet,
		Path:    "/:id/tracking",
		Summary: "Read order tracking",
		Handler: func(ctx ridu.EndpointContext) {
			if ctx.Actor == nil {
				http.Error(ctx.Writer, "authentication required", http.StatusUnauthorized)
				return
			}

			id := ctx.RouteParams["id"]
			// The same value is also available through ctx.Request.PathValue("id").
			ctx.Writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(ctx.Writer).Encode(map[string]string{
				"id":     id,
				"status": "in-transit",
			})
		},
	}},
}
```

The typed `Collection` or `Global` field identifies the owning scope without parsing the URL.

## Use the authenticated local API {#local-api}

`EndpointContext.Local` is the application’s Local API. Pass both actor fields into
options so collection/global access, validation, hooks, transactions, and field redaction remain
authoritative:

```go title="content/order-endpoint.go"
order, err := ctx.Local.FindWithOptions(
	ctx.Request.Context(),
	"orders",
	ctx.RouteParams["id"],
	ridu.FindOptions{
		Actor:           ctx.Actor,
		ActorCollection: ctx.ActorCollection,
	},
)
if err != nil {
	// Translate the operation failure to the endpoint's chosen public response.
	http.Error(ctx.Writer, "order unavailable", http.StatusNotFound)
	return
}
```

The endpoint chooses its own response envelope. Return JSON, text, an empty response, or a bounded
stream as the route contract requires. Ridu still applies host and origin checks, CORS preflight,
request IDs and deadlines, identity resolution, panic recovery, and error reporting.

## Root and global endpoints {#root-and-global}

Root endpoints belong on the top-level config:

```go title="content/config.go"
return ridu.Config{
	Name: "Acme Editorial",
	Endpoints: []ridu.Endpoint{{
		Method:  http.MethodPost,
		Path:    "/revalidate/:site",
		Summary: "Revalidate a frontend site",
		Handler: revalidateSite,
	}},
	Collections: []ridu.Collection{Orders},
	Globals:     []ridu.Global{SiteSettings},
}
```

That route mounts at `POST /api/revalidate/:site`. A global declaration with
`Path: "/refresh"` on `site-settings` mounts at
`/api/globals/site-settings/refresh`.

## Body limits, cancellation, and errors {#safety}

`MaxBodyBytes: 0` inherits `ridu.HandlerOptions.MaxBodyBytes` (1 MiB by default). A positive value
sets a tighter route limit through `http.MaxBytesReader`. A negative value opts trusted streaming
code out of that byte limit; pair it with explicit work, duration, and downstream bounds.

Use `ctx.Request.Context()` for every dependency call. It carries client cancellation and the
framework request deadline. `ctx.RequestID` matches the `X-Request-ID` response header.

Call `ctx.ReportError(err, "stable_code")` when the endpoint writes its own public failure response
but an internal dependency error should reach `HandlerOptions.RequestError` and request
observations. The trusted error is never copied into Ridu’s panic response. An authentication-like
endpoint should call `ctx.AdmitAuthAttempt` before work that aliases or repeated requests can
amplify.

## Call a custom endpoint from TypeScript {#typescript}

Custom endpoints own arbitrary request and response shapes, so the SDK exposes raw Fetch rather
than pretending they share collection envelopes:

```ts title="tracking.ts"
const response = await ridu.request(`/api/collections/orders/${encodeURIComponent(id)}/tracking`, {
	method: 'GET'
});

if (!response.ok) throw new Error(`tracking failed: ${response.status}`);
const tracking = (await response.json()) as { id: string; status: string };
```

`request` accepts only a same-origin absolute-path reference. It retains configured credentials,
default and per-call headers, middleware, `AbortSignal`, and `keepalive`, but returns the raw
`Response` for every status and does not add a content type or parse an envelope. Use it for custom
endpoints; keep ordinary content work on the generated typed methods.

## OpenAPI and generation {#openapi}

The v1 manifest contains each custom endpoint’s uppercase method, Payload-style path, and optional
summary. Generation converts named segments to OpenAPI path parameters, for example
`/:id/tracking` to `/{id}/tracking`. The request and response schemas remain endpoint-owned, so the
generated operation documents unconstrained `2XX` and default responses. OpenAPI has no standard
`CONNECT` Path Item operation, so Ridu records that method under the valid `x-ridu-connect` vendor
extension. With `ridu dev` running, saving the config regenerates
`generated/ridu.openapi.json`; inspect and commit that file. Use `ridu generate` only when you need
the same update as a one-shot command, such as in CI.

Executable handlers, body-limit choices, actor data, and secrets never enter the manifest. Invalid
methods, paths, duplicates within one scope, or missing handlers fail config resolution before the
server starts.
