<!-- Generated from website/src/content/docs/local-api.md by scripts/sync-agent-docs.ts. -->

# Local Go API

`app.Local()` gives Go code the same operations as REST and the TypeScript SDK, without an HTTP
round trip. It is not a back door to the database: access rules, validation, hooks, versions and
transactions run exactly as they do for every other API.

## Get the Local API {#get}

Call `app.Local()` in your own handlers, services and tasks. Hooks, access rules and custom endpoints
receive it as `ctx.Local`:

```go title="Go" group="get-local" tab="In app code"
func publishedPosts(
	ctx context.Context,
	app *ridu.App,
	user *store.Document,
) (store.Page, error) {
	return app.Local().List(ctx, "posts", ridu.ListOptions{
		Where: query.Equal("status", "published"),
		Actor: user,
	})
}
```

```go title="Go" group="get-local" tab="In a hook"
func countAuthorPosts(ctx ridu.HookContext) error {
	authorID, _ := ctx.Document.Values["author"].StringValue()
	page, err := ctx.Local.List(ctx.Context, "posts", ridu.ListOptions{
		Where:           query.Equal("author", authorID),
		Limit:           1,
		Actor:           ctx.Actor,
		ActorCollection: ctx.ActorCollection,
	})
	if err != nil {
		return err
	}
	_ = *page.Total // the author's post count
	return nil
}
```

A call made inside a hook joins the transaction of the operation that ran the hook; see
[Transactions and errors](./local-api/errors.md#transactions).

## What it can do {#operations}

| To…                             | Use                                                                             | Guide                                             |
| ------------------------------- | ------------------------------------------------------------------------------- | ------------------------------------------------- |
| read documents and globals      | `Find`, `List`, `Global`, `Distinct`                                            | [Querying data](./querying.md)                  |
| create, change and remove them  | `Create`, `Update`, `Delete`, `Duplicate`, `UpdateGlobal`                       | [Writing data](./writing-data.md)               |
| publish, and work with history  | `PublishChanges`, `Publish`, `Unpublish`, `DiscardDraft`, `Versions`, `Restore` | [Drafts and versions](./drafts-and-versions.md) |
| change many documents at once   | `BulkUpdate`, `BulkPublish`, `BulkUnpublish`, `BulkDelete`                      | [Bulk actions and trash](./bulk-and-trash.md)   |
| restore or empty the trash      | `RestoreDeleted`, `DeletePermanent`, `EmptyTrash`                               | [Bulk actions and trash](./bulk-and-trash.md)   |
| copy values between locales     | `CopyLocale`, `CopyGlobalLocale`                                                | [Localization](./localization.md)               |
| read or change an inverse join  | `ListJoin`, `MutateJoin`                                                        | [Relationships and joins](https://riducms.com/docs/relationships/)   |
| ask what the caller may do      | `Capabilities`                                                                  | [Access control](./access-control.md)           |
| import documents from elsewhere | `Import`, which keeps their IDs, status and timestamps                          | [Move from Payload](../../payload-to-ridu/references/migration-guide.md)        |

Versioned globals have matching `Global…` methods, such as `PublishGlobalChanges`. Files go through
the upload methods on `App`; see [Uploads](./uploads.md#sdk-upload).

## Work with values {#dynamic-values}

The Local API takes and returns `store.Values`, a map from field names to values. Build values with
`store.String`, `store.Number`, `store.Boolean`, `store.List`, `store.Object` and `store.Null`, and
read them back with methods such as `StringValue`:

```go
post, err := local.Create(ctx, "posts", store.Values{
	"title": store.String("Hello, Ridu"),
	"views": store.Number(0),
}, ridu.MutationOptions{Actor: user})
if err != nil {
	return err
}
title, _ := post.Values["title"].StringValue()
```

Returned documents are copies: changing them changes nothing stored. A relationship is its target's
ID unless you [populate](./querying/select-and-populate.md#populate) it.
[Documents and values](./go-packages/store.md) covers nested values and the difference between
leaving a field out and clearing it. For compile-time checked structs instead, use
[typed handles](./local-api/typed-handles.md).

## Pass the caller {#actors}

Every call takes the document of the user it acts for as `Actor`, and access rules and hooks see it.
`nil` means an anonymous visitor, never an administrator.

When your application has more than one auth collection, an ID alone doesn't say which user it is.
Pass the collection as well:

```go
session, err := app.Session(ctx, rawSessionToken)
if err != nil {
	return err
}

post, err := app.Local().Find(ctx, "posts", postID, ridu.FindOptions{
	Actor:           &session.User,
	ActorCollection: session.Collection,
})
```

Copy both from the identity your authentication established, such as a `ridu.AuthIdentity`.
Services built around one user, such as scheduling, previews, preferences and document locks, take
an `AuthIdentity` directly.

## Server-owned work {#system}

Some writes belong to the server rather than the user who triggered them: a hook that updates a
counter, or enrolls a league's owner. Set `System: true` for those calls instead of loosening access
rules for everyone:

```go
func enrolOwner(ctx ridu.HookContext) error {
	_, err := ctx.Local.Create(ctx.Context, "memberships", store.Values{
		"league":  store.String(ctx.Document.ID),
		"learner": store.String(ctx.Actor.ID),
	}, ridu.MutationOptions{
		System:          true,
		Actor:           ctx.Actor,
		ActorCollection: ctx.ActorCollection,
	})
	return err
}
```

A system call skips access rules, including field access, and reads drafts unless you set `Draft`
to `false`. Validation, hooks, versions and the transaction still run, and `Actor` still tells hooks
whom the work is for. Nested calls don't inherit `System`; pass it again only for work that is also
trusted. Only Go can set it: no transport accepts it from a client.

## Options {#options}

Each method takes its required values positionally and a final options struct:

| Options             | Used by                    | Holds                                                                                          |
| ------------------- | -------------------------- | ---------------------------------------------------------------------------------------------- |
| `FindOptions`       | `Find`, `Global`           | `Select`, `Populate`, `Draft`, `TrashOnly`, locale, and the caller                             |
| `ListOptions`       | `List`                     | everything in `FindOptions`, plus `Where`, `Sort`, `Page`, `Limit` and `SkipTotal`             |
| `MutationOptions`   | writes and publishing      | the caller or `System`, `ExpectedRevision`, `Draft`, the write locale, and returned population |
| `BulkOptions`       | bulk actions, `EmptyTrash` | the caller or `System`, and locale                                                             |
| `CapabilityOptions` | `Capabilities`             | candidate `Data`, the caller, trash mode and locale                                            |
| `ImportOptions`     | `Import`                   | the caller or `System`, and source metadata                                                    |

`Draft` is a `*bool`, so leaving it out, `true` and `false` mean different things. On a read, `true`
asks for the working draft and `false` for the published version. On a create, `true` allows an
incomplete draft and `false` validates and publishes at once. See
[Drafts and versions](./drafts-and-versions.md) for what leaving it out does.

`Select` controls stored fields; `OutputFields` separately controls computed fields and inverse
joins. `AllLocales` is for reads only; write one locale at a time, or use `CopyLocale`.

## Next {#next}

- [Typed handles](./local-api/typed-handles.md): generated structs instead of `store.Values`.
- [Transactions and errors](./local-api/errors.md): nested calls, rollbacks, and handling
  `*ridu.OperationError`.
- The [Go API reference](https://riducms.com/reference/ridu/) lists every method and option.
