<!-- Generated from website/src/content/docs/local-api.md by scripts/sync-agent-docs.ts. -->

# Local Go API

`app.Local()` is the in-process data API for Go handlers, task code, application services, access
rules, and hooks. It avoids serialization and an HTTP round trip, but it is not a privileged store
handle: access, field redaction, validation, relationships, hooks, versions, transactions, and
localization are the same engine used by REST, the SDK, GraphQL, and the admin.

## Dynamic values {#dynamic-values}

The dynamic API uses `store.Values`, a map from field names to document values. Construct a value
with `store.String`, `Number`, `Boolean`, `Object`, `List`, or `Null`.
[Documents and values](./go-packages/store.md) explains these constructors, reading their
results, nested objects, and the difference between omitting a field and clearing it.

```go title="service/posts.go"
package service

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func RecentPosts(
	ctx context.Context,
	app *ridu.App,
	actor *store.Document,
) (store.Page, error) {
	post, err := app.Local().Create(ctx, "posts", store.Values{
		"title":   store.String("Hello, Ridu"),
		"summary": store.String("An in-process write."),
	}, ridu.MutationOptions{Actor: actor})
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
[Querying data](./querying.md) for expressions, selection, sorting, and bounded population.

## Pass the caller, not a bypass flag {#actors}

`Actor` in the final options argument is the authenticated document supplied
to access rules and hooks. `nil` means anonymous. It never means superuser, and the local API has no
access-override option.

When an application has more than one auth collection, a document ID is not a complete identity.
Carry the collection slug as well:

```go
session, err := app.Session(ctx, rawSessionToken)
if err != nil {
	return err
}

post, err := app.Local().Find(
	ctx,
	"posts",
	postID,
	ridu.FindOptions{
		Actor:           &session.User,
		ActorCollection: session.Collection,
	},
)
```

All option-bearing reads, mutations, and capability checks accept `ActorCollection`. Transport
code should preserve the exact `ridu.AuthIdentity` established by authentication and copy both
`Actor` and `Collection` into local options. Identity-sensitive application services such as scheduling,
preferences, previews, account unlocks, and document locks accept `AuthIdentity` directly.

## Read and write options {#options}

Each semantic action has one signature: context, resource identifiers and required values are
positional; optional controls go in one final named options value. History revisions remain positional.

| Type                   | Controls                                                                                                                                        |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `FindOptions`          | `Select`, relationship `Populate`, computed/join `OutputFields`, draft visibility, exact actor identity, trash-only mode, and locale projection |
| `ListOptions`          | All find controls plus `Where`, one-based `Page`, `Limit`, and ordered `Sort`                                                                   |
| `MutationOptions`      | Exact actor identity, optimistic `ExpectedRevision`, returned population/output fields, draft status, and write locale                          |
| `CapabilityOptions`    | Candidate `Data`, exact actor identity, trash mode, and locale for a side-effect-free permission summary                                        |
| `BulkOptions`          | Exact actor identity and locale controls for bulk actions and empty-trash                                                                       |
| `ImportOptions`        | Exact actor identity and source metadata for import                                                                                             |
| `TypedMutationOptions` | Mutation controls without `AllLocales`; typed writes remain single-locale                                                                       |

Caller `Where`, `Sort`, `Distinct` and `ListWindow` paths follow the
[field query-access contract](./access-control.md#field-access). A configured Read rule on
the field or an ancestor makes the path unavailable for queries, even to actors who can read it
in individual documents. Collection access predicates remain trusted.

`Local().ListJoin(ctx, sourceCollection, sourceID, joinField, options)` reads an authored inverse
join through source and target read access. The engine derives the membership constraint from
config; `options.Where` and `options.Sort` retain ordinary caller query restrictions.

`Select` projects stored fields. `OutputFields` independently controls computed fields and inverse
joins: nil resolves all, while a non-nil empty slice resolves none. `Populate` changes the returned
shape only; it does not change what is validated or saved. `AllLocales` is read-only for ordinary
create/update calls—write one locale at a time or use `CopyLocale`.

`Draft` is a pointer so omitted, true, and false stay distinct. For reads, true includes drafts and
false restricts results to published documents. For creates, true requests draft and false requests
published status. Updates do not accept status intent: edit drafts with `Update`, and use
`PublishChanges` or `Unpublish` for lifecycle transitions. The omitted defaults are covered in
[Drafts and versions](./drafts-and-versions.md).

## Collection operation map {#collection-operations}

| Task                   | Local API methods                                                                                                                                        |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Create or copy         | `Create`, `Duplicate`                                                                                                                                    |
| Read                   | `Find`, `List`                                                                                                                                           |
| Inspect permission     | `Capabilities` returns operation and field booleans without exposing rule code or filtered predicates                                                    |
| Update                 | `Update`                                                                                                                                                 |
| Maintain inverse joins | `MutateJoin` atomically add/remove source IDs                                                                                                            |
| Publish state          | `Publish`, `PublishChanges`, `Unpublish`                                                                                                                 |
| Localization           | `CopyLocale`                                                                                                                                             |
| Version history        | `Versions`, `Version`, restore/preserve-status, and restore-as-draft forms                                                                               |
| Delete                 | `Delete`; trash-enabled resources also have restore, permanent delete, and `EmptyTrash`                                                                  |
| Bulk                   | Bounded update, publish, unpublish, delete, restore-deleted, and permanent-delete methods                                                                |
| Migration import       | `Import` preserves a source ID, status, and timestamps while still running ordinary access, validation, hooks, relationships, versions, and transactions |

Capability results are suitable for deciding which controls to show. They are not a lease or
authorization token; run the requested operation and handle its result. Import is intended for trusted
migration code, not ordinary end-user creation. Upload collections use the storage-aware methods
on `App` rather than dynamic `Create` for file-bearing documents.

Bulk operations accept 1–100 explicit IDs and commit every item or none. `BulkOptions` forwards
both `Actor` and `ActorCollection`, plus locale controls. `ImportOptions` carries the same identity.
Special operations consume their documented controls: history reads use identity and locale;
copy-locale uses identity and expected revision; inverse-join mutations use identity and locale.
Accepting an options value does not enable unrelated selection or publication settings.

## Globals use singleton methods {#globals}

Globals do not pretend to be one-row collections. Use `Global` to read and
`UpdateGlobal` to create-or-update the singleton. Version-enabled globals
also expose publish, unpublish, copy-locale, version reads, and restore methods.

```go
settings, err := app.Local().UpdateGlobal(
	ctx,
	"site-settings",
	store.Values{"siteName": store.String("Acme")},
	ridu.MutationOptions{
		Actor:            actor,
		ActorCollection:  "users",
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
}, ridu.TypedMutationOptions{Actor: actor})
if err != nil {
	return err
}

