---
title: 'REST API'
description: 'Call Ridu’s generated HTTP contract from any language or Fetch-compatible runtime.'
product: data
eyebrow: 'Data and APIs'
order: 100
navigation:
  section: 'Work with data'
  parent: 'data-access'
  order: 20
  title: 'REST API'
---

Ridu generates an OpenAPI document from the resolved application manifest. The tables below are
the framework route families; your `generated/ridu.openapi.json` is authoritative for the concrete
collection, global, auth, upload, version, and plugin routes enabled by your config.

A browser frontend on another origin must configure the HTTP boundary before calling these routes.
See [CORS](/docs/cors/) for exact origins, credentials, custom headers, proxy trust, and preflight
errors. Server-to-server clients do not need CORS.

## Collection routes {#collection-routes}

`{collection}` is the authored collection slug and `{id}` is the public document ID.

| Task                   | Method and path                                          | Availability or input                                                                                                                    |
| ---------------------- | -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| List / create          | `GET, POST /api/collections/{collection}`                | JSON create for ordinary collections. An auth collection must use `create-user`; an upload collection uses multipart or `remote-upload`. |
| Read / update / delete | `GET, PATCH, DELETE /api/collections/{collection}/{id}`  | `PATCH` accepts `If-Match`. With trash enabled, delete moves the row to trash.                                                           |
| Count                  | `GET /api/collections/{collection}/count`                | Accepts the same filter, locale, and trash context as list.                                                                              |
| Duplicate              | `POST /api/collections/{collection}/{id}/duplicate`      | JSON object contains optional field overrides; unavailable for auth collections.                                                         |
| Copy locale            | `POST /api/collections/{collection}/{id}/copy-locale`    | Body `{ "from": "en", "to": "fr" }`; accepts `If-Match`.                                                                                 |
| Mutate inverse join    | `PATCH /api/collections/{collection}/{id}/joins/{field}` | Body `{ "additions": [...], "removals": [...] }`; only writable joins.                                                                   |
| Bulk operations        | `POST /api/collections/{collection}/bulk`                | Body `{ action, ids, data? }`; 1–100 unique non-empty IDs.                                                                               |

Bulk `action` is exactly one of `update`, `publish`, `unpublish`, `delete`, `restoreDeleted`, or
`deletePermanent`. `data` is required only by `update`. Capability and access checks still run per
document; this route is not a permission bypass.

### Trash {#trash}

These routes exist only for a trash-enabled collection:

| Method and path                                           | Task                                    |
| --------------------------------------------------------- | --------------------------------------- |
| `GET /api/collections/{collection}?trash=true`            | List only trashed documents.            |
| `POST /api/collections/{collection}/{id}/restore-deleted` | Restore one document.                   |
| `DELETE /api/collections/{collection}/{id}/permanent`     | Permanently delete one document.        |
| `DELETE /api/collections/{collection}/trash`              | Permanently empty the collection trash. |

### Versions and scheduling {#versions}

These routes exist only when collection versions are enabled; draft/publish actions additionally
depend on the configured version capability.

| Method and path                                              | Task                                                       |
| ------------------------------------------------------------ | ---------------------------------------------------------- |
| `GET /api/collections/{collection}/{id}/versions`            | List snapshots.                                            |
| `GET /api/collections/{collection}/{id}/versions/{revision}` | Read one positive integer revision.                        |
| `POST /api/collections/{collection}/{id}/publish`            | Publish the current document.                              |
| `POST /api/collections/{collection}/{id}/unpublish`          | Return the current document to draft.                      |
| `POST /api/collections/{collection}/{id}/restore/{revision}` | Restore a snapshot; add `?draft=true` to restore as draft. |
| `GET, POST /api/collections/{collection}/{id}/schedule`      | List jobs or schedule publication with `{ "runAt": "…" }`. |
| `DELETE /api/collections/{collection}/{id}/schedule/{jobId}` | Cancel a scheduled publication.                            |

Publishing, unpublishing, restoring, and scheduling accept `If-Match` for optimistic concurrency.

### Uploads {#uploads}

Upload-enabled collections add these transports:

| Method and path                                    | Content                                                                           |
| -------------------------------------------------- | --------------------------------------------------------------------------------- |
| `POST /api/collections/{collection}`               | `multipart/form-data` with required `file` and optional `data` JSON-object field. |
| `POST /api/collections/{collection}/remote-upload` | JSON `{ "url": "https://…", "data": { … } }`.                                     |
| `PATCH /api/collections/{collection}/{id}/image`   | JSON focal point and optional crop coordinates; accepts `If-Match`.               |
| `GET, HEAD /api/uploads/{collection}/{object-key}` | Access-checked object delivery.                                                   |

