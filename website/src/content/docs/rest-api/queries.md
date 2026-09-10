---
title: 'Queries, responses, and errors'
description: 'Encode Ridu REST queries and handle strict request bodies, success envelopes, limits, and errors.'
product: data
eyebrow: 'REST API'
order: 103
navigation:
  section: 'Work with data'
  parent: 'rest-api'
  order: 30
  title: 'Queries, responses, and errors'
---

Collection list and count routes accept the same filtering and locale context. Unknown query
parameters are rejected, which catches misspelled options instead of silently returning the wrong
result.

## Configure a list request {#query-parameters}

| Query parameter   | Accepted value                                                                                                  |
| ----------------- | --------------------------------------------------------------------------------------------------------------- |
| `page`            | Positive integer; default `1`, maximum `1,000,000`.                                                             |
| `limit`           | Positive integer; default `10`, maximum `100`.                                                                  |
| `sort`            | Repeat for each field; prefix a field with `-` for descending order.                                            |
| `where`           | URL-encoded JSON query expression.                                                                              |
| `select`          | URL-encoded JSON selection object.                                                                              |
| `populate`        | URL-encoded JSON population object with per-target selection.                                                   |
| `depth`           | Integer from `0` to `5`; uniformly expands eligible relationship roots and cannot be combined with `populate`.  |
| `trash`           | Exactly `true` or `false`; `true` requires trash support.                                                       |
| `locale`          | A configured locale code, `all`, or `*`.                                                                        |
| `fallback-locale` | One locale, a comma-separated chain, or `false`; `fallbackLocale` is an alias, but both names may not conflict. |

Build JSON-valued parameters with `JSON.stringify` instead of hand-writing escaped query text:

```ts title="published-posts.ts" focus={2-7,9-10}
const query = new URLSearchParams({
	page: '1',
	limit: '20',
	where: JSON.stringify({ status: { equals: 'published' } }),
	select: JSON.stringify({ title: true, author: true }),
	locale: 'fr',
	'fallback-locale': 'en'
});

// Repeating sort keeps the order explicit: newest first, then title.
query.append('sort', '-createdAt');
query.append('sort', 'title');

const response = await fetch(
	`http://localhost:8080/api/collections/posts?${query}`,
	{ credentials: 'include' }
);
```

Use explicit `populate` when each relationship needs a different target selection. Use `depth` for
a small, uniform expansion. Exact operators and legal field paths come from the generated contract;
see [Querying data](/docs/querying/) for worked filters, selection, population, and performance advice.

## Send a valid request body {#request-bodies}

JSON input is strict. Ridu rejects:

- unknown object properties;
- duplicate JSON keys or trailing data;
- bodies over the configured server limit, which is 1 MiB by default; and
- input that exceeds the structural complexity limit.

Send `Content-Type: application/json` for JSON. Direct uploads use `multipart/form-data`; see
[Collections, globals, and files](/docs/rest-api/collections/#uploads). Custom endpoints and plugin
transports may define their own bounded request shape in the generated OpenAPI document.

## Read the success envelope {#responses}

| Operation                   | Successful JSON shape                                           |
| --------------------------- | --------------------------------------------------------------- |
| Read or mutate one resource | `{ "doc": { … } }` or the operation-specific document envelope. |
| List resources              | `{ "docs": [ … ], "pagination": { … } }`.                       |
| Delete a resource           | `{ "id": "…", "deleted": true }`.                               |
| Resolve capabilities        | A capability-specific envelope defined in OpenAPI.              |

Delete is not a generic `204` response. Check the generated operation schema rather than assuming
that every successful route returns the same body.

Every response includes `X-Request-ID`. CORS permits `Accept`, `Authorization`, `Content-Type`, and
`If-Match` by default when the origin is allowed. Host allowlists, rate limits, timeouts, upload
admission, and plugin-specific body limits can reject a request before the operation handler runs.

## Handle errors by code {#errors}

Failures use one stable JSON envelope. Validation failures use HTTP 422 and include field-addressable
issues; revision and uniqueness conflicts use HTTP 409. Do not branch on the human-readable message.

```json title="validation-response.json" focus={3,6-13}
{
	"error": {
		"code": "validation",
		"status": 422,
		"message": "document validation failed",
		"requestId": "req_…",
		"issues": [
			{
				"code": "required",
				"path": "title",
				"message": "title is required"
			}
		]
	}
}
```

Application validators can add `fieldId`, `collectionId` or `globalId`, and `locale` metadata. A
save issue’s `target` is an opaque correlation token, while `path` is the display path in the
validated candidate. Preserve the target when correlating an issue after an array or block row is
reordered.

Go validators construct [relative `operation.Issue.Target` values](/docs/fields/#validation-issue-targets);
they do not construct the wire token or numeric runtime paths. The [protocol reference](/reference/protocol/)
lists shared envelopes and stable error codes. In TypeScript, the [generated SDK](/docs/typescript-sdk/)
encodes query options and raises a structured `RiduError` for non-success responses.
