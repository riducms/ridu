---
title: 'Querying data'
description: 'Find, filter, sort, paginate, and populate content with one query language from Go, TypeScript, and REST.'
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
    'search',
    'find',
    'list',
    'count'
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

Every read in Ridu uses one query language. The Local Go API, the TypeScript SDK, REST and your
access rules all build the same filters, so each example below shows all three. Pick a tab once and
the rest of the docs follow it.

## Example schema {#schema}

The examples query this `posts` collection. Each post's `author` is a document in `users`, which
has a `name` field.

```go title="content/posts.go"
var Posts = ridu.Collection{
	Slug: "posts",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Select("status", "draft", "published").
			Default("draft"),
		field.Relationship("author", "users"),
		field.TextList("tags"),
		field.Group("seo", field.Fields{
			field.Text("title"),
			field.Textarea("description"),
		}),
		field.Date("publishedAt"),
		field.Number("views"),
	},
}
```

Each tab assumes a little setup:

| Tab        | Uses                                                                                                                                                                    |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Go         | `local`, the [Local Go API](/docs/local-api/) from `app.Local()` or a hook's `ctx.Local`, and `user`, the signed-in caller's document ([why](/docs/local-api/#actors)). |
| TypeScript | `ridu`, a [generated client](/docs/typescript-sdk/#install-and-create).                                                                                                 |
| REST       | `$RIDU`, the server's URL, and `$TOKEN`, a [session token](/docs/rest-api/authentication/).                                                                             |

## Find many {#find-many}

List a page of documents that match a filter:

```go title="Go" group="find-many" tab="Go"
page, err := local.List(ctx, "posts", ridu.ListOptions{
	Where: query.Equal("status", "published"),
	Actor: user,
})
```

```ts title="TypeScript" group="find-many" tab="TypeScript"
const page = await ridu.list('posts', {
	where: { status: { equals: 'published' } }
});
```

```sh title="REST" group="find-many" tab="REST"
curl -G "$RIDU/api/collections/posts" \
  -H "Authorization: Session $TOKEN" \
  --data-urlencode 'where={"status":{"equals":"published"}}'
```

```json title="Result, trimmed"
{
	"docs": [
		{
			"id": "posts_49d8d5f5eebb60b11c3fd9f9",
			"title": "Hello, Ridu",
			"status": "published",
			"author": "users_c15d8cdad9e97e3c8ba53543",
			"tags": ["launch", "go"],
			"publishedAt": "2026-09-01",
			"views": 1200
		}
	],
	"pagination": {
		"page": 1,
		"limit": 10,
		"totalDocs": 3,
		"totalPages": 1,
		"hasNextPage": false,
		"hasPrevPage": false
	}
}
```

In Go, `page.Documents` holds the documents, and `page.Total` and `page.HasNextPage` hold the
pagination. Change the page, its size and its order in
[Sorting and pagination](/docs/querying/sorting-and-pagination/).

## Find one {#find-one}

Read one document by ID:

```go title="Go" group="find-one" tab="Go"
post, err := local.Find(ctx, "posts", postID, ridu.FindOptions{
	Actor: user,
})
```

```ts title="TypeScript" group="find-one" tab="TypeScript"
const post = await ridu.find('posts', postID);
```

```sh title="REST" group="find-one" tab="REST"
curl "$RIDU/api/collections/posts/$POST_ID" \
  -H "Authorization: Session $TOKEN"
```

A missing document, or one the caller can't read, fails with `not_found`: a `*ridu.OperationError`
in Go, a `RiduError` in TypeScript, and a `404` over REST.

## Count {#count}

Count the documents that match a filter, without reading them:

```go title="Go" group="count" tab="Go"
// Go has no separate count: read one document and its total.
page, err := local.List(ctx, "posts", ridu.ListOptions{
	Where: query.In("tags", "guide"),
	Limit: 1,
	Actor: user,
})
total := *page.Total
```

```ts title="TypeScript" group="count" tab="TypeScript"
const { totalDocs } = await ridu.count('posts', {
	where: { tags: { in: ['guide'] } }
});
```

```sh title="REST" group="count" tab="REST"
curl -G "$RIDU/api/collections/posts/count" \
  -H "Authorization: Session $TOKEN" \
  --data-urlencode 'where={"tags":{"in":["guide"]}}'
```

```json title="Result"
{ "totalDocs": 2 }
```

## Read a global {#globals}

A global is a single document, such as site settings, so you read it by its slug:

```go title="Go" group="global" tab="Go"
settings, err := local.Global(ctx, "site-settings", ridu.FindOptions{
	Actor: user,
})
```

```ts title="TypeScript" group="global" tab="TypeScript"
const settings = await ridu.global('site-settings');
```

```sh title="REST" group="global" tab="REST"
curl "$RIDU/api/globals/site-settings" \
  -H "Authorization: Session $TOKEN"
```

## Shape the results {#options}

Every read takes the same options. Each has a guide:

| To…                                  | Go (`ListOptions`)          | TypeScript and REST        | Guide                                                                       |
| ------------------------------------ | --------------------------- | -------------------------- | --------------------------------------------------------------------------- |
| keep only matching documents         | `Where`                     | `where`                    | [Filters](/docs/querying/filters/)                                          |
| order them                           | `Sort`                      | `sort`                     | [Sorting and pagination](/docs/querying/sorting-and-pagination/)            |
| pick a page                          | `Page`, `Limit`             | `page`, `limit`            | [Sorting and pagination](/docs/querying/sorting-and-pagination/)            |
| skip counting every match            | `SkipTotal`                 | `pagination: false`        | [Sorting and pagination](/docs/querying/sorting-and-pagination/#skip-total) |
| return only some fields              | `Select`                    | `select`                   | [Selecting and populating](/docs/querying/select-and-populate/)             |
| include related documents            | `Populate`                  | `populate`, `depth`        | [Selecting and populating](/docs/querying/select-and-populate/#populate)    |
| read another content locale          | `Locale`, `FallbackLocales` | `locale`, `fallbackLocale` | [Localization](/docs/localization/)                                         |
| read drafts or the published version | `Draft`                     | `draft`                    | [Drafts and versions](/docs/drafts-and-versions/)                           |
| read deleted documents               | `TrashOnly`                 | `trash`                    | [Bulk actions and trash](/docs/bulk-and-trash/)                             |

`Find` takes the options that apply to one document: selection, population, locale, draft and
trash. A count takes the filter, locale, draft and trash options.

## Locales, drafts and trash {#modes}

Reads use the request's content locale and its fallbacks for filters, sorting and population as
well as output. Pass `locale: 'all'` to get every locale's values at once; see
[Localization](/docs/localization/).

On a collection with drafts, a read returns the draft when the caller may read drafts, and the
published version otherwise. Set `draft` to choose explicitly; see
[Drafts and versions](/docs/drafts-and-versions/).

Deleted documents in a trash-enabled collection stay out of ordinary reads. Set `trash: true`
(`TrashOnly` in Go) to read only deleted documents; one query can't mix the two.

## How access rules apply {#access}

A collection's read rule can return a filter instead of a yes or no. Ridu adds it to your `where` in
the same database query, so a page never contains documents the caller can't read, and the totals
count only documents they can. A field with a read rule can't be filtered or sorted, even by callers
who can read it. See [Access control](/docs/access-control/).

## Current limits {#limits}

There is no `notIn` operator (wrap `in` in `not` instead), full-text ranking, operator that
requires every item of a list, or geospatial query. A point field can be stored and returned, but
not queried by distance. Distinct values are available only from Go, for one direct field; see
[Distinct values](/docs/querying/sorting-and-pagination/#distinct).

For query performance, indexes and population cost, see
[Database and query performance](/docs/performance/database-and-queries/).
