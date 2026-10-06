<!-- Generated from website/src/content/docs/local-api/typed-handles.md by scripts/sync-agent-docs.ts. -->

# Typed handles

`ridu generate` writes a Go struct for each collection's documents, creates and updates, plus a
handle that reads and writes them. Bind a handle to the Local API and the compiler checks every
field name and type. It runs the same operations as `store.Values`; only the types change.

Examples use the [example schema](../querying.md#schema). Its generated package is `generated`.

## Bind a handle {#bind}

```go
posts := generated.PostsCollection.With(app.Local())
```

Bind once and reuse the handle. In a hook, bind to `ctx.Local` so calls join the hook's
transaction.

## Create {#create}

```go
post, err := posts.Create(ctx, generated.PostCreate{
	Title:  "Hello, Ridu",
	Status: core.Set(generated.PostStatusPublished),
	Tags:   core.Set([]string{"launch", "go"}),
}, ridu.TypedMutationOptions{Actor: user})
```

`post` is a `generated.Post`. Required fields, such as `Title`, are plain values; the others are
optional, as described in [Optional and null values](#inputs).

## Read {#read}

```go
page, err := posts.List(ctx, core.TypedListOptions{
	Where: query.Equal("status", "published"),
	Sort:  []query.Sort{query.Desc("publishedAt")},
	Actor: user,
})
if err != nil {
	return err
}
for _, post := range page.Documents {
	fmt.Println(*post.Title)
}
```

`List` takes the same filters, sorting, pagination, selection and population as the dynamic API;
see [Querying data](../querying.md). `Find` reads one document with `core.TypedReadOptions`.

Output fields are pointers because access rules or a `select` can leave any of them out. A
relationship is a `generated.Reference[T]` with the target's `ID`, and its `Document` once you
populate it:

```go
post, err := posts.Find(ctx, postID, core.TypedReadOptions{
	Populate: []query.Population{{Path: query.Field("author")}},
	Actor:    user,
})
if err != nil {
	return err
}
if post.Author != nil && post.Author.Document != nil {
	fmt.Println(*post.Author.Document.Name)
}
```

## Update and delete {#update}

```go
title := "A better idea"
post, err := posts.Update(ctx, postID, generated.PostUpdate{
	Title: &title,
	Tags:  core.Null[[]string](),
}, ridu.TypedMutationOptions{Actor: user})
```

This retitles the post and clears its tags; fields you leave `nil` keep their values. `Delete`
takes the ID and the same options.

## Optional and null values {#inputs}

An optional field in a create or update struct is a `*core.Input[T]`:

| Write            | To                                                       |
| ---------------- | -------------------------------------------------------- |
| `nil`            | leave the field out, keeping its stored value or default |
| `core.Set(v)`    | set it to `v`                                            |
| `core.Null[T]()` | clear it                                                 |

A required field is a plain value in a create, and a `*T` in an update because you may leave it
out; it can't be cleared. Lists, maps and some plugin types that can never be null use
`core.NonNullInput[T]`: set them with `core.NonNull(v)` when required, or `core.SetNonNull(v)` when
optional.

## Publish a versioned document {#publish}

In a versioned collection, a published document changes by being published again. Send the changes
to `PublishChanges` with the revision you read:

```go
pages := generated.PagesCollection.With(app.Local())

title := "About us"
updated, err := pages.PublishChanges(ctx, page.ID, generated.PageUpdate{
	Title: &title,
}, ridu.TypedMutationOptions{
	ExpectedRevision: page.Revision,
	Actor:            user,
})
```

`Update` refuses a published document here with `publish_required`. `Publish` publishes a saved
draft, and `Unpublish` takes a document offline and keeps its content. See
[Update a versioned document](../writing-data.md#versioned) for revision checks.

## Globals {#globals}

```go
settings := generated.SiteSettingsGlobal.With(app.Local())

current, err := settings.Update(ctx, generated.SiteSettingsUpdate{
	SiteName: core.Set("Acme"),
}, ridu.TypedMutationOptions{Actor: user})
```

A global handle has `Find` and `Update`, and for a versioned global `PublishChanges`, `Publish`,
`Unpublish`, `Restore`, `RestoreAsDraft`, `SaveDraft` and `DiscardDraft`.

## What a handle covers {#coverage}

A collection handle has `Create`, `Find`, `List`, `Update`, `Delete` and `Import`; for versioned
collections, `PublishChanges`, `Publish` and `Unpublish`; and for collections with drafts,
`CreateDraft`, `SaveDraft` and `DiscardDraft`. For bulk actions, trash, versions, locale copies
and inverse joins, use the dynamic Local API from the [operation table](../local-api.md#operations)
with the same `ctx` and caller.

A localized project also gets `AllLocales` read bindings, whose fields hold every locale's value.
Typed writes always target one locale.
