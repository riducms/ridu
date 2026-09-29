<!-- Generated from website/src/content/docs/hooks/collections.md by scripts/sync-agent-docs.ts. -->

# Collection hooks

Collection hooks run your Go functions at set points while Ridu creates, reads, updates, or
deletes a document. Use them when your code needs the whole document. To change a single field,
use [field hooks](./fields.md) instead.

Add hooks to a collection's `Hooks` property. This collection uses one hook from each section on
this page:

```go title="content/articles.go" focus={21-31}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Articles = ridu.Collection{
	Slug: "articles",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Text("slug"),
		field.Textarea("body"),
		field.Text("excerpt").Required(),
		field.Number("wordCount"),
		field.Text("metaTitle"),
		field.Checkbox("featured"),
		field.Text("lastEditedBy"),
	},
	// Each list runs its functions in the order you write them.
	Hooks: ridu.CollectionHooks{
		BeforeValidate:  []ridu.Hook{fillExcerpt},
		BeforeChange:    []ridu.Hook{recordLastEditor},
		BeforeOperation: []ridu.Hook{countWords},
		AfterChange:     []ridu.Hook{writeAuditEntry},
		AfterRead:       []ridu.Hook{fillMetaTitle},
		BeforeDuplicate: []ridu.Hook{markCopy},
		BeforeDelete:    []ridu.Hook{keepFeatured},
		AfterError:      []ridu.Hook{logFailure},
		AfterCommit:     []ridu.Hook{purgeArticleCache},
	},
}
```

Every collection hook has the same shape. It receives a `ridu.HookContext` and returns an error:

```go
func myHook(ctx ridu.HookContext) error {
	// Read ctx, or change ctx.Data before a save.
	return nil // Continue. Return an error to stop the operation.
}
```

Hooks run on the server for every request: from the admin, REST, the TypeScript SDK, and the
local Go API. Ridu waits for each hook to finish before it moves on.

Returning an error stops the operation, and Ridu rolls back anything it wrote. To tell the editor
why, return `ridu.Reject("…")`: the admin shows your message, and API callers receive a
`rejected` error. Any other error is reported to the caller as an internal error, and its message
stays in your server logs. An error from `AfterCommit` cannot undo the save; see
[Transactions and errors](./transactions-and-errors.md).

## Hook arguments {#arguments}

`ctx` describes the current operation. Which properties are set depends on the hook; each section
below lists what its hook receives.

