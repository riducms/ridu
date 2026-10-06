<!-- Generated from website/src/content/docs/writing-data.md by scripts/sync-agent-docs.ts. -->

# Writing data

Writes go through the same engine as reads, whichever API you use: access rules, validation and
hooks run every time, and the response is the saved document. Examples use the
[example schema](./querying.md#schema) and the same tab setup as
[Querying data](./querying.md#schema).

## Create {#create}

**A new post:**

```go title="Go" group="create" tab="Go"
post, err := local.Create(ctx, "posts", store.Values{
	"title": store.String("Draft idea"),
	"tags":  store.List(store.String("idea")),
}, ridu.MutationOptions{Actor: user})
```

```ts title="TypeScript" group="create" tab="TypeScript"
const post = await ridu.create('posts', {
	title: 'Draft idea',
	tags: ['idea']
});
```

```sh title="REST" group="create" tab="REST"
curl -X POST "$RIDU/api/collections/posts" \
  -H "Authorization: Session $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Draft idea","tags":["idea"]}'
```

```json title="Result"
{
	"id": "posts_4c1a7d85841d75a1d744f90f",
	"title": "Draft idea",
	"status": "draft",
	"tags": ["idea"],
	"createdAt": "2026-10-06T13:48:20.726903Z",
	"updatedAt": "2026-10-06T13:48:20.726903Z"
}
```

Fields you leave out get their defaults, so `status` is `draft`. Over REST the document arrives as
`{ "doc": { … } }`; Go and TypeScript return the document itself. In Go, build values with
`store.String`, `store.Number`, `store.List` and the other
[value constructors](./go-packages/store.md), or use [typed handles](./local-api/typed-handles.md).

## Update {#update}

Send only the fields you want to change. The rest keep their stored values.

**Retitle a post:**

```go title="Go" group="update" tab="Go"
post, err := local.Update(ctx, "posts", postID, store.Values{
	"title": store.String("A better idea"),
}, ridu.MutationOptions{Actor: user})
```

```ts title="TypeScript" group="update" tab="TypeScript"
const post = await ridu.update('posts', postID, {
	title: 'A better idea'
});
```

```sh title="REST" group="update" tab="REST"
curl -X PATCH "$RIDU/api/collections/posts/$POST_ID" \
  -H "Authorization: Session $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"A better idea"}'
```

To clear a field, send `null` (`store.Null()` in Go).

## Update a versioned document {#versioned}

A collection with `Versions: true` keeps every saved revision, and a published document only changes
by being published again. Send your changes to the publish operation, with the revision you last
read:

```go title="Go" group="versioned" tab="Go"
// page is the document you read before editing.
updated, err := local.PublishChanges(ctx, "pages", page.ID,
	store.Values{"title": store.String("About us")},
	ridu.MutationOptions{
		ExpectedRevision: page.Revision,
		Actor:            user,
	},
)
```

```ts title="TypeScript" group="versioned" tab="TypeScript"
const updated = await ridu.publishChanges(
	'pages',
	page.id,
	{ title: 'About us' },
	{ revision: page._revision }
);
```

```sh title="REST" group="versioned" tab="REST"
curl -X POST "$RIDU/api/collections/pages/$PAGE_ID/publish" \
  -H "Authorization: Session $TOKEN" \
  -H "Content-Type: application/json" \
  -H 'If-Match: "1"' \
  -d '{"title":"About us"}'
```

A plain update of a published document is refused with `publish_required` (`409`), and the message
names the publish route. With drafts enabled, you can also save changes as a draft first; see
[Drafts and versions](./drafts-and-versions.md).

### Don't overwrite someone else's edit {#revisions}

The revision makes the write conditional. If someone saved the document after you read it, its
revision has moved on and your write fails with `conflict` (`409`) instead of replacing their
changes:

```json title="REST response"
{
	"error": {
		"code": "conflict",
		"status": 409,
		"message": "document conflicts with an existing value"
	}
}
```

Reload the document, show the newer version, and let the author decide. Pass the revision on every
interactive edit; [document locks](./document-locks.md) coordinate editors but don't replace it.
Leaving it out, or passing zero in Go, writes without the check.

## Delete {#delete}

```go title="Go" group="delete" tab="Go"
_, err := local.Delete(ctx, "posts", postID, ridu.MutationOptions{
	Actor: user,
})
```

```ts title="TypeScript" group="delete" tab="TypeScript"
await ridu.delete('posts', postID);
```

```sh title="REST" group="delete" tab="REST"
curl -X DELETE "$RIDU/api/collections/posts/$POST_ID" \
  -H "Authorization: Session $TOKEN"
```

```json title="REST response"
{ "id": "posts_0354693e1c584b9c537d890e", "deleted": true }
```

In a collection with trash, delete moves the document to the trash instead; restore and permanent
delete are separate operations. See [Bulk actions and trash](./bulk-and-trash.md).

## Duplicate {#duplicate}

Copy a document, optionally changing some fields:

```go title="Go" group="duplicate" tab="Go"
copied, err := local.Duplicate(ctx, "posts", postID, store.Values{
	"title": store.String("Copy of a better idea"),
}, ridu.MutationOptions{Actor: user})
```

```ts title="TypeScript" group="duplicate" tab="TypeScript"
const copied = await ridu.duplicate('posts', postID, {
	title: 'Copy of a better idea'
});
```

```sh title="REST" group="duplicate" tab="REST"
curl -X POST "$RIDU/api/collections/posts/$POST_ID/duplicate" \
  -H "Authorization: Session $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Copy of a better idea"}'
```

The copy is a new document with its own ID. Access rules and hooks run as for a create.

## Update a global {#globals}

A global has no create or delete: updating it the first time saves it.

```go title="Go" group="update-global" tab="Go"
settings, err := local.UpdateGlobal(ctx, "site-settings",
	store.Values{"siteName": store.String("Acme")},
	ridu.MutationOptions{Actor: user},
)
```

```ts title="TypeScript" group="update-global" tab="TypeScript"
const settings = await ridu.updateGlobal('site-settings', {
	siteName: 'Acme'
});
```

```sh title="REST" group="update-global" tab="REST"
curl -X PATCH "$RIDU/api/globals/site-settings" \
  -H "Authorization: Session $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"siteName":"Acme"}'
```

Before its first save, a global reads as its defaults. A versioned global changes through
`PublishGlobalChanges` (`publishGlobalChanges` in TypeScript), like a
[versioned document](#versioned).

## Change several documents at once {#bulk}

**Set two posts back to draft:**

```go title="Go" group="bulk" tab="Go"
ids := []string{firstID, secondID}
posts, err := local.BulkUpdate(ctx, "posts", ids,
	store.Values{"status": store.String("draft")},
	ridu.BulkOptions{Actor: user},
)
```

```ts title="TypeScript" group="bulk" tab="TypeScript"
const posts = await ridu.bulkUpdate('posts', [firstID, secondID], {
	status: 'draft'
});
```

```sh title="REST" group="bulk" tab="REST"
curl -X POST "$RIDU/api/collections/posts/bulk" \
  -H "Authorization: Session $TOKEN" \
  -H "Content-Type: application/json" \
  -d @- <<JSON
{"action":"update","ids":["$FIRST_ID","$SECOND_ID"],
 "data":{"status":"draft"}}
JSON
```

A bulk action takes 1 to 100 IDs and saves all of them or none. Each document still passes its own
access rules, validation and hooks. The same form covers publishing, unpublishing, deleting,
restoring and permanently deleting several documents; see [Bulk actions and trash](./bulk-and-trash.md).

## When a write fails {#errors}

A write that breaks a rule saves nothing and fails with a code you can branch on:

| Code               | Status | Means                                                                                       |
| ------------------ | ------ | ------------------------------------------------------------------------------------------- |
| `validation`       | `422`  | A field is invalid; `issues` lists each one with its `path`                                 |
| `conflict`         | `409`  | A stale revision, or a unique value already in use                                          |
| `publish_required` | `409`  | A plain update of a published versioned document; [publish the changes](#versioned) instead |
| `access_denied`    | `403`  | The caller may not make this change                                                         |
| `not_found`        | `404`  | The document doesn't exist, or the caller can't see it                                      |
| `rejected`         | `422`  | A hook refused the change; its message is written for people                                |

Handle them in Go with [`*ridu.OperationError`](./local-api/errors.md), in TypeScript with
[`RiduError`](./typescript-sdk.md#errors), and over REST from the
[error envelope](https://riducms.com/docs/rest-api/queries/#errors).
