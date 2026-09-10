---
title: 'Performance'
description: 'Make Ridu applications faster by improving queries, indexes, hooks, database placement, and admin code.'
product: core
eyebrow: 'Performance'
order: 230
aliases: ['performance', 'optimize', 'speed', 'memory', 'footprint']
navigation:
  section: 'Develop & operate'
  order: 80
  title: 'Performance'
---

Most Ridu performance work comes down to four choices: keep the application close to its database,
index the fields used to find content, request only the data a caller needs, and keep custom code
proportional to the document being processed. Measure before and after each change with a restored
copy of your own data.

Ridu serves the API and embedded admin from one Go process. It also avoids repeated copies of
unchanged field values and reuses locale projections during an operation. Those framework
optimizations work best when hooks return `operation.Keep`, queries use deliberate selections and
population, and application code reads immutable values without first copying their containers.

## Start with the largest wins {#checklist}

| Check                      | What to change                                                                                                                           | Why it matters                                                                                       |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Database round-trip time   | Run Ridu and the database in the same region and private network.                                                                        | Every list, relationship population, access lookup, and nested operation can wait on the database.   |
| Frequent filters and sorts | Add `.Index()` to suitable stored fields, then create and apply the migration.                                                           | An index avoids scanning every document for common query paths.                                      |
| Response shape             | Use `select`; use explicit `populate` with the smallest useful `depth` and target `select`.                                              | Less data is read, authorized, transformed, serialized, and transferred.                             |
| Lists and counts           | Set a bounded `limit`; call `count` when the caller only needs a total.                                                                  | Large pages increase database, operation-engine, JSON, and browser work.                             |
| Hooks and validators       | Return `operation.Keep` when unchanged, read with `Get`, `Lookup`, `Entries`, or `Elements`, and move external effects to `AfterCommit`. | Ridu can retain immutable backing and avoids holding a database transaction open for unrelated work. |
| Shared blocks              | Register reusable definitions in `Config.Blocks` and select them with `.References(...)`.                                                | The schema and admin can share one definition rather than repeating it at each placement.            |
| Application lifetime       | Construct one `ridu.App` for the process and reuse `app.Local()` and `app.Handler(...)`.                                                 | Rebuilding config, runtime bindings, and database pools per request wastes work and connections.     |
| Admin extensions           | Keep custom components focused and inspect production chunks after adding a large browser dependency.                                    | Static imports become part of the admin build even though Node is absent at runtime.                 |

## Choose the relevant guide {#guides}

| You are seeing                                                     | Read                                                                      | First action                                                  |
| ------------------------------------------------------------------ | ------------------------------------------------------------------------- | ------------------------------------------------------------- |
| Slow filters, sorts, lists, or relationship-heavy reads            | [Database and query performance](/docs/performance/database-and-queries/) | Inspect the query shape and its indexes.                      |
| High allocation, slow saves, or expensive nested content callbacks | [Hook and value performance](/docs/performance/hooks-and-values/)         | Profile the callback and remove unnecessary container copies. |
| Slow author interactions or a large custom admin bundle            | [Admin performance](/docs/performance/admin/)                             | Profile the browser task and inspect production chunks.       |
| Unclear capacity, memory, bundle, or regression results            | [Measure performance](/docs/performance/measurement/)                     | Reproduce a representative workload with repeated trials.     |

## Use the operation engine {#operation-engine}

Call the local Go API for server-side reads and writes. It enters the same operation engine as REST
and the TypeScript SDK, without making a loopback HTTP request:

```go title="content/published_posts.go" focus={17-23}
package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func PublishedPosts(
	ctx context.Context,
	app *ridu.App,
	actor *store.Document,
) (store.Page, error) {
	status, _ := query.NewPath("status")
	return app.Local().List(ctx, "posts", ridu.ListOptions{
		Where: query.Equal(status, query.String("published")),
		Page:  1,
		Limit: 20,
		Actor: actor,
	})
}
```

Keep one `*ridu.App` and pass it to the code that needs it. A local API call still enforces access,
field validation, hooks, transactions, localization, selection, population, and final redaction.
Do not call a `store.Store` or official adapter from application features to make a request faster:
that bypasses those guarantees and couples the feature to storage details.

## Know what Ridu already reuses {#framework-work}

Within one operation, Ridu:

- shares immutable object and list branches that have not changed;
- applies a field-hook replacement to the affected branch instead of materializing the complete
  surrounding list for every occurrence;
- reuses matching locale projections while still checking embedded input each time; and
- retains a value between operation passes when its immutable backing is unchanged.

This is bounded, operation-local reuse. It is not a cross-request response cache. A hook that
rebuilds an equal object still supplies new backing, a changed wide root may need a full projection,
and requested population still performs access-checked target reads. Use an application or edge
cache only when its invalidation and authorization model are explicit.

## Avoid performance shortcuts that change behavior {#safe-optimizations}

| Shortcut                                    | Problem                                                                                              | Use instead                                                                           |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| Query an adapter directly                   | Skips collection access, field redaction, hooks, versions, localization, and operation transactions. | `app.Local()` or a generated typed handle.                                            |
| Populate every relationship to a high depth | Work grows with every reachable reference and returned document.                                     | Explicit `populate` paths with target `select`.                                       |
| Start a goroutine from a transaction hook   | The operation may commit, roll back, or exit before that work is safely owned.                       | `AfterCommit`; use a durable [task](/docs/tasks/) when work needs retries.            |
| Mutate a `store.Value` container            | Nested value backing is immutable and may be shared.                                                 | `CopyObject`, `CopyList`, `CopyDocument`, or `WithListItem` at the mutation boundary. |
| Cache a caller-specific response globally   | Responses can differ by actor, field access, locale, draft mode, and population.                     | Cache a result only with those dimensions and a clear invalidation rule.              |

## Continue with production readiness {#next}

Performance does not replace readiness, graceful shutdown, migration verification, or security
bounds. Use [Production](/docs/production/) for the deployment checklist and the relevant
[PostgreSQL](/docs/postgres/), [SQLite](/docs/sqlite/), or [MongoDB](/docs/mongodb/) guide for the
adapter's operating envelope.
