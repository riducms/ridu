---
title: 'TypeScript SDK'
description: 'Use Ridu’s generated, Fetch-based client from browsers, server frameworks, and tooling.'
product: sdk
eyebrow: 'TypeScript SDK'
order: 145
navigation:
  section: 'Work with data'
  parent: 'data-access'
  order: 30
  title: 'TypeScript SDK'
---

`@riducms/sdk` is a small Fetch client with ordinary `Promise<T>` methods. Ridu’s generator binds it
to your application so collection slugs, input shapes, filters, selection, population, and
capability-specific methods are checked by TypeScript.

## Install and create a client {#install-and-create}

Generated applications already include `generated/ridu.generated.ts`. Prefer that module in
application code: it carries the config type produced from the same manifest as the server. When an
external frontend or tool needs the framework-neutral SDK directly, install it with that project's
package manager:

Examples below use `~/generated` as a readable project alias. Configure that alias in your
frontend, or replace it with the relative path to the CMS project's `generated/` directory.

```bash title="terminal" package-manager="bun"
bun add @riducms/sdk
```

```bash title="terminal" package-manager="npm"
npm install @riducms/sdk
```

```bash title="terminal" package-manager="pnpm"
pnpm add @riducms/sdk
```

```bash title="terminal" package-manager="yarn"
yarn add @riducms/sdk
```

```ts title="lib/ridu.ts"
import { createClient } from '~/generated/ridu.generated';

export const ridu = createClient({
	baseURL: 'https://cms.example.com'
});
```

If this code runs in a browser on another origin, configure the API before testing requests. The
[CORS guide](/docs/cors/) covers exact origins, credentialed cookies, custom headers, proxy trust,
and preflight failures. Server-side SDK use does not need CORS.

You can also import `createClient` from `@riducms/sdk`. A generated config augments the SDK’s
`GeneratedRiduConfigRegistry`; when exactly one generated application is visible, inference selects
it automatically. Tooling that loads no generated config—or several applications—should bind the
config explicitly or import each application’s generated wrapper. This avoids silently choosing the
wrong schema.

```ts title="tooling.ts"
import { createClient } from '@riducms/sdk';
import type { RiduConfig } from '~/generated/ridu.generated';

const cms = createClient<RiduConfig>({
	baseURL: process.env.RIDU_URL!
});
```

When `ridu dev` is running, a Go config change regenerates this module automatically. Commit the
result. See [Generated contracts](/docs/generated-contracts/) for one-shot generation and drift
checks, and the complete [SDK reference](/reference/sdk/) for every type and signature.

## Read content {#read-content}

`list`, `find`, and `count` cover ordinary collection reads. `global` reads a singleton. Filters,
selects, and populations are generated from the resource instead of accepting an untyped query bag.

```ts title="posts.ts"
const page = await ridu.list('posts', {
	page: 1,
	limit: 24,
	where: {
		and: [
			{ status: { equals: 'published' } },
			{ title: { contains: 'ridu' } }
		]
	},
	sort: ['-createdAt', 'title'],
	select: { title: true, status: true, author: true },
	populate: { author: { depth: 1, select: { name: true } } }
});

const post = await ridu.find('posts', page.docs[0].id, {
	select: { title: true, author: true }
});
const { totalDocs } = await ridu.count('posts', {
	where: { status: { equals: 'published' } }
});
const site = await ridu.global('site-settings');
```

A group, array, or blocks container supports an `exists` filter. Nested group, array-row, and block
filters use canonical dotted keys such as `"seo.description"`, `"sections.reviewer"`, and
`"layout.quote.source"`. Generated where contracts do not model these paths as nested objects
because the REST API decodes the same dotted path vocabulary used by the operation engine.

Use either `depth` for uniform relationship expansion or `populate` for explicit paths, never both
in one request. Locale-aware reads accept `locale`, including `"all"`, and
`fallbackLocale: string | string[] | false`. The generated output changes to locale-keyed fields
when `locale: "all"` is statically known. Locale-aware mutations accept one generated locale and
reject `"all"`.

## Create and change content {#mutations}

The core mutation family is `create`, `update`, and `delete`. Globals use `updateGlobal`.
Optimistic concurrency is opt-in on revision-aware methods: pass the document revision and the SDK
sends `If-Match`. Methods without a revision contract reject a revision-bearing options variable
instead of silently ignoring it.

```ts title="mutations.ts"
const post = await ridu.create('posts', {
	title: 'SDK guide',
	status: 'draft'
});

const published = await ridu.update(
	'posts',
	post.id,
	{ status: 'published' },
	{ revision: post._revision }
);

await ridu.copyLocale(
	'posts',
	published.id,
	{ from: 'en', to: 'fr' },
	{
		revision: published._revision
	}
);
await ridu.duplicate('posts', published.id);
```