Multipart requests are limited to the collection’s maximum file size plus framing allowance.
Remote-host, redirect, MIME, image-dimension, spool, and storage policies come from the configured
upload backend. Object responses are always private and non-cacheable; HTML, SVG, and XML-like
active content is forced to download with a sandbox policy.

## Globals and schema {#globals-and-schema}

| Method and path                                 | Task                                                |
| ----------------------------------------------- | --------------------------------------------------- |
| `GET /api/schema`                               | Read the canonical public manifest envelope.        |
| `GET, PATCH /api/globals/{global}`              | Read or update a global; update accepts `If-Match`. |
| `POST /api/globals/{global}/copy-locale`        | Copy localized values; accepts `If-Match`.          |
| `GET /api/globals/{global}/versions`            | List global versions.                               |
| `GET /api/globals/{global}/versions/{revision}` | Read a global revision.                             |
| `POST /api/globals/{global}/publish`            | Publish a versioned global.                         |
| `POST /api/globals/{global}/unpublish`          | Unpublish a versioned global.                       |
| `POST /api/globals/{global}/restore/{revision}` | Restore a global revision; accepts `If-Match`.      |
| `GET /healthz`                                  | Process liveness.                                   |
| `GET /readyz`                                   | Configured dependency and migration readiness.      |

Global scheduled publishing is not implemented. Version routes are absent when versions are not
configured.

## Authentication and account routes {#authentication}

Auth-collection feature flags determine which routes are generated.

| Method and path                                      | Task                                                        |
| ---------------------------------------------------- | ----------------------------------------------------------- |
| `GET /api/auth/{collection}/bootstrap`               | Check whether one-time first-admin setup is available.      |
| `POST /api/auth/{collection}/create-user`            | Create a user with `{ "data": { … }, "password": "…" }`.    |
| `POST /api/auth/{collection}/login`                  | Authenticate with email and password.                       |
| `POST /api/auth/{collection}/forgot-password`        | Request recovery when enabled.                              |
| `POST /api/auth/{collection}/reset-password`         | Consume a recovery token when enabled.                      |
| `POST /api/auth/{collection}/request-verification`   | Request verification when enabled.                          |
| `POST /api/auth/{collection}/verify`                 | Consume a verification token when enabled.                  |
| `POST /api/auth/{collection}/{id}/unlock`            | Clear login-attempt lock state when enabled and authorized. |
| `GET /api/auth/me`                                   | Read the current session identity.                          |
| `POST /api/auth/refresh`                             | Rotate/refresh the current session.                         |
| `POST /api/auth/logout`, `POST /api/auth/logout-all` | Revoke the current or all actor sessions.                   |
| `POST /api/auth/change-password`                     | Change password and revoke the current cookie session.      |
| `GET /api/auth/sessions`                             | List sessions for the current actor.                        |
| `DELETE /api/auth/sessions/{id}`                     | Revoke one session.                                         |
| `GET, POST /api/auth/api-keys`                       | List API-key metadata or create a key when enabled.         |
| `DELETE /api/auth/api-keys/{id}`                     | Revoke an API key.                                          |

Login establishes the HttpOnly, SameSite=Lax `ridu_session` cookie. Cookie-authenticated browser
requests must include credentials. A raw session token uses `Authorization: Session <token>`;
`Authorization: JWT <token>` remains accepted for compatibility. An API key uses
`Authorization: Bearer <key>`. API-key creation specifically requires cookie authentication, and
the returned secret cannot be retrieved from a later list call.

## Access, preferences, and document locks {#coordination}

| Method and path                                             | Task                                                                         |
| ----------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `POST /api/access/collections/{collection}`                 | Resolve operation and field capabilities for optional `{ id, data, trash }`. |
| `POST /api/access/collections/{collection}/selection`       | Resolve an access-checked `{ where?, trash? }` to at most 100 IDs.           |
| `POST /api/access/globals/{global}`                         | Resolve global capabilities for optional `{ data }`.                         |
| `GET, PUT, DELETE /api/preferences/{key}`                   | Read, set (`{ value }`), or delete an actor preference.                      |
| `DELETE /api/preferences`                                   | Reset all preferences for the actor.                                         |
| `GET, POST, DELETE /api/collections/{collection}/{id}/lock` | Inspect, acquire (`{ takeover? }`), or release an editor lock.               |

Capability responses help build interfaces. Every later operation independently re-runs access
rules and storage predicates, so a cached capability response is never authorization.

## Preview routes {#preview}

