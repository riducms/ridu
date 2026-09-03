---
title: 'Local Go API'
description: 'Read and mutate collections and globals through the full Ridu operation engine without an HTTP round trip.'
product: data
eyebrow: 'Data and APIs'
order: 90
navigation:
  section: 'Work with data'
  parent: 'data-access'
  order: 10
  title: 'Local Go API'
---

`app.Local()` is the in-process data API for Go handlers, task code, application services, access
rules, and hooks. It avoids serialization and an HTTP round trip, but it is not a privileged store
handle: access, field redaction, validation, relationships, hooks, versions, transactions, and
localization are the same engine used by REST, the SDK, GraphQL, and the admin.

## Dynamic values {#dynamic-values}

The dynamic surface uses `store.Values`, a map whose values have a finite JSON-shaped vocabulary.
Construct values with `store.String`, `Number`, `Boolean`, `Object`, `List`, and `Null` rather than
passing `any`.

```go title="service/posts.go"
package service

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func RecentPosts(ctx context.Context, app *ridu.App, actor *store.Document) (store.Page, error) {
	post, err := app.Local().Create(ctx, "posts", store.Values{
		"title":   store.String("Hello, Ridu"),
		"summary": store.String("An in-process write."),
	}, actor)
	if err != nil {
		return store.Page{}, err
	}

	title, _ := post.Values["title"].StringValue()
	_ = title

	categoryPath, err := query.NewPath("category")
	if err != nil {
		return store.Page{}, err
	}
	categorySort, err := query.NewSort(categoryPath, query.Ascending)
	if err != nil {
		return store.Page{}, err
	}
	return app.Local().List(ctx, "posts", ridu.ListOptions{
		Page:  1,
		Limit: 20,
		Where: query.Equal(categoryPath, query.String("news")),
		Sort:  []query.Sort{categorySort},
		Actor: actor,
	})
}
```

Values returned from the store are detached snapshots. A string relationship value is an ID;
when that path is explicitly populated, it becomes a `store.Populated` document value. See
[Querying data](/docs/querying/) for expressions, selection, sorting, and bounded population.

## Pass the caller, not a bypass flag {#actors}

The final `actor` argument, or `Actor` in an options struct, is the authenticated document supplied
to access rules and hooks. `nil` means anonymous. It never means superuser, and the local API has no
access-override option.

When an application has more than one auth collection, a document ID is not a complete identity.
Carry the collection slug as well:

```go
session, err := app.Session(ctx, rawSessionToken)
if err != nil {
	return err
}

post, err := app.Local().FindWithOptions(ctx, "posts", postID, ridu.FindOptions{
	Actor:           &session.User,
	ActorCollection: session.Collection,
})
```

All option-bearing reads, mutations, and capability checks accept `ActorCollection`. Transport
code should preserve the exact `ridu.AuthIdentity` established by authentication and copy both
`Actor` and `Collection` into local options. Concise actor-only methods remain convenient for an
application's trusted internal work. Identity-sensitive application services such as scheduling,
preferences, previews, account unlocks, and document locks accept `AuthIdentity` directly.

## Read and write options {#options}

The short methods accept an actor and optional `LocaleOptions`. Use the `WithOptions` forms when a
request needs more control:

| Type                | Controls                                                                                                                                        |
| ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `FindOptions`       | `Select`, relationship `Populate`, computed/join `OutputFields`, draft visibility, exact actor identity, trash-only mode, and locale projection |
| `ListOptions`       | All find controls plus `Where`, one-based `Page`, `Limit`, and ordered `Sort`                                                                   |
| `MutationOptions`   | Exact actor identity, optimistic `ExpectedRevision`, returned population/output fields, draft status, and write locale                          |
| `CapabilityOptions` | Candidate `Data`, exact actor identity, trash mode, and locale for a side-effect-free permission summary                                        |
| `LocaleOptions`     | Locale, replacement fallback chain, fallback disablement, or all-locales reads for concise methods                                              |