Related task methods are grouped by capability:

- `mutateJoin` mutates a writable inverse join.
- `bulkUpdate`, `bulkPublish`, `bulkUnpublish`, `bulkDelete`, `bulkRestoreDeleted`, and
  `bulkDeletePermanent` operate on explicit document IDs. A request is limited to 100 unique IDs.
- `restoreDeleted`, `deletePermanent`, and `emptyTrash` exist only for trash-enabled collections.
- `copyGlobalLocale` copies localized global values.

The SDK exposes three precise option families: `MutationLocaleOptions` for single-locale mutations
without a revision fence, `MutationOptions` for localized revision-aware mutations, and
`RevisionOptions` for revision-aware operations that do not consume a locale query. `copyLocale`
and `copyGlobalLocale` take their locale scope from `from` and `to`; `updateUploadImage` and
`schedulePublish` likewise use revision-only options.

The type system removes collection slugs from capability-specific methods when the generated
manifest says the capability is absent; the server remains the authorization authority.

## Check a field before saving {#live-validation}

Use `collectionLiveValidation` or `globalLiveValidation` to request feedback for an unsaved form
in a custom client. The built-in admin already handles these requests; configure
`.LiveValidate(...)` in Go to enable them. See [Live server validation](/docs/fields/live-validation/)
for the server setup and custom admin editor bindings.

This example checks that a sale price is lower than the regular price, using the `products`
collection from that guide. Include both unsaved values so the server can compare them.

```ts title="scripts/check-sale-price.ts" focus={7-15,23-31}
import { createClient } from '~/generated/ridu.generated';

const ridu = createClient({
	baseURL: 'http://localhost:8080'
});
const controller = new AbortController();
const pending = ridu.collectionLiveValidation(
	'products',
	{
		// Send the regular price too: the rule compares these values.
		data: { price: 100, salePrice: 120 },
		fields: ['salePrice']
		// Add id: product.id when checking an existing document.
	},
	{ signal: controller.signal }
);

// Call controller.abort() when input changes or the editor closes.
try {
	const { evaluations } = await pending;
	// A superseded response must not put old messages back in the UI.
	if (!controller.signal.aborted) {
		for (const evaluation of evaluations) {
			if (evaluation.status === 'skipped') {
				console.info(evaluation.path, 'Not checked');
				continue;
			}
			// "checked" means the callback ran; it may have found issues.
			for (const issue of evaluation.issues) {
				console.info(issue.path, issue.message);
			}
		}
	}
} catch (error) {
	// Fetch cancellation is expected when a newer edit replaces this check.
	if (!controller.signal.aborted) throw error;
}
```

This request returns a message on `salePrice` because 120 is greater than 100. Changing
`salePrice` to 80 passes this live check.

For your form, replace the example data with its current values and display the returned
`issue.message` beside `issue.path`. Clear old feedback immediately after a relevant edit, call
`controller.abort()`, then create a new controller for the next request. Also cancel when the
editor closes or switches documents or locales. Cancellation and the response guard prevent a
slower, older request from restoring obsolete messages. They do not schedule requests: choose
when your client checks, such as on blur or after a short typing pause.

### Choose the document and fields {#live-validation-input}

| Input            | What to send                                                                                                                                                                                             |
| ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Collection slug  | The resource name, such as `products`, as the first argument.                                                                                                                                            |
| `id`             | The saved collection document's ID when editing it. Omit it for a new document.                                                                                                                          |
| `data`           | The unsaved field values, including other fields the rule reads. Incomplete input is allowed; this is not a generated create or update contract.                                                         |
| `fields`         | Field paths to check, such as `salePrice`, `seo.title`, or `variants.0.salePrice`. These are paths through the submitted data, not field IDs. A container path also selects live checks on its children. |
| `options.locale` | One content locale. Locale fallback and `"all"` are unavailable for live checks.                                                                                                                         |
| `options.signal` | An `AbortSignal` for cancelling an outdated request. Headers and authentication work like other SDK calls.                                                                                               |

For a global, call `globalLiveValidation('site-settings', { data, fields }, options)` with your
global's slug and fields. Do not send `id`; globals are checked as updates even before their first
save. Plugin editors with detached data can also send `embedded` scopes; see
[Live server validation](/docs/fields/live-validation/) before building that request.

### Read the result {#live-validation-results}

The response contains `evaluations`. Each evaluation includes `path`, optional `target`, `status`,
and an `issues` array. Preserve a supplied `target` when associating feedback with nested or
embedded fields; it is an opaque identifier, not a string to parse or manufacture.