| Method and path                                         | Task                                                                 |
| ------------------------------------------------------- | -------------------------------------------------------------------- |
| `POST /api/preview/collections/{collection}/{id}/token` | Mint a short-lived collection preview token.                         |
| `GET /api/preview/collections/{collection}/{id}`        | Read preview content with the preview token as Bearer authorization. |
| `POST /api/preview/globals/{global}/token`              | Mint a global preview token.                                         |
| `GET /api/preview/globals/{global}`                     | Read a global preview.                                               |
| `POST /api/preview/token/revoke`                        | Revoke `{ "token": "…" }`.                                           |

Preview tokens are resource-bound credentials, not general API keys. See [Live preview](/guides/live-preview/)
for iframe URL and update-channel handling.

## Plugin routes {#plugins}

Application-authored root, collection, and global routes appear in the same generated OpenAPI
document. Their handlers own arbitrary request and response shapes, and they are not authenticated
automatically. See [Custom endpoints](/docs/custom-endpoints/) for mounting, path parameters,
actor/LocalAPI context, limits, route precedence, and raw SDK calls.

Compiled plugins contribute concrete method/path pairs to the generated OpenAPI document. Generic
namespaced endpoints use `/api/plugins/{plugin-key}/{endpoint}`; a plugin transport may instead
declare its exact path, such as the official GraphQL transport. Only declared methods are accepted,
request bodies are bounded, and plugin errors use the same Ridu envelope. Inspect your generated
OpenAPI file and the installed plugin's reference module, such as
[`graphql`](/reference/graphql/), for its available routes.

## Query-string encoding {#queries}

Collection list and count routes reject unknown query parameters.

| Parameter         | Encoding and limit                                                                                                          |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `page`            | Positive integer; default `1`, maximum `1,000,000`.                                                                         |
| `limit`           | Positive integer; default `10`, maximum `100`.                                                                              |
| `sort`            | Repeat the parameter for each field; prefix a field with `-` for descending order.                                          |
| `where`           | URL-encoded JSON query expression.                                                                                          |
| `select`          | URL-encoded JSON selection object.                                                                                          |
| `populate`        | URL-encoded JSON population object.                                                                                         |
| `depth`           | Integer from `0` to `5`; expands eligible relationship roots uniformly. Cannot be combined with `populate`.                 |
| `trash`           | Exactly `true` or `false`; `true` requires trash capability.                                                                |
| `locale`          | Locale code, `all`, or `*`.                                                                                                 |
| `fallback-locale` | One locale, a comma-separated chain, or `false`. `fallbackLocale` is an accepted alias, but the two names may not conflict. |

```ts title="raw-fetch.ts"
const query = new URLSearchParams({
	page: '1',
	limit: '20',
	where: JSON.stringify({ status: { equals: 'published' } }),
	select: JSON.stringify({ title: true, author: true }),
	locale: 'fr',
	'fallback-locale': 'en'
});
query.append('sort', '-createdAt');
query.append('sort', 'title');

const response = await fetch(`${origin}/api/collections/posts?${query}`, {
	credentials: 'include'
});
```

Exact operators and resource paths come from the generated contract; see
[Querying data](/docs/querying/).

## Requests, responses, and limits {#requests-responses}

JSON input is strict: request objects reject unknown fields, duplicate keys, trailing data,
excessive structural complexity, and bodies over the configured server limit (1 MiB by default).
Send `Content-Type: application/json` for JSON and `multipart/form-data` for direct uploads.
Successful resource reads use Ridu envelopes such as `{ "doc": … }`, `{ "docs": …,
"pagination": … }`, or capability-specific envelopes. `204` is not the generic delete shape;
delete operations return a JSON `{ id, deleted: true }` envelope.

Every response includes `X-Request-ID`; include it when reporting a failure. CORS permits
`Accept`, `Authorization`, `Content-Type`, and `If-Match` by default when an origin is allowed.
`If-Match` accepts a quoted or unquoted positive revision. Host allowlists, CORS, rate limits,
request timeouts, upload admission, and plugin-specific body limits may reject a request before its
handler runs.

## Errors {#errors}

Failures use one stable JSON envelope. Validation errors use HTTP 422 and put field-addressable
problems in `issues`; conflicts use HTTP 409. Do not branch on message text.

```json title="response.json"
{
	"error": {
		"code": "validation",
		"status": 422,
		"message": "document validation failed",
		"requestId": "req_…",
		"issues": [{ "code": "required", "path": "title", "message": "title is required" }]
	}
}
```

The generated OpenAPI file is the language-neutral authority for enabled paths and schemas. The
[protocol reference](/reference/protocol/) lists the shared envelopes and stable error-code union;
the [SDK reference](/reference/sdk/) lists the typed Fetch methods. Prefer the
[TypeScript SDK](/docs/typescript-sdk/) when TypeScript is available—it performs query encoding and
success-envelope checks for you.