page, err := posts.List(ctx, core.TypedListOptions{
	Page: 1, Limit: 20, Actor: actor, ActorCollection: "users",
})
```

Typed collection handles currently cover create, import, find, list, update,
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
in an update or because a server default exists use `*T`. Generated mutation wrappers expose `Get()` for inspecting a concrete value, and generated field
codecs preserve explicit nulls when decoding mutations.

Use the ordinary collection handle for reads and writes. `Find` accepts `core.TypedReadOptions`,
and `List` accepts `core.TypedListOptions`; both support population, projection, actor identity,
draft intent and locale selection. Global `Find` uses the same read options. Generated relationship
values expose an `ID` and an optional typed `Document`, so population does not require switching
handles. Localized projects generate separate `AllLocales` read bindings with locale-map models.

Typed writes accept `TypedMutationOptions`, forwarding actor collection, expected revision,
returned population and locale controls. They remain single-locale and omit `AllLocales`.

## Optimistic revisions {#optimistic-writes}

Versioned documents and globals expose `_revision`. Pass the revision you last observed through
`ExpectedRevision` in the final mutation options on update, publish, unpublish, and restore. Zero means no revision fence; a stale positive revision returns a `conflict` operation
error instead of replacing a newer edit.

```go
updated, err := app.Local().Update(
	ctx,
	"posts",
	post.ID,
	values,
	ridu.MutationOptions{
		Actor:            actor,
		ActorCollection:  "users",
		ExpectedRevision: post.Revision,
	},
)
```

Document locks coordinate editors but do not replace this fence. Prefer revisions on every
interactive write.

## Nested calls and transactions {#nested-transactions}

Resource `HookContext.Local` and `AccessContext.Local` expose the same API. Field callbacks
receive `operation.Reader`, a bound read-only lookup that preserves the actor, locale, and transaction.
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
external effects. See [Hooks](./hooks.md).

## Structured errors {#errors}

Local failures can be inspected as `*ridu.OperationError`. Branch on `Code`, HTTP-shaped `Status`,
and validation `Issues`; do not parse `Message`.

```go
import "errors"

post, err := app.Local().Update(ctx, "posts", id, patch, ridu.MutationOptions{Actor: actor, ExpectedRevision: revision})
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
The complete methods and option members are in the [Go API reference](https://riducms.com/reference/ridu/).