| Result                    | What it means for your UI                                                                                               |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `checked`, with issues    | The callback ran and returned feedback to show.                                                                         |
| `checked`, without issues | That live check found no problem in this snapshot. Saving may still fail another rule.                                  |
| `skipped`                 | The check did not run, for example because the input had the wrong type. Do not show it as a successful check.          |
| Rejected request          | Handle `RiduError` for server failures and the Fetch abort error for cancellation. It is not a clean validation result. |

Live checks do not save data or run `.Validate(...)`. Use `create`, `update`, or `updateGlobal` to
save the form and handle their validation errors separately.

## Drafts, versions, and publishing {#versions}

For versioned collections, use `versions` and `version` to inspect snapshots, then `publish`,
`publishChanges`, `unpublish`, or `restore`. `publishChanges` submits edited values and the status
transition as one publish operation, so publish access and hooks cannot be bypassed by an ordinary
update. `schedulePublish`, `scheduledPublishes`, and `cancelScheduledPublish` manage durable
collection publication jobs. Versioned globals use `globalVersions`, `globalVersion`,
`publishGlobal`, `publishGlobalChanges`, `unpublishGlobal`, and `restoreGlobal`; scheduled
publishing is collection-only today.

```ts title="publishing.ts"
const history = await ridu.versions('posts', post.id);
const restored = await ridu.restore(
	'posts',
	post.id,
	history[0].Revision,
	{
		draft: true,
		revision: post._revision
	}
);

const job = await ridu.schedulePublish(
	'posts',
	restored.id,
	new Date('2027-01-02T09:00:00Z'),
	{
		revision: restored._revision
	}
);
```

## Uploads and media {#uploads}

Upload-enabled collections add three creation paths and one metadata operation:

```ts title="media.ts"
const asset = await ridu.upload('media', file, {
	data: { alt: 'Team gathered outside the studio' }
});

const imported = await ridu.uploadFromURL(
	'media',
	'https://assets.example.com/photo.jpg',
	{
		data: { alt: 'Imported photo' }
	}
);

await ridu.updateUploadImage(
	'media',
	asset.id,
	{
		focalX: 0.5,
		focalY: 0.35,
		cropX: 0,
		cropY: 0,
		cropWidth: 1200,
		cropHeight: 630
	},
	{ revision: asset._revision }
);
```

`upload` sends multipart data and `uploadFromURL` asks the configured backend to fetch a URL.
Stored object delivery is an access-checked REST `GET` rather than a document SDK method. File-size,
MIME, image, remote-host, and storage limits come from server config, not the SDK.

## Authentication and account tasks {#authentication}

`login`, `session`, `refreshSession`, `logout`, and `logoutAll` implement the ordinary browser
session flow. The client defaults `credentials` to `"include"`, so the server’s `ridu_session`
HttpOnly cookie is sent on same-origin or correctly configured cross-origin requests.

Auth collections also enable `createAuthUser`. Configured recovery, verification, API-key, and
login-attempt-lock features add `requestPasswordReset`, `resetPassword`, `requestVerification`,
`verifyEmail`, `createAPIKey`, `apiKeys`, `revokeAPIKey`, and `forceUnlock`. Account operations include
`changePassword`, `sessions`, and `revokeSession`.

For a service client, supply an API key as a Bearer credential. For a raw session token, use the
`Session` scheme (`JWT` remains accepted for compatibility). Creating an API key requires a cookie
session, and its secret is returned only by `createAPIKey`; later listings expose metadata only.

```ts title="service-client.ts"
const service = createClient({
	baseURL: 'https://cms.example.com',
	headers: async () => ({
		Authorization: `Bearer ${await loadAPIKey()}`
	})
});
```

Preview reads use separate methods: `preview` and `previewGlobal` place the short-lived
preview token in `Authorization: Bearer …` for that request. Use `createPreviewToken`,
`createGlobalPreviewToken`, and `revokePreviewToken` to manage those credentials. For iframe
updates, pair them with `connectLivePreview`; see [Live preview](/guides/live-preview/).

## Access, locks, and preferences {#coordination}

The admin-facing coordination APIs are public and useful in custom tools:

- `collectionAccess` and `globalAccess` resolve operation and field capabilities for the proposed
  document context. `resolveFilteredSelection` converts an access-checked filter into at most 100
  explicit IDs for safe bulk work.
- `documentLock`, `acquireDocumentLock`, and `releaseDocumentLock` coordinate editors. Passing
  `takeover: true` does not bypass the server’s access rules.
- `preference`, `setPreference`, `deletePreference`, and `resetPreferences` store actor-scoped UI
  state.

