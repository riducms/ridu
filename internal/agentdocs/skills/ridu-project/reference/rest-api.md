<!-- Generated from website/src/content/docs/rest-api.md by scripts/sync-agent-docs.ts. -->

# REST API overview

Every Ridu application exposes an HTTP API for its configured collections, globals, authentication,
uploads, versions, and plugins. These routes enter the same operation engine as the local Go API and
generated TypeScript SDK, so access rules, validation, hooks, transactions, localization, selection,
population, and response redaction behave consistently across all three clients.

## Make your first request {#first-request}

A collection uses its authored slug in the URL. This request lists the first ten `posts`:

```ts title="list-posts.ts"
const response = await fetch(
	'http://localhost:8080/api/collections/posts?limit=10',
	{
		// Include this when the request should use a browser login session.
		credentials: 'include'
	}
);

if (!response.ok) {
	throw new Error(`Ridu returned ${response.status}`);
}

const { docs, pagination } = await response.json();
console.log(docs, pagination);
```

The response contains an access-checked `docs` array and pagination metadata. A public collection
does not require authentication. An authenticated request can use the session cookie, a session
token, or an API key as described in [Authentication and actor routes](https://riducms.com/docs/rest-api/authentication/).

## Find the route you need {#routes}

| Goal                                                                | Route family or guide                                                                                            |
| ------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| List, create, read, update, or delete collection documents          | `/api/collections/{collection}` — [Collections, globals, and files](https://riducms.com/docs/rest-api/collections/)                 |
| Read or update a global                                             | `/api/globals/{global}` — [Collections, globals, and files](https://riducms.com/docs/rest-api/collections/)                         |
| Use trash, versions, publishing, scheduling, or uploads             | [Collections, globals, and files](https://riducms.com/docs/rest-api/collections/)                                                   |
| Log in, manage sessions or API keys, or inspect the current actor   | [Authentication and actor routes](https://riducms.com/docs/rest-api/authentication/)                                                |
| Send filters, selection, population, sorting, locale, or pagination | [Queries, responses, and errors](https://riducms.com/docs/rest-api/queries/)                                                        |
| Add an application-owned route                                      | [Custom endpoints](https://riducms.com/docs/custom-endpoints/)                                                                      |
| Call an installed plugin transport                                  | Inspect its generated OpenAPI operation and plugin guide, such as [GraphQL](./graphql.md) or [MCP](./mcp.md) |

The common collection routes are deliberately predictable:

| Method and path                             | What it does                                         |
| ------------------------------------------- | ---------------------------------------------------- |
| `GET /api/collections/{collection}`         | List documents.                                      |
| `POST /api/collections/{collection}`        | Create a document.                                   |
| `GET /api/collections/{collection}/{id}`    | Read one document.                                   |
| `PATCH /api/collections/{collection}/{id}`  | Update one document.                                 |
| `DELETE /api/collections/{collection}/{id}` | Delete one document or move it to trash.             |
| `GET /api/collections/{collection}/count`   | Count matching documents without returning the page. |

Feature-specific routes only exist when the collection or global enables that capability. For
example, Ridu does not generate version routes for a collection without versions.

## Treat OpenAPI as the exact contract {#openapi}

Ridu writes `generated/ridu.openapi.json` from the resolved application manifest. It contains the
concrete paths, request bodies, response envelopes, and schemas enabled by the current application.
Use this file for language-neutral client generation and API tooling; use these guides to understand
how to choose and combine the operations.

The framework route tables describe the stable route families. The generated file remains
authoritative when a plugin supplies a route, an auth feature is disabled, or an application has a
different set of collections and globals.

Application-owned root, collection, and global endpoints also appear in OpenAPI. Their handlers own
their request shape and must opt into authentication deliberately. Compiled plugins commonly use
`/api/plugins/{plugin-key}/{endpoint}`, while a transport plugin can declare an exact path such as
the official GraphQL route. Only the declared HTTP methods are accepted, and plugin errors use the
same Ridu error envelope. See [Custom endpoints](https://riducms.com/docs/custom-endpoints/) for route placement,
precedence, actor context, Local API access, and body limits.

## Choose the right client {#clients}

| Caller                                  | Recommended client                                | Why                                                                      |
| --------------------------------------- | ------------------------------------------------- | ------------------------------------------------------------------------ |
| Go code in the Ridu process             | [Local Go API](./local-api.md)                  | Avoids a loopback HTTP request while preserving the operation engine.    |
| TypeScript                              | [Generated TypeScript SDK](./typescript-sdk.md) | Encodes queries, checks envelopes, and returns generated document types. |
| Another language, service, or HTTP tool | REST plus `generated/ridu.openapi.json`           | Uses an ordinary, language-neutral HTTP contract.                        |

Do not call a database adapter directly from application behavior to save an HTTP hop. Use the
local API inside Go; direct adapter calls bypass access rules, hooks, validation, versions,
localization, and the operation transaction.

## Configure browsers and production clients {#browser-clients}

A browser on another origin needs an allowed CORS origin and must opt into credentials when it uses
the session cookie. See [CORS](https://riducms.com/docs/cors/) for origins, headers, proxy trust, and preflight errors.
Server-to-server clients do not need CORS, but they still need an authorization header for protected
content.

Every response includes `X-Request-ID`. Record it with the HTTP status and stable error code when
reporting a failed request. Continue with [Queries, responses, and errors](https://riducms.com/docs/rest-api/queries/)
for strict JSON rules, success envelopes, concurrency headers, and the error shape.
