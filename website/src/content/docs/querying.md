---
title: 'Querying data'
description: 'Filter, sort, paginate, select, populate, and localize collection reads through one finite query language.'
product: data
eyebrow: 'Data and APIs'
order: 95
aliases: ['query', 'where', 'filter', 'sort', 'select', 'populate', 'depth', 'pagination', 'search']
availability:
  status: limited
  label: 'Finite query vocabulary'
  description: 'Typed filtering, stable sorting, selection, and bounded population work today; full-text, distinct, notIn, array, and geospatial operators are not implemented.'
  anchor: limits
navigation:
  section: 'Work with data'
  parent: 'data-access'
  order: 40
  title: 'Querying data'
---

Ridu uses one query vocabulary across the local Go API, REST, SDK, access rules, and store adapters.

## Filter with `where` {#where}

Generated TypeScript clients expose only paths from your application schema:

```ts
const page = await client.list('posts', {
	where: {
		and: [
			{ status: { equals: 'published' } },
			{
				or: [{ title: { contains: 'ridu' } }, { 'author.name': { like: 'Ada Lovelace' } }]
			}
		]
	}
});
```

| TypeScript operator               | Go operator                                                     | Meaning                                                           |
| --------------------------------- | --------------------------------------------------------------- | ----------------------------------------------------------------- |
| `equals`                          | `query.Equal`                                                   | exact scalar equality                                             |
| `notEquals`                       | `query.NotEqual`                                                | scalar inequality                                                 |
| `in`                              | `query.In`                                                      | value equals one member of a non-empty list                       |
| `exists`                          | `query.Compare(path, query.OperatorExists, query.Boolean(...))` | field is present or absent                                        |
| `greaterThan`, `greaterThanEqual` | `query.GreaterThan`, `query.GreaterThanEqual`                   | ordered string/number comparison                                  |
| `lessThan`, `lessThanEqual`       | `query.LessThan`, `query.LessThanEqual`                         | ordered string/number comparison                                  |
| `contains`                        | `query.Contains`                                                | case-insensitive substring match                                  |
| `like`                            | `query.Like`                                                    | case-insensitive match for every whitespace-delimited search word |
| `and`, `or`, `not`                | `query.And`, `query.Or`, `query.Not`                            | recursive logical composition                                     |

`and` and `or` require at least two children; `not` requires exactly one. HTTP `where` accepts at
most 100 comparison expressions, nesting depth 16, and 100 values in one `in` expression. A query
path has at most 64 segments and 4,096 bytes. Invalid shapes fail with `bad_query` before storage.

### Current query limits {#limits}

There is currently no `notIn`, full-text ranking, array all-elements operator, or general
geospatial comparison. The Local Go API supports distinct values for one direct singular stored
field; relationship-path population, custom aggregate ordering, and a public REST distinct route
remain outside that focused contract. A point field can be stored, returned, and rendered without
implying a radius or bounding-box query API.

## Nested and repeated paths {#paths}

Paths use authored field names separated by dots, such as `seo.title`. Blocks include their stable
block key in the schema path. Runtime array indexes are not part of a path: one nested path applies
to matching rows. The resolver validates a path against the schema and rejects layout-only,
non-sortable, or incompatible fields for the requested operation.

Filters do not populate relationships first. They operate on persisted fields according to the
schema/store contract. Population is a separate output step and re-authorizes every target.

## Sort stably {#sort}

Pass repeated sort terms in priority order. Prefix a path with `-` for descending order:

```ts
const page = await client.list('posts', {
	sort: ['-publishedAt', 'title']
});
```

REST repeats the query key: `?sort=-publishedAt&sort=title`. The HTTP API accepts at most 16 unique
sort fields. Nested repeated structures, groups, arrays, blocks, JSON, and opaque plugin fields are
not sortable. The store appends `id` as the final tie-breaker when needed, which keeps page ordering
deterministic when authored values are equal.

Generated SDK sort terms are validated strings today, not path unions; the server remains the
authority for whether a field can be sorted.

## Paginate and count {#pagination}

REST list requests default to page 1 and limit 10. Both values must be positive; the HTTP maximum
limit is 100. SDK page results keep metadata under `pagination`:

```ts
const { docs, pagination } = await client.list('posts', { page: 2, limit: 25 });

console.log(pagination.totalDocs);
console.log(pagination.totalPages);
console.log(pagination.hasNextPage);
```

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

Selection changes the runtime response, but the current TypeScript method still returns the
collection's complete output type. Projection-dependent result inference is planned, so code must
not assume an unselected field exists at runtime merely because its static property is present.

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

As with `select`, current SDK return types are not narrowed or expanded according to the population
object yet.

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

Use the [`query` Go reference](/reference/query/) for constructors and types, or continue with the
[TypeScript SDK](/docs/typescript-sdk/) and [REST API](/docs/rest-api/) representations.
