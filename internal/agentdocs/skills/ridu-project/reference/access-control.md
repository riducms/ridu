<!-- Generated from website/src/content/docs/access-control.md by scripts/sync-agent-docs.ts. -->

# Access control

Access rules decide who can use your content. Collection and global rules protect documents;
field rules protect individual values. Ridu checks these rules for requests from the admin, REST,
the SDK, and the local Go API.

If `operation.Context`, `store.Document`, or `query.Path` is unfamiliar, start with
[Go packages](./go-packages.md). That guide explains the values and return types used below.

## Access configuration {#configuration}

| API                                        | Where it applies                                                         | Result                                                                                    |
| ------------------------------------------ | ------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------- |
| `ridu.Allow()`                             | Collection or global access rule                                         | Permits the operation, subject to field rules and normal validation.                      |
| `ridu.Deny()`                              | Collection or global access rule                                         | Rejects the operation.                                                                    |
| `ridu.Where(expression)`                   | Collection document operations                                           | Adds a database predicate atomically to the caller's query or target lookup.              |
| `Collection.Access`                        | `Create`, `Read`, `Update`, `Delete`, and capability-specific operations | Configures document-level rules for each operation family.                                |
| `Global.Access`                            | `Read`, `Update`                                                         | Configures singleton access.                                                              |
| Field `.Access(field.Access{...})`         | `Create`, `Read`, `Update`                                               | Replaces the field's boolean rules. A denied read removes the field from output.          |
| Field `.RestrictAccess(...)`               | Existing field access                                                    | Conjoins supplied rules with inherited or reusable rules.                                 |
| `ctx.Actor` / `ctx.ActorCollection`        | Every access callback                                                    | Identifies the signed-in document and the auth collection that owns it.                   |
| `ctx.Data`, `ctx.Document`, `ctx.Siblings` | Phase-specific callbacks                                                 | Exposes current candidate or saved values; see each context before assuming availability. |

## Write an access rule {#decisions}

A collection rule receives `ridu.AccessContext`, which includes the signed-in user (`Actor`),
the requested operation, document ID, submitted values, and locale. Return one of these decisions:

- `ridu.Allow()` permits the operation.
- `ridu.Deny()` rejects it.
- `ridu.Where(expression)` permits it only for documents that match a filter, such as posts
  belonging to the signed-in author.

Keep reusable rules beside your content model. These helpers allow public reads, require a
signed-in user, or limit access to documents belonging to that user:

```go title="content/access.go" focus={13-17,26-29}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
)

func publicRead(ridu.AccessContext) (ridu.AccessDecision, error) {
	return ridu.Allow(), nil
}

func signedIn(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	// Actor is nil when no user is signed in.
	if ctx.Actor == nil {
		return ridu.Deny(), nil
	}
	return ridu.Allow(), nil
}

func ownDocuments(path query.Path) ridu.AccessRule {
	// Return a rule that remembers this collection's owner field.
	return func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil {
			return ridu.Deny(), nil
		}
		// Check ownership as part of the database operation.
		return ridu.Where(
			query.Equal(path, query.String(ctx.Actor.ID)),
		), nil
	}
}
```

## Protect a collection {#collection-rules}

Add the rules to `Access` on your collection. This example lets anyone read posts, lets signed-in
users create them, and lets authors update or delete their own posts. The `author` relationship
stores the user ID checked by `ownDocuments`. The `internalNotes` helper is defined below.

```go title="content/posts.go" focus={23-28}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
)

func Posts() ridu.Collection {
	// Check this fixed query path while constructing the config.
	authorPath, err := query.NewPath("author")
	if err != nil {
		panic(err)
	}

	return ridu.Collection{
		Slug: "posts",
		Fields: field.Fields{
			field.Text("title").Required(),
			field.Relationship("author", "users").Required(),
			internalNotes("internalNotes"),
		},
		Access: ridu.CollectionAccess{
			Create: signedIn,
			Read:   publicRead,
			Update: ownDocuments(authorPath),
			Delete: ownDocuments(authorPath),
		},
	}
}
```

Use `Posts()` in your config's `Collections` list. It is a function here so it can check the error
from `query.NewPath` before returning the collection.

<aside class="callout" data-variant="important">
<strong>When to use a filter</strong>
<p><code>Where</code> checks the filter in the same database operation that reads or changes the document. Use it for reads, version history, updates, deletes, and unlocking. Creating a document or entering the admin requires <code>Allow</code> or <code>Deny</code>. Global rules can filter an existing global, but its first save requires <code>Allow</code> because there is no saved document to check.</p>
</aside>