`Select` projects stored fields. `OutputFields` independently controls computed fields and inverse
joins: nil resolves all, while a non-nil empty slice resolves none. `Populate` changes the returned
shape only; it does not change what is validated or saved. `AllLocales` is read-only for ordinary
create/update calls—write one locale at a time or use `CopyLocale`.

`Draft` is a pointer so omitted, true, and false stay distinct. For reads, true includes drafts and
false restricts results to published documents. For creates, true requests draft and false requests
published status. Updates do not accept status intent: edit drafts with `Update`, and use
`PublishChanges` or `Unpublish` for lifecycle transitions. The omitted defaults are covered in
[Drafts and versions](/docs/drafts-and-versions/).

## Collection operation map {#collection-operations}

| Task                   | Local API methods                                                                                                                                        |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Create or copy         | `Create`, `CreateWithOptions`, `Duplicate`, `DuplicateWithOptions`                                                                                       |
| Read                   | `Find`, `FindWithOptions`, `List`                                                                                                                        |
| Inspect permission     | `Capabilities` returns operation and field booleans without exposing rule code or filtered predicates                                                    |
| Update                 | `Update`, `UpdateWithOptions`, `UpdateRevision`                                                                                                          |
| Maintain inverse joins | `MutateJoin`, `MutateJoinWithOptions` atomically add/remove source IDs                                                                                   |
| Publish state          | `Publish`, `Unpublish` and their option-bearing forms                                                                                                    |
| Localization           | `CopyLocale`, `CopyLocaleWithOptions`                                                                                                                    |
| Version history        | `Versions`, `Version`, restore/preserve-status, and restore-as-draft forms                                                                               |
| Delete                 | `Delete`; trash-enabled resources also have restore, permanent delete, and `EmptyTrash`                                                                  |
| Bulk                   | Bounded update, publish, unpublish, delete, restore-deleted, and permanent-delete methods                                                                |
| Migration import       | `Import` preserves a source ID, status, and timestamps while still running ordinary access, validation, hooks, relationships, versions, and transactions |

Capability results are suitable for deciding which controls to show. They are not a lease or
authorization token; run the requested operation and handle its result. Import is intended for trusted
migration code, not ordinary end-user creation. Upload collections use the storage-aware methods
on `App` rather than dynamic `Create` for file-bearing documents.

Bulk operations accept 1–100 explicit IDs and commit every item or none. The current Go bulk,
`EmptyTrash`, and `Import` signatures are actor-only: they do not accept `ActorCollection` or an
`AuthIdentity`. In a multi-auth application, use the REST/SDK bulk transport—which preserves the
transport's exact identity—or reserve these local methods for application-owned callers whose
identity cannot be ambiguous. Individual option-bearing local methods are the exact-identity path.

## Globals use singleton methods {#globals}

Globals do not pretend to be one-row collections. Use `Global`/`GlobalWithOptions` to read and
`UpdateGlobal`/`UpdateGlobalWithOptions` to create-or-update the singleton. Version-enabled globals
also expose publish, unpublish, copy-locale, version reads, and restore methods.

```go
settings, err := app.Local().UpdateGlobalWithOptions(
	ctx,
	"site-settings",
	store.Values{"siteName": store.String("Acme")},
	ridu.MutationOptions{
		Actor:            actor,
		ActorCollection: "users",
		ExpectedRevision: currentRevision,
	},
)
```

There are no global create/list/delete/trash, auth, upload, or lock methods. A never-persisted
global reads as a schema-shaped singleton with defaults; its first update persists it.

## Generated typed handles {#typed-handles}

`ridu generate` writes output, create, and update structs plus typed collection and global handles.
Bind a generated definition to the same local engine:

```go title="service/typed-posts.go"
posts := generated.PostsCollection.With(app.Local())

post, err := posts.Create(ctx, generated.PostCreate{
	Title:   "Hello, Ridu",
	Summary: "Checked by Go's compiler",
}, actor)
if err != nil {
	return err
}

page, err := posts.List(ctx, core.TypedListOptions{
	Page: 1, Limit: 20, Actor: actor, ActorCollection: "users",
})
```

