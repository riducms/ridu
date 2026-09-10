---
title: 'Collection hooks'
description: 'Change document values before saving and choose when collection hooks run during creates, reads, updates, and deletes.'
product: core
eyebrow: 'Hooks'
order: 81
navigation:
  section: 'Extend Ridu'
  parent: hooks
  order: 10
  title: 'Collection hooks'
---

Collection hooks run Go functions when someone creates, reads, updates, or deletes a document.
Use them when your code needs the whole document: record the editor, calculate several values,
or save a related record. They run for the admin, REST, the SDK, and the local Go API.

Add hooks to the collection's [`Hooks`](/reference/ridu/collection-hooks/) property. Each function
receives [`ridu.HookContext`](/reference/ridu/hook-context/) and returns an error. Returning `nil`
lets the operation continue. Ridu waits for each hook before moving to the next stage.

## Record who edited a document {#derive-values}

Use `BeforeChange` to change document values before saving. This helper records the signed-in
user's ID on creates and updates:

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

Add a field to store the ID and register the helper on your collection:

```go title="content/articles.go" focus={14-17}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Articles = ridu.Collection{
	Slug: "articles",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Text("lastEditedBy"),
	},
	Hooks: ridu.CollectionHooks{
		// Run before saving so the editor ID is stored with the article.
		BeforeChange: []ridu.Hook{recordLastEditor},
	},
}
```

Add `Articles` to `Config.Collections`. Create or edit an article while signed in: the saved
`lastEditedBy` value should be your user ID. Anonymous writes leave it unchanged. To include
duplication, publishing, or unpublishing, add those operation values to the helper's condition.

`ctx.Data` contains the values being saved. Change its entries to update the document; returning
`nil` keeps those changes. Use `ctx.Original` to compare with the saved document, or `ctx.Actor`
to read the signed-in user. See [Hook context](/docs/hooks/context/) for the values available at
each stage and the difference between changing stored values and changing a response.

## Choose when your hook runs {#lifecycle}

Hook names describe when they run. For example, `BeforeValidate` runs before Ridu checks field
values, and `AfterCommit` runs once the database has successfully saved the operation. If you add
several hooks to the same list, Ridu runs them in the order you declare them.

| Hook                          | When it runs                                    | Use it to                                           |
| ----------------------------- | ----------------------------------------------- | --------------------------------------------------- |
| `BeforeDuplicate`             | After the source is copied                      | Clear values that should not be copied              |
| `BeforeValidate`              | Before built-in field checks                    | Trim whitespace or normalize submitted input        |
| `BeforeChange`                | After built-in checks, before custom validators | Calculate values or record the editor               |
| `BeforeOperation`             | Before final validation and storage             | Make a final change or inspect an operation         |
| `BeforeRead`                  | Before documents are read                       | Prepare data needed while reading                   |
| `BeforeDelete`                | After the original document is loaded           | Clean up related data before deletion               |
| `AfterChange` / `AfterDelete` | After storage, before the transaction commits   | Write related data that must succeed together       |
| `AfterRead`                   | Before unreadable fields are removed            | Change values returned to the caller                |
| `AfterOperation`              | After the operation, before commit              | Run follow-up database work                         |
| `AfterError`                  | When an operation fails                         | Log the failure or record a metric                  |
| `AfterCommit`                 | After the transaction commits                   | Send email, call webhooks, or update a search index |

`BeforeDuplicate`, `BeforeChange`, and `AfterChange` are specific to changes. `BeforeDelete`
and `AfterDelete` run for deletion, including trash and permanent deletion. Restoring a trashed
document does not run `AfterChange`.

`BeforeValidate`, `BeforeOperation`, `AfterOperation`, and `AfterCommit` are shared stages: they
can run for reads and deletes too. Check `ctx.Operation` when a hook should work only on saves.
`AfterRead` runs whenever Ridu prepares a returned document, including create, update, and delete
responses. A list runs it for each returned document.

### The order of a save {#save-order}

For an ordinary create or update, Ridu runs this sequence:

1. Check collection access and load the saved document when updating.
2. Run the collection's `BeforeValidate` hooks, then the fields' `BeforeValidate` hooks.
3. Apply [field defaults](/docs/fields/defaults/) to omitted values in new documents, objects, or
   rows, combine an update with saved values, and run built-in checks.
4. Run collection hooks, then field hooks, for `BeforeChange` and then `BeforeOperation`.
5. Recheck values and field permissions, run custom `.Validate(...)` rules, and check relationships.
6. Save the document inside the database transaction.
7. Run collection hooks, then field hooks, for `AfterChange` and then `AfterOperation`.
8. Prepare the response, including computed fields. Run collection `AfterRead`, then field
   `AfterRead`, and remove values the caller cannot read.
9. Commit the transaction, then run field `AfterCommit` hooks followed by collection `AfterCommit`
   hooks.

A duplicate adds `BeforeDuplicate` before `BeforeValidate`, after copying the source. A publish
or unpublish also runs the change hooks, with its own `ctx.Operation` value. A failed hook stops
that operation before later stages run; [after-commit failures](/docs/hooks/transactions-and-errors/#after-commit) are different
because the database has already committed.

The important distinction is between built-in checks and your custom validators. `BeforeChange`
receives values that have passed the first built-in checks, but your `.Validate(...)` rules run
**after** write hooks. Put a rule that must accept or reject the final value in
[custom validation](/docs/fields/validation/).

For hooks in the same list, declaration order is execution order. Do not rely on the order of
unrelated fields to coordinate several changes. Put that work in one collection hook.

## Save related records or notify another service {#next-steps}

An `AfterChange` hook still runs inside the transaction. Use it for database changes that must
succeed together, such as an audit entry. Use `AfterCommit` for email, webhooks, or a search index:
the document has been saved by then, so a later failure cannot undo it.

[Transactions and errors](/docs/hooks/transactions-and-errors/) shows how to save related
records, handle failures, and send notifications after commit. For a change to just one field,
see [Field hooks](/docs/hooks/fields/).