## Protect an individual field {#field-access}

Field rules return `true` to allow access or `false` to deny it. If a write is denied, Ridu rejects
the write. If a read is denied, Ridu leaves the field out of the response.

Their `operation.Context` is a field callback context. See
[Operations and callbacks](./go-packages/operation.md) for the caller, nearby values, and
how this differs from a collection's `ridu.AccessContext`.

This helper creates a notes field that only administrators can write and that editors and
administrators can read. Reuse it in any collection, group, array, or block that needs the same
protection.

```go title="content/field_access.go" focus={21-26}
package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func internalNotes(name string) field.TextField {
	allowed := func(roles ...string) field.AccessRule {
		return func(ctx operation.Context) (bool, error) {
			// A missing role matches none of the allowed names.
			role, _ := ctx.Actor.Data.String("role")
			for _, candidate := range roles {
				if role == candidate {
					return true, nil
				}
			}
			return false, nil
		}
	}
	return field.Textarea(name).Access(field.Access{
		// Protect creates as well as updates.
		Create: allowed("admin"),
		Read:   allowed("editor", "admin"),
		Update: allowed("admin"),
	})
}
```

Here only administrators may create or update `internalNotes`; editors may read it. Omitting the
`Create` rule would leave that write allowed by default, even if `Update` were restricted.

Setting `field.Admin{ReadOnly: true}` only disables editing in the admin. Use access rules when
the API must reject changes as well.

`.Access(...)` replaces all of a field's access rules. When extending a reusable field, use
`.RestrictAccess(...)` to add a restriction while keeping its existing rules; both rules must
allow the operation.

### Filtering and sorting protected fields {#protected-field-queries}

Adding a `Read` rule also prevents API callers from filtering or sorting by that field. Otherwise,
someone could infer a hidden value by trying different filters, even though the response omitted
it. This restriction applies to every user, including users who can read the field in an individual
document.

It covers filters used by lists, counts, distinct values, select-all operations, and index windows.
Protecting a group, array, or block also protects its nested fields. Sorting the parent is blocked
when a nested field has a `Read` rule. Choosing another locale does not bypass the restriction.
These queries return HTTP `403 access_denied`; the local API and GraphQL use
`field_access_denied`.

Your own `ridu.Where(...)` access rules can still filter by a protected field. For example, Ridu
can enforce ownership without exposing the owner field to the caller. Inverse relationships also
continue to find their related documents, but callers cannot add filters or sorting on a protected
backing field.

Use the field paths described in [Querying data](./querying.md). Invalid paths, including block
paths without a variant and paths into internal rich-text storage, return `400 bad_request`
(`bad_query` in the local API). Queries inside an unrestricted JSON field depend on the database
adapter's support for JSON queries.

## Use another field in an access rule {#sibling-data}

Read `ctx.Siblings` to check other fields in the same group, array row, or block. For a field at
the document's top level, it contains the document's top-level fields. It also includes the field
being checked.

Here each link has a `membersOnly` checkbox. Anonymous readers receive the URL only when that
checkbox is off; signed-in readers can see every URL. Field rules identify an anonymous reader
with an empty `ctx.Actor.ID`.

```go title="content/pages.go" focus={8-12}
var Pages = ridu.Collection{
	Slug: "pages",
	Fields: field.Fields{
		field.Array("links", field.Fields{
			field.Text("label").Required(),
			field.Text("url").Required().Access(field.Access{
				Read: func(ctx operation.Context) (bool, error) {
					// Read the checkbox in this link row.
					membersOnly, _ := ctx.Siblings.Get(
						"membersOnly",
					).BooleanValue()
					return !membersOnly || ctx.Actor.ID != "", nil
				},
			}),
			field.Checkbox("membersOnly").Default(false),
		}),
	},
}
```

During a write, `ctx.Prior` contains the saved values from the same group or row before the change.
Ridu matches array and block rows by their key, so reordering rows does not mix up their previous
values. New rows and ordinary reads have no previous values in `Prior`.

Access rules can read these values but cannot change them. Use a [field hook](./hooks/fields.md#normalize-input)
to change a value. See [Using other field values](./fields/callback-values.md) for examples of
`Root`, `Siblings`, `Prior`, and looking up a related document, or
[`operation.Context`](https://riducms.com/reference/operation/context/) for the full reference.