Typed collection handles currently cover create, import, find, list, update, revision-aware update,
and delete. Typed global handles cover find, update, publish, unpublish, restore, and
restore-as-draft. Use the dynamic local API for advanced operations that are not on a generated
handle. The typed layer JSON-encodes generated input and decodes the result; it changes compile-time
ergonomics, not runtime semantics or authorization.

Generated mutation fields are presence-aware. Nullable fields use `*core.Input[T]`: nil omits the
key, `core.Set(value)` sends a concrete value, and `core.Null[T]()` sends explicit JSON `null`.
Non-null slices, maps, fallback `json.RawMessage` fields, and plugin-owned Go types whose JSON
nullability cannot be proven use `core.NonNullInput[T]`: construct a required value with
`core.NonNull(value)`, or an omittable default/update value with `core.SetNonNull(value)`. Encoding
rejects nil or otherwise null-encoding wrapped values. Other non-null fields that are optional only
in an update or because a server default exists use `*T`. Generated mutation wrappers are
write-only, not general-purpose JSON-unmarshal contracts. `TypedListOptions` excludes population
and all-locale reads because those operations change relationship and localized field shapes; use
the dynamic local API when you need either.

Except for typed `List`, these generated methods currently take an actor document rather than an
options struct, so they cannot carry `ActorCollection`, locale selection, draft intent, or returned
population. In a multi-auth or option-rich operation, keep the generated input/output types where
useful but call the dynamic `WithOptions` method so the exact identity and request semantics are not
lost.

## Optimistic revisions {#optimistic-writes}

Versioned documents and globals expose `_revision`. Pass the revision you last observed through
`ExpectedRevision`, `UpdateRevision`, or the expected-revision argument on publish, unpublish, and
restore. Zero means no revision fence; a stale positive revision returns a `conflict` operation
error instead of replacing a newer edit.

```go
updated, err := app.Local().UpdateWithOptions(ctx, "posts", post.ID, values, ridu.MutationOptions{
	Actor:            actor,
	ActorCollection: "users",
	ExpectedRevision: post.Revision,
})
```

Document locks coordinate editors but do not replace this fence. Prefer revisions on every
interactive write.

## Nested calls and transactions {#nested-transactions}

`HookContext.Local`, `AccessContext.Local`, and `FieldAccessContext.Local` expose the same API.
Calls made during a pre-commit operation phase automatically reuse the outer store transaction.
The nested operation still runs its own access, validation, hooks, version snapshot, and redaction;
if it fails, the transaction becomes rollback-only even when a hook tries to swallow the error.

After-commit and after-error callbacks run outside the completed/failed transaction, so their local
calls start a new transaction. An after-commit error means the original document is already durable
and must not be blindly retried. Guard operations that call back into the same hooked resource:
Ridu bounds total nesting and repeated operation frames, but `operation_recursion` is a safety net,
not workflow design.

Ridu does not currently expose caller-controlled begin/commit/rollback or savepoints. Put atomic
related writes in transactional hook phases and use an after-commit dispatcher or durable task for
external effects. See [Hooks](/docs/hooks/).

## Structured errors {#errors}

Local failures can be inspected as `*ridu.OperationError`. Branch on `Code`, HTTP-shaped `Status`,
and validation `Issues`; do not parse `Message`.

```go
import "errors"

post, err := app.Local().UpdateRevision(ctx, "posts", id, patch, revision, actor)
if err != nil {
	var operationErr *ridu.OperationError
	if errors.As(err, &operationErr) {
		switch operationErr.Code {
		case "conflict":
			return reloadAndAskTheAuthor(operationErr)
		case "validation":
			return showFieldIssues(operationErr.Issues)
		case "access_denied", "not_found":
			return hideUnavailableDocument()
		}
	}
	return err
}
_ = post
```

`not_found` may hide a row rejected by filtered access. `Committed` marks an error
from work after a successful commit; `CommitAttempted` marks an ambiguous commit outcome. Those
flags matter when reconciling external objects or deciding whether retrying a mutation is safe.
The complete methods and option members are in the [Go API reference](/reference/ridu/).