Treat capability results as presentation guidance, not a replacement for handling authorization
failure: access can depend on the actor, request, proposed data, or stored row.

## Plugin endpoints {#plugin-endpoints}

Use `requestPlugin` for a compiled plugin's declared namespaced endpoint:

```ts
const generated = await ridu.requestPlugin<{ result: string }>(
	'seo',
	'generate-title',
	{
		collection: 'pages',
		locale: 'en',
		document: { title: 'About Acme' }
	},
	{ signal }
);
```

The method sends `POST /api/plugins/{plugin}/{path}` through the same base URL, credentials,
headers, middleware, cancellation, and `RiduError` handling as the typed content methods. It rejects
invalid plugin keys and empty, whitespace, traversal, query, fragment, or wildcard path segments
before dispatch. The result type is caller-supplied because each compiled plugin owns its endpoint
contract; use that plugin's guide and API reference rather than treating the route as generic
content CRUD.

## Raw custom endpoint requests {#custom-endpoints}

Application custom endpoints can return JSON, text, empty responses, or streams. Use `request` to
retain Fetch semantics instead of forcing an arbitrary route through a typed content envelope:

```ts
const response = await ridu.request('/api/revalidate/storefront', {
	method: 'POST',
	body: JSON.stringify({ paths: ['/products'] }),
	headers: { 'Content-Type': 'application/json' }
});

if (!response.ok)
	throw new Error(`revalidation failed: ${response.status}`);
```

The path must begin with one `/`, stay on the configured origin, and omit a fragment. The method
returns the raw `Response` for every HTTP status, does not parse JSON, and does not supply a default
content type. Client credentials, default/per-call headers, middleware, cancellation, and
`keepalive` still apply. See [Custom endpoints](/docs/custom-endpoints/) for the Go handler contract.

## Fetch, middleware, and cancellation {#fetch-and-errors}

Pass a request-local `fetch` in SvelteKit or another server framework. `headers` may be static or an
async function, and request-specific headers override client defaults. Middleware wraps a concrete
`Request`, which makes tracing, retries, logging, and test interception possible without a separate
transport adapter.

```ts title="server-client.ts"
const cms = createClient({
	baseURL: 'https://cms.example.com',
	fetch,
	credentials: 'include',
	headers: () => ({ 'X-App': 'storefront' }),
	middleware: [
		async (request, next) => {
			const response = await next(request);
			console.debug(
				request.method,
				request.url,
				response.headers.get('X-Request-ID')
			);
			return response;
		}
	]
});

const controller = new AbortController();
const pending = cms.list('posts', { signal: controller.signal });
controller.abort();
await pending;
```

Every request option accepts `signal`; mutation requests also accept `keepalive`. Cancellation is
standard Fetch cancellation and rejects with the runtime’s abort error, not `RiduError`.

## Structured errors {#errors}

Successful calls return the useful value rather than the wire envelope. Non-success HTTP responses
reject with `RiduError`, which preserves `code`, `status`, `message`, optional `requestId`, field
`issues`, and `details`.

```ts title="errors.ts"
import { RiduError } from '@riducms/sdk';

try {
	await ridu.update('posts', id, { title: '' }, { revision });
} catch (error) {
	if (error instanceof RiduError && error.code === 'validation') {
		for (const issue of error.issues)
			console.error(issue.path, issue.message);
		return;
	}
	if (error instanceof RiduError && error.code === 'conflict') {
		// Reload: another writer changed this revision.
		return;
	}
	throw error;
}
```

If a proxy returns a non-Ridu error page, the SDK still produces a fallback `RiduError` from the
HTTP status. It validates Ridu’s success envelopes, but does not runtime-validate every application
document against the generated TypeScript type. Validation and authorization remain server-side.

## Current type boundaries {#boundaries}

Generated `where`, `select`, and `populate` inputs are resource-specific. Literal `select` options
narrow collection and global results while retaining framework metadata. Explicit `populate` paths
infer target documents and target selections, including references inside groups, arrays, Blocks,
and all-locale results. Preserve literal options when reusing a query; widened runtime options
retain broader types. Authored fields can still be omitted by access rules. A numeric `depth` alone
does not provide the same population inference, and sort terms remain strings rather than a
generated union of sortable paths.
The client does not cache, coalesce, or retry automatically; add policy in middleware only when the
operation is safe to repeat. There is no required result-wrapper library and no Node-only transport.

Use the [REST API guide](/docs/rest-api/) for the underlying transport, the
[protocol reference](/reference/protocol/) for shared envelopes and error codes, and
[Capability status](/docs/status/) for wider product limits.
