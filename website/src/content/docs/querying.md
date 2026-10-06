---
title: 'Querying data'
description: 'Filter, sort, paginate, select, populate, and localize collection reads through one finite query language.'
product: data
eyebrow: 'Data and APIs'
order: 95
aliases:
  [
    'query',
    'where',
    'filter',
    'sort',
    'select',
    'populate',
    'depth',
    'pagination',
    'search'
  ]
availability:
  status: limited
  label: 'Finite query vocabulary'
  description: 'Typed filtering, stable sorting, selection, bounded population, and direct-field Local Go distinct work today; full-text, notIn, array all-elements, and geospatial operators remain unavailable.'
  anchor: limits
navigation:
  section: 'Work with data'
  order: 20
  title: 'Querying data'
---

Ridu uses one query vocabulary across the local Go API, REST, SDK, access rules, and store adapters.

## Read options {#options}

| Option              | SDK / REST                 | Local Go API                                                 | Default and purpose                                                  |
| ------------------- | -------------------------- | ------------------------------------------------------------ | -------------------------------------------------------------------- |
| Filter              | `where`                    | `ListOptions.Where`                                          | No caller filter. Combine finite comparisons before pagination.      |
| Sort                | `sort`                     | `ListOptions.Sort`                                           | Stable store order; add one or more supported authored paths.        |
| Page                | `page`                     | `ListOptions.Page`                                           | Page 1. Values are one-based.                                        |
| Page size           | `limit`                    | `ListOptions.Limit`                                          | 10 over HTTP; the HTTP maximum is 100.                               |
| Totals              | `pagination`               | `ListOptions.SkipTotal`                                      | Counted. `pagination: false` skips the count and omits totals.       |
| Selection           | `select`                   | `ListOptions.Select`                                         | All readable authored fields. Restrict top-level output fields.      |
| Explicit population | `populate`                 | `ListOptions.Populate`                                       | None. Expand named relationship/upload paths with per-target access. |
| Uniform population  | `depth`                    | Build matching `query.Population` values                     | 0; maximum HTTP/SDK depth is 5. Do not combine with `populate`.      |
| Content locale      | `locale`, `fallbackLocale` | `Locale`, `FallbackLocales`, `DisableFallback`, `AllLocales` | Project defaults. Choose exact, fallback, or all-locale output.      |
| Lifecycle state     | `draft`, trash options     | `Draft`, `TrashOnly`                                         | Published and non-trashed content.                                   |

`find` accepts the output, population, locale, and lifecycle options that apply to one document.
`count` accepts filtering, locale, draft, and trash options but does not build document output.

For Go examples, start with [Filters and paths](/docs/go-packages/query/) if `query.Equal` or
`query.Expression` is new to you. It explains how to build a filter and use it in an access rule
or local API call.

## Filter with `where` {#where}

Generated TypeScript clients expose only paths from your application schema:

```ts
const page = await client.list('posts', {
	where: {
		and: [
			{ status: { equals: 'published' } },
			{
				or: [
					{ title: { contains: 'ridu' } },
					{ 'author.name': { like: 'Ada Lovelace' } }
				]
			}
		]
	}
});
```

