<!-- Generated from website/src/content/docs/querying/select-and-populate.md by scripts/sync-agent-docs.ts. -->

# Selecting and populating

A read returns every field the caller may read, and relationships as document IDs. Select fewer
fields, or populate relationships to get the related documents in the same request. Examples use
the [example schema](../querying.md#schema).

## Select fields {#select}

**Only each post's title and author:**

```go title="Go" group="select" tab="Go"
page, err := local.List(ctx, "posts", ridu.ListOptions{
	Select: []query.Path{
		query.Field("title"),
		query.Field("author"),
	},
	Actor: user,
})
```

```ts title="TypeScript" group="select" tab="TypeScript"
const page = await ridu.list('posts', {
	select: { title: true, author: true }
});
```

```sh title="REST" group="select" tab="REST"
curl -G "$RIDU/api/collections/posts" \
  -H "Authorization: Session $TOKEN" \
  --data-urlencode 'select={"title":true,"author":true}'
```

```json title="Result: one document"
{
	"id": "posts_49d8d5f5eebb60b11c3fd9f9",
	"title": "Hello, Ridu",
	"author": "users_c15d8cdad9e97e3c8ba53543",
	"createdAt": "2026-10-06T13:38:37.505562Z",
	"updatedAt": "2026-10-06T13:38:37.505562Z"
}
```

`id`, `createdAt` and `updatedAt` are always included. A selected relationship stays an ID until you
populate it. Select top-level fields; REST accepts at most 256.

In TypeScript, a literal `select` narrows the result's type to the selected fields. If you build
the options separately, keep them literal with `as const` or `satisfies`, or the type stays broad.

## Populate a relationship {#populate}

**Each post with its author's name:**

```go title="Go" group="populate" tab="Go"
page, err := local.List(ctx, "posts", ridu.ListOptions{
	Populate: []query.Population{{
		Path:   query.Field("author"),
		Select: []query.Path{query.Field("name")},
	}},
	Actor: user,
})
```

```ts title="TypeScript" group="populate" tab="TypeScript"
const page = await ridu.list('posts', {
	populate: { author: { select: { name: true } } }
});
```

```sh title="REST" group="populate" tab="REST"
curl -G "$RIDU/api/collections/posts" \
  -H "Authorization: Session $TOKEN" \
  --data-urlencode 'populate={"author":{"select":{"name":true}}}'
```

```json title="Result: one document, trimmed"
{
	"id": "posts_49d8d5f5eebb60b11c3fd9f9",
	"title": "Hello, Ridu",
	"author": {
		"id": "users_c15d8cdad9e97e3c8ba53543",
		"name": "Ada Lovelace",
		"createdAt": "2026-10-06T13:38:22.661404Z",
		"updatedAt": "2026-10-06T13:38:22.661404Z"
	}
}
```

Leave out `select` to get the whole related document. Ridu reads each related document with the
caller's own access, so a document they can't read stays out, and fields they can't read are
removed. In Go, a populated relationship's value is a `store.Populated` document.

In TypeScript, a literal `populate` changes the relationship's type from an ID to the related
document, including the fields you selected.

### Go deeper {#nested-populate}

Populate a relationship of the related document with `depth`, up to 5 levels:

```ts
const page = await ridu.list('posts', {
	populate: { author: { depth: 2 } }
});
```

In Go, set `Depth` on the `query.Population`. A relationship inside a group, array or block uses its
[dotted path](./filters.md#paths), such as `'sections.quote.source': true`.

## Populate every relationship {#depth}

`depth` on its own populates every top-level relationship to that many levels:

```ts title="TypeScript" group="depth" tab="TypeScript"
const page = await ridu.list('posts', { depth: 1 });
```

```sh title="REST" group="depth" tab="REST"
curl -G "$RIDU/api/collections/posts" \
  -H "Authorization: Session $TOKEN" \
  --data-urlencode 'depth=1'
```

Use it for quick reads. For production queries, name the relationships with `populate`: the result's
shape and cost stay predictable, and TypeScript knows the populated types. You can't combine
`depth` with `populate`. In Go, list each relationship as a `query.Population`.

## Limits {#limits}

One request can name at most 64 relationship paths and populate to depth 5. Across the whole
request, Ridu expands at most 256 schema paths and reads at most 4,096 related documents. These are
limits for the request, not for each document.
