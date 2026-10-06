---
title: 'Client options'
description: 'Configure the TypeScript SDK’s fetch, headers and middleware, cancel requests, call custom and plugin endpoints, and use the admin’s access, lock and preference APIs.'
product: sdk
eyebrow: 'TypeScript SDK'
order: 150
aliases:
  [
    'fetch',
    'middleware',
    'headers',
    'abort',
    'signal',
    'request',
    'requestPlugin',
    'custom endpoint',
    'locks',
    'preferences'
  ]
navigation:
  section: 'Work with data'
  parent: typescript-sdk
  order: 50
  title: 'Client options'
---

## Fetch, headers and middleware {#fetch}

```ts title="server-client.ts"
const cms = createClient({
	baseURL: 'https://cms.example.com',
	// A request-scoped fetch, such as SvelteKit's event.fetch.
	fetch,
	credentials: 'include',
	headers: () => ({ 'X-App': 'storefront' }),
	middleware: [
		async (request, next) => {
			const response = await next(request);
			console.debug(request.method, request.url, response.status);
			return response;
		}
	]
});
```

`headers` can be an object or a function, which may be async; headers passed to a single call
override them. Middleware receives each `Request` before it is sent, so it can log, trace, retry or
stub requests in tests. The client never caches or retries on its own; only retry an operation
that is safe to repeat.

## Cancel a request {#cancel}

Every method accepts a `signal`:

```ts
const controller = new AbortController();
const pending = cms.list('posts', { signal: controller.signal });
controller.abort();
```

A cancelled request rejects with the runtime's abort error, not a `RiduError`. Writes also accept
`keepalive`, so they can finish while a page unloads.

## Call a custom endpoint {#custom-endpoints}

`request` calls one of your [custom endpoints](/docs/custom-endpoints/) and returns the raw
`Response`, whatever it contains:

```ts
const response = await cms.request('/api/revalidate/storefront', {
	method: 'POST',
	headers: { 'Content-Type': 'application/json' },
	body: JSON.stringify({ paths: ['/products'] })
});
if (!response.ok)
	throw new Error(`revalidation failed: ${response.status}`);
```

The path must start with `/` and stay on the client's origin. Credentials, headers, middleware and
cancellation apply as usual, but nothing is parsed and no content type is added for you.

## Call a plugin endpoint {#plugin-endpoints}

A compiled plugin's endpoint takes JSON and returns a typed result:

```ts
const suggestion = await cms.requestPlugin<{ result: string }>(
	'seo',
	'generate-title',
	{
		collection: 'pages',
		locale: 'en',
		document: { title: 'About Acme' }
	}
);
```

It sends `POST /api/plugins/{plugin}/{path}` with the client's credentials and handling, and
errors arrive as `RiduError`. Each plugin's guide describes its endpoints and their result types.

## Admin helpers {#coordination}

The APIs the admin uses are public, for custom editing tools:

| Methods                                                               | For                                                                                   |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `list(…, { includeAccess: true })`                                    | a page of documents plus what the caller may do with the collection and each document |
| `collectionAccess`, `globalAccess`                                    | what the caller may do with a document and each of its fields                         |
| `resolveFilteredSelection`                                            | turning a filter into at most 100 IDs the caller may act on, for a bulk action        |
| `documentLock`, `acquireDocumentLock`, `releaseDocumentLock`          | showing and taking the lock that keeps two editors apart                              |
| `preference`, `setPreference`, `deletePreference`, `resetPreferences` | saving per-user interface state                                                       |

Use access results to decide what to show, not as permission: the server checks every operation
again, and access can depend on the document's data.