| TypeScript operator               | Go operator                                   | Meaning                                                                                                 |
| --------------------------------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `equals`                          | `query.Equal`                                 | exact scalar equality                                                                                   |
| `notEquals`                       | `query.NotEqual`                              | scalar inequality                                                                                       |
| `in`                              | `query.In`                                    | value equals one member of a non-empty list; for a [set-valued field](#membership), any item equals one |
| `exists`                          | `query.Exists`                                | field is present or absent                                                                              |
| `greaterThan`, `greaterThanEqual` | `query.GreaterThan`, `query.GreaterThanEqual` | ordered string/number comparison                                                                        |
| `lessThan`, `lessThanEqual`       | `query.LessThan`, `query.LessThanEqual`       | ordered string/number comparison                                                                        |
| `contains`                        | `query.Contains`                              | case-insensitive substring of a stored string                                                           |
| `like`                            | `query.Like`                                  | case-insensitive match for every whitespace-delimited search word                                       |
| `and`, `or`, `not`                | `query.And`, `query.Or`, `query.Not`          | recursive logical composition                                                                           |

`and` and `or` require at least two children; `not` requires exactly one. HTTP `where` accepts at
most 100 comparison expressions, nesting depth 16, and 100 values in one `in` expression. A query
path has at most 64 segments and 4,096 bytes. Invalid shapes fail with `bad_query` before storage.

Lists, has-many fields and polymorphic relationships accept only `in` (negated with `not`),
`exists` and null equality; see [Filter by membership](#membership). Any other operator on them
fails with `bad_query` and an `unsupported_operator` issue on every transport and database,
instead of matching nothing.

### Current query limits {#limits}

There is currently no `notIn`, full-text ranking, array all-elements operator, or general
geospatial comparison. The Local Go API supports distinct values for one direct singular stored
field; relationship-path population, custom aggregate ordering, and a public REST distinct route
remain outside that focused contract. A point field can be stored, returned, and rendered without
implying a radius or bounding-box query API.

## Filter inside groups, arrays and blocks {#paths}

Paths use authored field names separated by dots, such as `seo.title`. An array adds its field
name, and a Blocks field adds its name and the block slug. A note's text inside a card inside a
page layout is therefore `layout.card.children.note.text`. A path can pass through any number of
groups, arrays and Blocks fields. Row positions never appear in it.

```ts
const pages = await client.list('pages', {
	where: { 'layout.card.children.note.text': { contains: 'launch' } }
});
```

A path through arrays or Blocks fields reaches the field in every row, and each block slug only
reaches rows of that block type. In the example, only `note` rows inside `card` rows are compared;
other blocks beside the note, and notes inside other block types, are skipped. A comparison then
matches a document by the values it reaches:

| Comparison                                                 | Matches a document when                                                         |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------- |
| `equals`, `in`, `contains`, `like`, ranges, `exists: true` | at least one reached value matches                                              |
| `notEquals`, `exists: false`, `not`                        | its opposite (`equals`, `exists: true`, the inner filter) matches no such value |
| `equals: null`, or `null` in `in`                          | a reached value is null, or the path reaches no value                           |

Each comparison is matched on its own, so `and` can be satisfied by two different rows:
`{ and: [{ 'layout.card.heading': { equals: 'A' } }, { 'layout.card.tone': { equals: 'loud' } }] }`
matches a layout with one card titled A and another, loud card.

Localized fields on the path use the request locale and its fallbacks, as they do at the top
level. A field with a read access rule cannot be filtered, and neither can anything inside it.
Every database adapter returns the same documents for these filters.

Some paths have no answer for an operation. Ridu rejects them before reading, with a `400` whose
issue has code `unsupported_path`:

- Sorting by a path that passes through or ends at an array or Blocks field: a document has no
  single value there to order by.
- `Distinct` on a field that is not a direct field of the collection.
- Paths inside the blocks of a rich-text or other plugin field. Those paths describe the editor's
  data; they are not filters. Store values you need to search in ordinary fields.

Filters do not populate relationships first. They operate on persisted fields according to the
schema/store contract. Population is a separate output step and re-authorizes every target.

### Compare JSON values {#json}

A [JSON field](/docs/fields/json/) holds any value, so a comparison applies to it only when the
stored value is a scalar of the operand's type. `contains` and `like` match a stored string,
ignoring case: `{ metadata: { contains: 'plan' } }` matches `"Launch PLAN"`. An array or object
never contains text, even when one of its items would, and never equals a scalar; it only counts
as present for `exists`. Use a [text list](/docs/fields/lists/) when items should match one by one.

A path inside a JSON value, such as `metadata.owner`, follows object keys and stops at an array:
`metadata.items.note` reaches nothing when `items` is an array. PostgreSQL, SQLite and the
in-memory store compare such paths; MongoDB rejects them with `unsupported_path`.

## Sort stably {#sort}

Pass repeated sort terms in priority order. Prefix a path with `-` for descending order:

```ts
const page = await client.list('posts', {
	sort: ['-createdAt', 'title']
});
```

In Go, build the same terms with `query.Desc` and `query.Asc`:

```go
page, err := app.Local().List(ctx, "posts", ridu.ListOptions{
	Sort: []query.Sort{query.Desc("createdAt"), query.Asc("title")},
})
```

REST repeats the query key: `?sort=-createdAt&sort=title`. The HTTP API accepts at most 16 unique
sort fields. A sort path must reach one stored scalar, directly or through groups such as
`seo.title`. These paths are not sortable:

- groups themselves, and fields inside a localized group, where each locale holds its own group;
- arrays, blocks, and fields inside them (see [nested paths](#paths));
- JSON and plugin fields, and paths inside them;
- text and number lists, has-many selects, relationships and uploads, and polymorphic
  relationships.

The operation engine rejects them before any database runs, so every transport reports the same
`bad_query` error with an `unsupported_path` issue naming the path; REST answers `400`. A
localized field outside a localized group sorts by its value in the request locale. The store
appends `id` as the final tie-breaker when needed, which keeps page ordering deterministic when
authored values are equal.

Generated SDK sort terms are validated strings today, not path unions; the server remains the
authority for whether a field can be sorted.

## Paginate and count {#pagination}

REST list requests default to page 1 and limit 10. Both values must be positive; the HTTP maximum
limit is 100. SDK page results keep metadata under `pagination`:

```ts
const { docs, pagination } = await client.list('posts', {
	page: 2,
	limit: 25
});

console.log(pagination.totalDocs);
console.log(pagination.totalPages);
console.log(pagination.hasNextPage);
```

Every list counts all matching documents by default, so `totalDocs` and `totalPages` are exact.
That count grows with the collection. When you only need the next page, as in infinite scroll,
"Load more", or a feed, pass `pagination: false`:

```ts
const { docs, pagination } = await client.list('posts', {
	page: 2,
	limit: 25,
	pagination: false
});

if (pagination.hasNextPage) showLoadMore();
// pagination.totalDocs and pagination.totalPages are absent.
```

No store adapter runs a count for that request. It reads one document beyond `limit`, returns at most
`limit`, and uses the extra document only to set `hasNextPage`. So `hasNextPage` stays exact, and
`page`, `limit` and `hasPrevPage` are unchanged. Ridu omits `totalDocs` and `totalPages` rather than
estimating them. Filters, access rules, sorting, population, locale, draft and trash work as in any
list.

A literal `pagination: false` changes the SDK result type: its `pagination` has no `totalDocs` or
`totalPages`. A `pagination` option widened to `boolean` gives totals typed `number | undefined`.
REST takes `?pagination=false`, GraphQL list queries take `pagination: false`, and Go passes
`ridu.ListOptions{SkipTotal: true}`. A Go page then has a nil `Total`, and `HasNextPage` is exact.

Use `client.count('posts', { where })` when you only need the authorized total. Counts use the same
caller filter, access predicate, locale, draft, and trash semantics as lists.

Go applications can use `LocalAPI.Distinct` for a paginated, ascending set of values from one
direct scalar, singular relationship, singular upload, or `id` field. The caller filter and
collection read predicate stay combined in the adapter query, and field read access is checked
before values are selected. A selected field with a configured read-access rule is rejected:
Ridu rules may depend on each document and cannot be safely reduced to one aggregate predicate.

## Select output fields {#select}

`select` projects top-level authored fields while retaining framework identity fields:

```ts
const post = await client.find('posts', 'post_123', {
	select: { title: true, summary: true, author: true }
});
```

HTTP selection is a JSON object and accepts at most 256 entries. Selecting a relationship does not
populate it; it remains an ID or polymorphic reference unless `populate` is also requested.

Literal `select` options narrow the TypeScript result to the selected fields and framework
metadata, so unselected authored fields are absent from the inferred type. Selected fields can
still be omitted by access rules. When reusing options, preserve their literal types with `as const`
or a suitable `satisfies` constraint; a selection widened to runtime booleans retains the broader
output type.

## Populate relationships {#populate}

Population replaces relationship or upload references with access-checked target documents:

```ts
const page = await client.list('posts', {
	populate: {
		author: {
			depth: 1,
			select: { name: true, avatar: true }
		},
		'sections.quote.source': true
	}
});
```

Each target read re-applies collection access, field redaction, localization, and nested population
rules. Missing or denied targets do not become an authorization side channel. Population supports
at most 64 explicitly requested paths, maximum depth 5, 256 recursively expanded schema paths, and
a shared budget of 4,096 materialized related documents per operation. These are request-wide
ceilings, not per row.

REST also accepts `depth=1` to expand every root reference field. Do not combine `depth` and
`populate`; use explicit population for production queries that need predictable shape and cost.

Explicit literal `populate` paths infer target document types and apply target selections. This
also works for references nested in groups, arrays, and Blocks, and for all-locale output. Widened
runtime options retain broader types. A numeric `depth` alone does not provide the same inference;
use explicit population for a precisely typed consumer.

## Query localized and trashed content {#modes}

List, find, count, sort, filter, and population use the selected content locale. SDK requests accept
`locale` and `fallbackLocale`; REST uses `locale` and `fallback-locale` (with `fallbackLocale` as an
alias). `locale=all` returns locale-keyed values and makes ordinary mutations read-only. See
[Localization](/docs/localization/) for exact fallback and copy-locale behavior.

Trash-enabled collections keep deleted documents out of ordinary queries. Set the dedicated trash
mode to list only deleted documents; there is no mixed live-and-deleted query. Restore and permanent
delete are separate, access-checked operations described in [Editorial workflows](/docs/editorial-workflows/).

## Access filters remain atomic {#access-filters}

An access rule may allow, deny, or return a query predicate. Ridu combines a filtered access
decision with the caller's `where` inside the store request for reads, updates, deletes, duplicate,
and other target operations. It never reads an unauthorized page and removes rows afterward. See
[Access control](/docs/access-control/) for the operation matrix.

A rule's predicate follows the same query rules as a caller's filter, except that field read rules
do not limit it. Ridu checks it when the rule runs, before any database: an unknown path or an
operator a field does not support, such as `equals` on a has-many relationship, fails the
operation closed with a server error, code `invalid_access_predicate`. Its message and issue name
the collection, the rule (`read`, `update`, `delete` and so on), the operation, and the offending
path and operator. REST answers `500` and reports the detail only to `HandlerOptions.RequestError`.

Use the [`query` Go reference](/reference/query/) for constructors and types, or continue with the
[TypeScript SDK](/docs/typescript-sdk/) and [REST API](/docs/rest-api/) representations.
For the performance effect of indexes, selection, population, and pool sizing, see
[Database and query performance](/docs/performance/database-and-queries/).

## Filter lists and has-many fields by membership {#membership}

Some fields hold a set of items rather than one value:

| Field                                                                           | Items                           |
| ------------------------------------------------------------------------------- | ------------------------------- |
| [TextList and NumberList](/docs/fields/lists/)                                  | strings or numbers              |
| [MultiSelect](/docs/fields/select/)                                             | option values                   |
| [Relationships](/docs/fields/relationship/) and [Uploads](/docs/fields/upload/) | document IDs                    |
| [PolymorphicRelationship(s)](/docs/fields/relationship/)                        | `{ relationTo, id }` references |

For these fields, `in` matches a document when any item equals any candidate. This finds products
with the tag `"sale"` or `"featured"`:

```go title="content/find_tagged.go" focus={15-20}
package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func FindTaggedProducts(
	ctx context.Context,
	local *ridu.LocalAPI,
) (store.Page, error) {
	// Match either tag anywhere in the list, using exact values.
	return local.List(ctx, "products", ridu.ListOptions{
		Where: query.In("tags", "sale", "featured"),
		Page:  1,
		Limit: 20,
	})
}
```

The SDK filter is `{ tags: { in: ['sale', 'featured'] } }`. For numbers, use numeric candidates:
`{ availableSizes: { in: [10, 12] } }`. Has-many relationships and uploads take document IDs:
`{ authors: { in: [adaID] } }` finds posts that list Ada among their authors. These match either
candidate; to require both, combine separate `in` filters with `and` (or `query.And` in Go).

A polymorphic relationship's candidate has the relationship's own `{ relationTo, id }` shape,
because the same ID can exist in two collections. In Go, build it with `query.Reference`:

```go
query.In("subjects",
	query.Reference("posts", postID),
	query.Reference("media", mediaID),
)
```

REST and the SDK take `{ subjects: { in: [{ relationTo: 'posts', id: postID }] } }`. GraphQL uses
its own relationship shape, `{ relationTo: POSTS, value: "..." }`. A singular polymorphic
relationship holds at most one reference and is filtered the same way. `relationTo` must name one
of the field's target collections.

Matching is exact, so `"sale"` does not match `"wholesale"`. Order and repeated values do not
change the result. Wrap `in` in `not` (`query.Not` in Go) to exclude documents holding any
candidate; GraphQL also offers `not_in`. `exists` and null equality test whether the field holds
a value; an empty list counts as present.

Scalar equality, whole-list equality, substring search, range comparisons, sorting, and
indexes are unavailable. They fail with `bad_query` and an `unsupported_operator` issue rather
than matching nothing, on every transport and database. Generated `MembershipWhere<T>` exposes
only the supported operators. Fields inside groups, arrays and blocks use the same operators
through their [nested paths](#paths).
