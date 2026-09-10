---
title: 'Database and query performance'
description: 'Reduce database work with close placement, bounded pools, indexes, selection, and deliberate relationship population.'
product: data
eyebrow: 'Performance'
order: 231
aliases:
  [
    'query performance',
    'database performance',
    'indexes',
    'connection pool'
  ]
navigation:
  section: 'Develop & operate'
  parent: performance
  order: 10
  title: 'Database and queries'
---

Start with the database round trip and query shape. A fast handler cannot compensate for a remote
database, an unindexed frequent filter, or a response that expands far more content than the caller
uses.

## Keep the database close {#placement}

Run Ridu and its database in the same region. Prefer the provider's private network when available,
and measure latency from the running application rather than from a laptop. Relationship
population, access checks that read related records, nested local operations, authentication, and
tasks can add database round trips to an ordinary request.

SQLite is an embedded local-file database for one host and small or local workloads. Do not place
its database on a network filesystem or use it as a horizontally shared database. Use PostgreSQL
for a networked multi-replica application. Use MongoDB only inside its documented replica-set and
release-qualified operating envelope.

## Size the connection pool from real concurrency {#pools}

The official adapters use bounded pools. Zero values select Ridu's defaults, which are a safer
starting point than an unbounded pool. Increase a pool only after observing pool wait time and the
database's own connection capacity; multiply the setting by every running application replica.

| Adapter    | Configuration                                            | Useful controls                                                                                                                                     |
| ---------- | -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| PostgreSQL | `postgres.OpenWithConfig(ctx, postgres.PoolConfig{...})` | `MaxConnections`, `MinConnections`, connection lifetime/idle bounds, statement and lock timeouts, and the separate `MaxUploadLockConnections` pool. |
| MongoDB    | `mongodb.OpenWithConfig(ctx, mongodb.Config{...})`       | `MaxPoolSize`, `MinPoolSize`, connection idle time, connect timeout, and server-selection timeout.                                                  |
| SQLite     | `sqlite.OpenWithConfig(ctx, sqlite.Config{...})`         | `MaxConnections`, `BusyTimeout`, and WAL mode. SQLite still permits one writer at a time.                                                           |

Keep request deadlines shorter than the amount of time a caller is willing to wait. PostgreSQL
statement, lock, and idle-transaction timeouts put server-side bounds around work that survives a
client disconnect or waits on a lock. The generated server already propagates request cancellation
into operations and adapter calls.

## Index frequent filters and sorts {#indexes}

Add `.Index()` to a stored field used frequently in equality filters or sorting. `.Unique()` also
creates the constraint needed to enforce uniqueness; do not add it merely as a performance hint.

```go title="content/posts.go" focus={11-14}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Posts = ridu.Collection{
	Slug: "posts",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Text("slug").Required().Unique(),
		field.Select("status", "draft", "published").Index(),
		field.Date("publishedAt").Index(),
	},
}
```

After changing indexes, create, review, and apply the migration. An index speeds some reads by
adding storage and write work, so index the paths your production queries actually use. Nested,
repeated, JSON, Blocks, and opaque plugin paths do not become generally sortable or indexable just
because a database can represent them internally.

| Field method     | Default | Effect                                                                           |
| ---------------- | ------- | -------------------------------------------------------------------------------- |
| `.Index()`       | Off     | Adds a non-unique index for supported stored scalar/reference fields.            |
| `.Index(false)`  | —       | Removes an explicitly inherited or composed index setting.                       |
| `.Unique()`      | Off     | Requires distinct non-empty values and creates adapter-owned uniqueness support. |
| `.Unique(false)` | —       | Removes the field's uniqueness setting; review the generated migration.          |

## Request only the data you use {#shape}

The same read controls are available through the SDK, REST, and local Go API. Selection reduces the
authored fields in the output. Population expands references into documents and can multiply work,
so prefer explicit paths over a blanket depth.

```ts title="lib/posts.ts" focus={3-12}
const page = await ridu.list('posts', {
	where: { status: { equals: 'published' } },
	sort: ['-publishedAt'],
	page: 1,
	limit: 20,
	select: { title: true, slug: true, author: true },
	populate: {
		author: {
			depth: 1,
			select: { name: true }
		}
	}
});
```

| Read option                 | Default or limit                                              | Cost and use                                                                                               |
| --------------------------- | ------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `where`                     | No caller filter; HTTP accepts at most 100 comparisons        | Filter in the database. Prefer indexed direct paths for frequent queries.                                  |
| `sort`                      | Store-defined stable order; at most 16 unique terms over HTTP | Sort supported stored fields. Ridu adds `id` as a stable tie-breaker when needed.                          |
| `page`                      | `1`                                                           | Choose a positive result page. Deep offset pagination can still cost more as the page grows.               |
| `limit`                     | `10`; HTTP maximum `100`                                      | Bound documents read, processed, serialized, and rendered.                                                 |
| `select`                    | All readable authored fields; at most 256 entries over HTTP   | Return only needed top-level fields plus framework identity metadata.                                      |
| `populate`                  | No population; at most 64 explicit paths and depth `5`        | Expand named relationships with per-target access and redaction. Add a target `select`.                    |
| `depth`                     | `0`; maximum `5`                                              | Expand every root reference uniformly. Prefer `populate` when cost and output shape matter.                |
| `locale` / `fallbackLocale` | Project configuration                                         | Choose exact or fallback content. `locale: 'all'` returns every translation and does more projection work. |
| `draft` / trash mode        | Published, non-trashed reads                                  | Read the lifecycle state the caller needs; these modes still apply access and query rules.                 |

Do not combine `depth` and `populate`. A selection does not automatically populate a selected
relationship. Explicit population also produces a more precise generated TypeScript result than a
numeric depth.

## Count without loading documents {#count}

Use `count` when the caller needs only a total. It applies the same access predicate, caller
filter, locale, draft, and trash mode without constructing and serializing a page of documents.

```ts title="lib/post-count.ts"
const { totalDocs } = await ridu.count('posts', {
	where: { status: { equals: 'published' } }
});
```

Do not request a list with a large limit and discard its documents. For distinct values in Go,
`LocalAPI.Distinct` supports one direct singular stored field and bounded pagination; it is not a
general aggregation API.

## Keep access filters in the query {#access}

Collection access can return a predicate. Ridu combines it with the caller's filter in the atomic
store request for reads, updates, and deletes. Preserve that behavior: fetching broadly and
filtering in application code both wastes work and risks exposing unauthorized rows.

Continue with [Querying data](/docs/querying/) for operators and exact limits, or
[Hook and value performance](/docs/performance/hooks-and-values/) when the database query is fast
but operation processing is expensive.