| Property              | Description                                                                                                                |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `ctx.Operation`       | What is happening, such as `operation.Create`, `operation.Update`, or `operation.Read`.                                    |
| `ctx.Data`            | The values being saved. Change its entries to change what is saved. On an update, it holds only the fields that were sent. |
| `ctx.Candidate()`     | The values the document will have once saved: `ctx.Original` with each field in `ctx.Data` on top. A detached copy.        |
| `ctx.Original`        | The saved document before an update, duplicate, or delete. `nil` on create and on reads.                                   |
| `ctx.Document`        | The document Ridu returns, once it exists. Changes to its values affect the response only.                                 |
| `ctx.Actor`           | The signed-in user's document, or `nil` for an anonymous request.                                                          |
| `ctx.ActorCollection` | The auth collection that the signed-in user belongs to.                                                                    |
| `ctx.System`          | `true` when trusted server code started the operation with [`System`](../local-api.md#system), so no access rule ran.     |
| `ctx.Local`           | The [local API](../local-api.md), for reading or writing other documents.                                                 |
| `ctx.Context`         | Cancellation, deadline, and the active transaction. Pass it to `ctx.Local` and network calls.                              |
| `ctx.Locale`          | The content locale, or empty without [localization](../localization.md).                                                  |
| `ctx.Error`           | The failure. Set only in `AfterError`.                                                                                     |
| `ctx.CollectionID`    | Ridu's stable ID for the collection. It is not the slug.                                                                   |

Values in `ctx.Data` and `ctx.Document.Values` are `store.Value`s. Read one with
`ctx.Data["title"].StringValue()` and set one with `ctx.Data["title"] = store.String("Hello")`.
[Documents and values](../go-packages/store.md) covers every value type, and
[operation kinds](../go-packages/operation.md#operation-kinds) lists every `ctx.Operation`.

## Which hooks run for each operation {#lifecycle}

| Operation                          | Hooks, in order                                                                                                        |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| Create, update, publish, unpublish | `BeforeValidate` → `BeforeChange` → `BeforeOperation` → `AfterChange` → `AfterOperation` → `AfterRead` → `AfterCommit` |
| Duplicate                          | `BeforeDuplicate`, then the same hooks as a create                                                                     |
| Find or list                       | `BeforeRead` → `BeforeOperation` → `AfterOperation` → `AfterRead`                                                      |
| Delete, to trash or permanently    | `BeforeDelete` → `BeforeOperation` → `AfterDelete` → `AfterOperation` → `AfterRead` → `AfterCommit`                    |
| Restore from trash                 | `BeforeOperation` → `AfterOperation` → `AfterRead` → `AfterCommit`                                                     |
| Any operation that fails           | `AfterError`                                                                                                           |

`BeforeOperation` and `AfterOperation` run for every operation, including reads; check
`ctx.Operation` inside them. `AfterCommit` runs after every committed change, including deletes
and restores, but never after a read. On a list, `AfterRead` runs once for each returned document. [Where validation and field hooks fit](#save-order) shows where
built-in checks, custom validators, and field hooks run between these hooks.

## BeforeValidate {#before-validate}

Runs before Ridu checks field values, such as `Required()`, when a document is created,
duplicated, updated, published, or unpublished. Use it to fill in or clean up submitted values
that must pass those checks.

**Receives:** `ctx.Data` with the submitted values. `ctx.Original` holds the saved document on
an update, and `ctx.Candidate()` combines the two.

This hook fills a missing excerpt from the first 30 words of the body. It must run before
validation: in `BeforeChange`, the required `excerpt` would already have been rejected.

```go title="content/excerpt.go" focus={15-17,23-24}
package content

import (
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func fillExcerpt(ctx ridu.HookContext) error {
	if ctx.Operation != operation.Create {
		return nil
	}
	if excerpt, _ := ctx.Data["excerpt"].StringValue(); excerpt != "" {
		return nil // The author wrote one.
	}
	body, _ := ctx.Data["body"].StringValue()
	words := strings.Fields(body)
	if len(words) > 30 {
		words = words[:30]
	}
	// Required() is checked after this hook, so the excerpt passes.
	ctx.Data["excerpt"] = store.String(strings.Join(words, " "))
	return nil
}
```

Creating an article without an excerpt saves the first 30 words of its body as the excerpt. An
excerpt the author wrote is kept.

## BeforeChange {#before-change}

<span id="derive-values"></span>

Runs after the built-in checks and before the document is saved. It runs for create, duplicate,
update, publish, and unpublish. Use it to calculate values or record who made a change.

**Receives:** `ctx.Data` with the values to save, `ctx.Original` on an update, and `ctx.Actor`.
On an update, `ctx.Data` holds only the fields that were sent. Read `ctx.Candidate()` when a rule
needs a field whether or not it changed:

```go
// A partial update may send only one of the two dates.
values := ctx.Candidate()
start, _ := values["startsAt"].StringValue()
end, _ := values["endsAt"].StringValue()
if end != "" && end < start {
	return ridu.Reject("The event must end after it starts")
}
```

```go title="content/last_editor.go" focus={19-20}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func recordLastEditor(ctx ridu.HookContext) error {
	// A nil actor means the request is anonymous.
	if ctx.Actor == nil {
		return nil
	}
	// Limit editor tracking to creates and ordinary updates.
	if ctx.Operation != operation.Create &&
		ctx.Operation != operation.Update {
		return nil
	}
	// Mutate Data entries to include them in the same save.
	ctx.Data["lastEditedBy"] = store.String(ctx.Actor.ID)
	return nil
}
```

Save an article while signed in, and `lastEditedBy` stores your user ID. Anonymous saves leave it
unchanged. Ridu checks values changed here again, and your
[custom `.Validate(...)` rules](../fields/validation.md) run after this hook.

## BeforeOperation {#before-operation}

Runs just before Ridu calls the database, for every operation, including reads and deletes. On a
save, it runs after every `BeforeChange` hook, including hooks added by plugins, so it sees the
final values.

**Receives:** the same values as `BeforeChange` on a save. On a read, `ctx.Data` is empty.

```go title="content/word_count.go" focus={12-17,22-24}
package content

import (
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func countWords(ctx ridu.HookContext) error {
	switch ctx.Operation {
	case operation.Create, operation.Duplicate, operation.Update,
		operation.Publish, operation.Unpublish:
	default:
		return nil // BeforeOperation also runs for reads and deletes.
	}
	body, sent := ctx.Data["body"].StringValue()
	if !sent {
		return nil // This update does not change the body.
	}
	// Every BeforeChange hook has run, so this is the final body.
	words := len(strings.Fields(body))
	ctx.Data["wordCount"] = store.Number(float64(words))
	return nil
}
```

Saving an article with a 40-word body stores `40` in `wordCount`. An update that changes only the
title keeps the stored count.

## AfterChange {#after-change}

Runs after the document is saved but before the transaction commits. It runs for create,
duplicate, update, publish, and unpublish. Use it for related database writes that must succeed
or fail together with the document.

**Receives:** `ctx.Document`, the saved document, and `ctx.Original` on an update.

This hook adds an audit entry for every save. Add `AuditLog` to `Config.Collections` too:

```go title="content/audit_log.go" focus={21-23,29-34,36-37}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

var AuditLog = ridu.Collection{
	Slug: "audit-log",
	Fields: field.Fields{
		field.Text("document").Required(),
		field.Text("operation").Required(),
	},
}

func writeAuditEntry(ctx ridu.HookContext) error {
	if ctx.Document == nil {
		return nil
	}
	// Passing ctx.Context saves both documents in one transaction.
	_, err := ctx.Local.Create(
		ctx.Context,
		"audit-log",
		store.Values{
			"document":  store.String(ctx.Document.ID),
			"operation": store.String(string(ctx.Operation)),
		},
		// Local API calls do not inherit the user or locale.
		ridu.MutationOptions{
			Actor:           ctx.Actor,
			ActorCollection: ctx.ActorCollection,
			Locale:          ctx.Locale,
		},
	)
	// Returning the error also rolls back the original save.
	return err
}
```

Each save adds an audit entry with the article's ID and the operation. If the audit entry cannot
be saved, the article is not saved either. The audit entry runs its own access rules, validation,
and hooks. Changing `ctx.Document.Values` here changes only the response; use `BeforeChange` to
change what is saved.

## AfterOperation {#after-operation}

Runs after the database call for every operation, including reads and deletes, before the
response is prepared and the transaction commits. An error still rolls back a save. On a list,
`ctx.Document` is `nil`.

Most work fits a more specific hook: `AfterChange` for saves, `AfterDelete` for deletes, and
`AfterRead` for responses. Use `AfterOperation` when one hook must follow every kind of
operation, and check `ctx.Operation` inside it.

## BeforeRead {#before-read}

Runs before a find or list reads from the database. No document has been loaded yet, and
`ctx.Data` is empty. Use it for work that must happen before any read, such as recording a metric.

To control which documents someone may read, use [access control](../access-control.md) instead.
An access rule filters the database query itself, so it also applies to counts and pagination.

## AfterRead {#after-read}

Runs for each document Ridu returns, before removing fields the caller is not allowed to read.
That includes reads and the responses to creates, updates, and deletes. Use it to change the
response without changing what is stored.

**Receives:** `ctx.Document`. Change `ctx.Document.Values` to change the response.

```go title="content/meta_title.go" focus={10-12}
package content

import "github.com/riducms/ridu"

func fillMetaTitle(ctx ridu.HookContext) error {
	values := ctx.Document.Values
	if metaTitle, _ := values["metaTitle"].StringValue(); metaTitle != "" {
		return nil
	}
	// This changes the response only. The stored metaTitle stays
	// empty, so it keeps following the title when the title changes.
	values["metaTitle"] = values["title"]
	return nil
}
```

An article with an empty `metaTitle` is returned with its title in that field. Filters and sorting
still use the stored, empty value. Because a list runs this hook for every document, avoid slow
lookups here.

## BeforeDuplicate {#before-duplicate}

Runs when a document is duplicated: after Ridu copies the original and before `BeforeValidate`.
Use it to change values that the copy should not keep.

**Receives:** `ctx.Data` with the copied values, and `ctx.Original`, the document being copied.

```go title="content/mark_copy.go" focus={9-14}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

func markCopy(ctx ridu.HookContext) error {
	// Data holds the values copied from the original article.
	if title, ok := ctx.Data["title"].StringValue(); ok {
		ctx.Data["title"] = store.String(title + " (copy)")
	}
	// The copy should not replace the original on the homepage.
	ctx.Data["featured"] = store.Boolean(false)
	return nil
}
```

Duplicating a featured article titled “Launch” creates “Launch (copy)”, which is not featured.
Ridu gives a copied [`field.Slug`](https://riducms.com/docs/fields/slug/) the next free value, such as
`launch-copy`. Other unique fields must be cleared or changed in the copy, or the duplicate fails
with a `unique` error on that field; the
[field `BeforeDuplicate` example](./fields.md#before-duplicate) clears one.

## BeforeDelete {#before-delete}

Runs after Ridu loads the document and before deleting it. It runs both when a document moves to
the trash and when it is deleted permanently. Return an error to stop the delete.

**Receives:** `ctx.Original`, the document about to be deleted.

```go title="content/keep_featured.go" focus={10-11,13-20}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
)

func keepFeatured(ctx ridu.HookContext) error {
	// Original is the saved article that is about to be deleted.
	featured, _ := ctx.Original.Values["featured"].BooleanValue()
	if featured {
		// Reject shows this message to the editor and stops the delete.
		return ridu.Reject(
			"Remove the article from the homepage first.",
			operation.Issue{
				Code:    "featured",
				Message: "Untick Featured, then delete the article.",
				Target:  operation.At("featured"),
			},
		)
	}
	return nil
}
```

Deleting a featured article stops with “Remove the article from the homepage first.” The admin
shows that message and marks the Featured field with the issue's message. REST, the SDK, and the
local API return a `rejected` error (HTTP 422) with the same message and issue. To decide who may
delete documents at all, use a `Delete` [access rule](../access-control.md).

## AfterDelete {#after-delete}

Runs after the document is deleted but before the transaction commits. An error restores the
document. Use it for related cleanup that must happen together with the delete.

**Receives:** `ctx.Document` and `ctx.Original`, both holding the deleted document.

The audit hook from [AfterChange](#after-change) works here too, recording deletes as well as
saves:

```go
Hooks: ridu.CollectionHooks{
	AfterChange: []ridu.Hook{writeAuditEntry},
	AfterDelete: []ridu.Hook{writeAuditEntry},
},
```

Restoring a document from the trash does not run the delete hooks or `AfterChange`.

## AfterError {#after-error}

Runs when an operation on this collection fails. Use it to log the failure or report it to an
error tracker.

**Receives:** `ctx.Error`, the original failure, and `ctx.Operation`.

```go title="content/log_failures.go" focus={12,14-15}
package content

import (
	"log"

	"github.com/riducms/ridu"
)

func logFailure(ctx ridu.HookContext) error {
	log.Printf(
		"Ridu %s failed (collection=%s global=%s): %v",
		ctx.Operation, ctx.CollectionID, ctx.GlobalID, ctx.Error,
	)
	// Finish logging; the original failure still reaches the caller.
	return nil
}
```

A blocked delete of a featured article logs a line such as
`Ridu delete failed (collection=… global=): Remove the article from the homepage first.` Returning `nil` does not
make the operation succeed. To log failures from every collection and global in one place, use
[application error hooks](../hooks.md#application-hooks).

## AfterCommit {#after-commit}

Runs after the transaction commits, for every change, including deletes and restores. It never
runs after a read. Use it for work that should happen only once a change is saved, such as sending
email, calling a webhook, or clearing a cache.

**Receives:** `ctx.Document`, and `ctx.Original` on updates and deletes.

```go title="content/purge_cache.go" focus={16-19}
package content

import (
	"context"

	"github.com/riducms/ridu"
)

// purgeCache stands in for your CDN client's purge call.
var purgeCache = func(ctx context.Context, path string) error {
	return nil
}

func purgeArticleCache(ctx ridu.HookContext) error {
	// AfterCommit runs after every committed change, including deletes.
	slug, _ := ctx.Document.Values["slug"].StringValue()
	// The article is already saved. An error here is reported to the
	// caller, but it cannot undo the save.
	return purgeCache(ctx.Context, "/articles/"+slug)
}
```

Saving or deleting the article `launch` purges `/articles/launch`; reading it purges nothing. If
the purge fails, the article stays saved and the caller receives an error marked as committed.
[Send notifications after a successful save](./transactions-and-errors.md#after-commit)
shows a webhook with a timeout, and how to retry failed deliveries.

## Where validation and field hooks fit {#save-order}

For a create or update, Ridu runs these steps in order:

1. Check collection access. On an update, load the saved document.
2. Run collection `BeforeValidate` hooks, then field `BeforeValidate` hooks.
3. Apply [default values](../fields/defaults.md), combine an update with the saved values, and
   run built-in checks such as `Required()`.
4. Run collection hooks, then field hooks, for `BeforeChange` and then for `BeforeOperation`.
5. Check the values and field permissions again, and run your custom `.Validate(...)` rules.
6. Save the document inside the database transaction.
7. Run collection hooks, then field hooks, for `AfterChange` and then for `AfterOperation`.
8. Prepare the response. Run collection `AfterRead`, then field `AfterRead`, then remove fields the
   caller cannot read.
9. Commit the transaction. Run field `AfterCommit` hooks, then collection `AfterCommit` hooks.

A duplicate runs `BeforeDuplicate` before step 2. A failed step stops the operation, and the later
steps do not run.
