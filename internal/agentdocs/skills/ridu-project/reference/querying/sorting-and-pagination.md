<!-- Generated from website/src/content/docs/querying/sorting-and-pagination.md by scripts/sync-agent-docs.ts. -->

# Sorting and pagination

Lists come back one page at a time, in the order you choose. Examples use the
[example schema](../querying.md#schema).

## Sort {#sort}

**Newest posts first, then by title:**

```go title="Go" group="sort" tab="Go"
page, err := local.List(ctx, "posts", ridu.ListOptions{
	Sort: []query.Sort{
		query.Desc("publishedAt"),
		query.Asc("title"),
	},
	Actor: user,
})
```

```ts title="TypeScript" group="sort" tab="TypeScript"
const page = await ridu.list('posts', {
	sort: ['-publishedAt', 'title']
});
```

```sh title="REST" group="sort" tab="REST"
curl -G "$RIDU/api/collections/posts" \
  -H "Authorization: Session $TOKEN" \
  --data-urlencode 'sort=-publishedAt' \
  --data-urlencode 'sort=title'
```

Sort terms apply in order: the second breaks ties in the first. A `-` prefix sorts descending.
Ridu adds `id` as the final tie-breaker, so equal values keep a stable order from page to page.

### What you can sort by {#sortable}

Sort by a field that holds one value, at the top level or inside a group, such as `seo.title`. A
localized field sorts by its value in the request's locale. These can't be sorted, because a
document has no single value to order by:

- arrays and blocks, and fields inside them;
- lists, multiple selects, has-many relationships and uploads, and polymorphic relationships;
- JSON and plugin fields, such as rich text;
- groups themselves, and fields inside a localized group.

Ridu refuses these before reading, with an `unsupported_path` issue. REST accepts at most 16 sort
fields.

## Paginate {#paginate}

**The second page of ten posts:**

```go title="Go" group="paginate" tab="Go"
page, err := local.List(ctx, "posts", ridu.ListOptions{
	Page:  2,
	Limit: 10,
	Actor: user,
})
```

```ts title="TypeScript" group="paginate" tab="TypeScript"
const page = await ridu.list('posts', { page: 2, limit: 10 });
```

```sh title="REST" group="paginate" tab="REST"
curl -G "$RIDU/api/collections/posts" \
  -H "Authorization: Session $TOKEN" \
  --data-urlencode 'page=2' \
  --data-urlencode 'limit=10'
```

```json title="Result: pagination"
{
	"page": 2,
	"limit": 10,
	"totalDocs": 34,
	"totalPages": 4,
	"hasNextPage": true,
	"hasPrevPage": true
}
```

Pages start at 1. Over REST and the SDK, `limit` defaults to 10 and can be at most 100. In Go,
`page.Total` points to the total and `page.HasNextPage` says whether another page follows.

## Skip the total count {#skip-total}

Counting every match costs more as a collection grows. For "Load more" buttons, infinite scroll and
feeds, you only need to know whether another page exists:

```go title="Go" group="skip-total" tab="Go"
page, err := local.List(ctx, "posts", ridu.ListOptions{
	Page:      2,
	Limit:     10,
	SkipTotal: true,
	Actor:     user,
})
// page.Total is nil; page.HasNextPage is exact.
```

```ts title="TypeScript" group="skip-total" tab="TypeScript"
const page = await ridu.list('posts', {
	page: 2,
	limit: 10,
	pagination: false
});

if (page.pagination.hasNextPage) showLoadMore();
```

```sh title="REST" group="skip-total" tab="REST"
curl -G "$RIDU/api/collections/posts" \
  -H "Authorization: Session $TOKEN" \
  --data-urlencode 'page=2' \
  --data-urlencode 'limit=10' \
  --data-urlencode 'pagination=false'
```

```json title="Result: pagination"
{ "page": 2, "limit": 10, "hasNextPage": true, "hasPrevPage": true }
```

Ridu reads one document past the page to set `hasNextPage`, and leaves out `totalDocs` and
`totalPages` instead of estimating them. In TypeScript, a literal `pagination: false` removes the
totals from the result's type too.

To count matches without listing them, see [Count](../querying.md#count).

## Distinct values {#distinct}

List each different value of one field, such as every status in use. This is available from Go
only:

```go title="Go"
statuses, err := local.Distinct(ctx, "posts", ridu.DistinctOptions{
	Field: query.Field("status"),
	Where: query.Exists("publishedAt", true),
	Actor: user,
})
// statuses.Values holds each value once, in ascending order.
```

`Distinct` works on a direct field that holds one value, a single relationship or upload, or `id`,
and pages like a list. A field with a read rule is refused, because the rule may differ from one
document to the next.
