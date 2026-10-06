<!-- Generated from website/src/content/docs/querying/filters.md by scripts/sync-agent-docs.ts. -->

# Filters

A filter keeps only the documents that match it. Pass it as `Where` in Go and `where` in TypeScript
and REST. Examples use the [example schema](../querying.md#schema).

**Published posts:**

```go title="Go" group="where" tab="Go"
page, err := local.List(ctx, "posts", ridu.ListOptions{
	Where: query.Equal("status", "published"),
	Actor: user,
})
```

```ts title="TypeScript" group="where" tab="TypeScript"
const page = await ridu.list('posts', {
	where: { status: { equals: 'published' } }
});
```

```sh title="REST" group="where" tab="REST"
curl -G "$RIDU/api/collections/posts" \
  -H "Authorization: Session $TOKEN" \
  --data-urlencode 'where={"status":{"equals":"published"}}'
```

The rest of this page shows only the filter. In TypeScript, filters are typed `PostsWhere`, which
`ridu generate` writes from the collection, so an unknown field or operator fails to compile. In
REST, the filter is the `where` parameter.

## Match a value {#equals}

**Posts that are published, and posts that aren't:**

```go title="Go" group="equals" tab="Go"
published := query.Equal("status", "published")
notPublished := query.NotEqual("status", "published")
```

```ts title="TypeScript" group="equals" tab="TypeScript"
const published: PostsWhere = { status: { equals: 'published' } };
const notPublished: PostsWhere = {
	status: { notEquals: 'published' }
};
```

```text title="REST" group="equals" tab="REST"
where={"status":{"equals":"published"}}
where={"status":{"notEquals":"published"}}
```

`equals: null` matches a field with no value.

## Match one of several values {#in}

**Posts that are drafts or published:**

```go title="Go" group="in" tab="Go"
draftOrPublished := query.In("status", "draft", "published")
```

```ts title="TypeScript" group="in" tab="TypeScript"
const draftOrPublished: PostsWhere = {
	status: { in: ['draft', 'published'] }
};
```

```text title="REST" group="in" tab="REST"
where={"status":{"in":["draft","published"]}}
```

To exclude several values, wrap `in` in [`not`](#combine).

## Compare numbers and dates {#ranges}

**Posts with at least 600 views:**

```go title="Go" group="ranges" tab="Go"
popular := query.GreaterThanEqual("views", 600)
```

```ts title="TypeScript" group="ranges" tab="TypeScript"
const popular: PostsWhere = { views: { greaterThanEqual: 600 } };
```

```text title="REST" group="ranges" tab="REST"
where={"views":{"greaterThanEqual":600}}
```

**Posts published since 15 September 2026:**

```go title="Go" group="dates" tab="Go"
recent := query.GreaterThanEqual("publishedAt", "2026-09-15")
```

```ts title="TypeScript" group="dates" tab="TypeScript"
const recent: PostsWhere = {
	publishedAt: { greaterThanEqual: '2026-09-15' }
};
```

```text title="REST" group="dates" tab="REST"
where={"publishedAt":{"greaterThanEqual":"2026-09-15"}}
```

Use `greaterThan`, `greaterThanEqual`, `lessThan` and `lessThanEqual` on numbers, dates and text.
Write a date in the field's own format.

## Search text {#text}

`contains` matches part of a value, ignoring case. `like` matches values that contain every word
you give, in any order.

**Titles containing "ridu", and titles with both "sdk" and "typescript":**

```go title="Go" group="text" tab="Go"
aboutRidu := query.Contains("title", "ridu")
sdkGuides := query.Like("title", "sdk typescript")
```

```ts title="TypeScript" group="text" tab="TypeScript"
const aboutRidu: PostsWhere = { title: { contains: 'ridu' } };
const sdkGuides: PostsWhere = { title: { like: 'sdk typescript' } };
```

```text title="REST" group="text" tab="REST"
where={"title":{"contains":"ridu"}}
where={"title":{"like":"sdk typescript"}}
```

The first matches "Hello, Ridu"; the second matches "TypeScript SDK tour". Neither ranks results
by relevance.

## Check that a field is set {#exists}

**Posts that haven't been given a publish date:**

```go title="Go" group="exists" tab="Go"
undated := query.Exists("publishedAt", false)
```

```ts title="TypeScript" group="exists" tab="TypeScript"
const undated: PostsWhere = { publishedAt: { exists: false } };
```

```text title="REST" group="exists" tab="REST"
where={"publishedAt":{"exists":false}}
```

`exists: true` matches documents where the field has a value. An empty list counts as a value.

## Combine conditions {#combine}

**Published posts that are tagged "typescript" or have more than 1,000 views:**

```go title="Go" group="combine" tab="Go"
featured := query.And(
	query.Equal("status", "published"),
	query.Or(
		query.In("tags", "typescript"),
		query.GreaterThan("views", 1000),
	),
)
```

```ts title="TypeScript" group="combine" tab="TypeScript"
const featured: PostsWhere = {
	and: [
		{ status: { equals: 'published' } },
		{
			or: [
				{ tags: { in: ['typescript'] } },
				{ views: { greaterThan: 1000 } }
			]
		}
	]
};
```

```json title="REST" group="combine" tab="REST"
{
	"and": [
		{ "status": { "equals": "published" } },
		{
			"or": [
				{ "tags": { "in": ["typescript"] } },
				{ "views": { "greaterThan": 1000 } }
			]
		}
	]
}
```

**Posts not tagged "go":**

```go title="Go" group="not" tab="Go"
notAboutGo := query.Not(query.In("tags", "go"))
```

```ts title="TypeScript" group="not" tab="TypeScript"
const notAboutGo: PostsWhere = { not: { tags: { in: ['go'] } } };
```

```text title="REST" group="not" tab="REST"
where={"not":{"tags":{"in":["go"]}}}
```

`and` and `or` take two or more conditions; `not` takes one.

## Lists and has-many fields {#membership}

Some fields hold several items: text and number lists, multiple selects, and relationships or
uploads that allow many documents. Filter them with `in`, which matches a document when **any** of
its items equals **any** value you give.

**Posts tagged "guide" or "launch":**

```go title="Go" group="membership" tab="Go"
guidesOrLaunches := query.In("tags", "guide", "launch")
```

```ts title="TypeScript" group="membership" tab="TypeScript"
const guidesOrLaunches: PostsWhere = {
	tags: { in: ['guide', 'launch'] }
};
```

```text title="REST" group="membership" tab="REST"
where={"tags":{"in":["guide","launch"]}}
```

Matching is exact, so "guide" doesn't match "guidebook". To require two tags, join two `in`
filters with `and`; to exclude a tag, wrap `in` in `not`. These fields also accept `exists` and
`equals: null`. Any other operator is refused, rather than matching nothing.

A has-many relationship takes document IDs: `{ authors: { in: [adaID] } }` finds posts that list Ada
among their authors. A polymorphic relationship can point into several collections, so each value
names one: `{ subjects: { in: [{ relationTo: 'posts', id: postID }] } }`, or
`query.Reference("posts", postID)` in Go.

## Fields in groups, arrays and blocks {#paths}

Reach a nested field with a dotted path of field names.

**Posts whose SEO title is "Hello Ridu":**

```go title="Go" group="paths" tab="Go"
seoTitled := query.Equal("seo.title", "Hello Ridu")
```

```ts title="TypeScript" group="paths" tab="TypeScript"
const seoTitled: PostsWhere = {
	'seo.title': { equals: 'Hello Ridu' }
};
```

```text title="REST" group="paths" tab="REST"
where={"seo.title":{"equals":"Hello Ridu"}}
```

A path through an array uses the array's name. A path through a blocks field adds the block's slug,
so a `note` block's `text` inside a `card` block inside a `layout` field is
`layout.card.children.note.text`. Row positions never appear in a path.

A path through arrays or blocks reaches the field in every row, so a document can match on any row:

| Comparison                                                 | Matches when                                   |
| ---------------------------------------------------------- | ---------------------------------------------- |
| `equals`, `in`, `contains`, `like`, ranges, `exists: true` | at least one row's value matches               |
| `notEquals`, `exists: false`, `not`                        | no row's value matches the opposite comparison |
| `equals: null`                                             | a row's value is null, or no row has the field |

Each condition in an `and` is matched on its own, so two conditions can be met by two different
rows. Paths inside a rich-text or other plugin field aren't filterable; store values you need to
search in ordinary fields.

<details class="docs-disclosure">
<summary>JSON fields</summary>
<div class="docs-disclosure-body">

A [JSON field](https://riducms.com/docs/fields/json/) can hold any value, so a comparison applies only when the stored
value is a scalar of the same type. `contains` and `like` match a stored string, ignoring case:
`{ metadata: { contains: 'plan' } }` matches `"Launch PLAN"`. An array or object never contains
text and never equals a scalar; it only counts as present for `exists`. Use a
[text list](../fields/lists.md) when items should match one by one.

A path into a JSON value, such as `metadata.owner`, follows object keys and stops at an array.
PostgreSQL, SQLite and the in-memory store support these paths; MongoDB refuses them.

</div>
</details>

## Operators {#operators}

| TypeScript and REST               | Go                                            | Matches                                                   |
| --------------------------------- | --------------------------------------------- | --------------------------------------------------------- |
| `equals`, `notEquals`             | `query.Equal`, `query.NotEqual`               | an exact value, or any other value                        |
| `in`                              | `query.In`                                    | one of several values; for lists, any item equals one     |
| `greaterThan`, `greaterThanEqual` | `query.GreaterThan`, `query.GreaterThanEqual` | a larger number, date or text                             |
| `lessThan`, `lessThanEqual`       | `query.LessThan`, `query.LessThanEqual`       | a smaller number, date or text                            |
| `contains`                        | `query.Contains`                              | text that contains the value, ignoring case               |
| `like`                            | `query.Like`                                  | text that contains every word of the value, ignoring case |
| `exists`                          | `query.Exists`                                | a field that has a value, or doesn't                      |
| `and`, `or`, `not`                | `query.And`, `query.Or`, `query.Not`          | combinations of other filters                             |

New to building filters in Go? [Filters and paths](../go-packages/query.md) explains
`query.Expression` and how the same filters work in access rules.

## When a filter is refused {#errors}

Ridu checks a filter before it reads anything. An unknown path, an operator the field doesn't
support, or a field with a read rule fails with `bad_query` (`400`) in Go, over REST and in the SDK.
The issue code says why: `unsupported_operator` or `unsupported_path`.

```json title="REST response"
{
	"error": {
		"code": "bad_query",
		"status": 400,
		"issues": [
			{
				"code": "unsupported_operator",
				"path": "tags",
				"message": "text list \"tags\" does not support \"equal\" …"
			}
		]
	}
}
```

Over HTTP, a `where` holds at most 100 comparisons, nested at most 16 deep, with at most 100 values
in one `in`. A path has at most 64 segments and 4,096 bytes.
