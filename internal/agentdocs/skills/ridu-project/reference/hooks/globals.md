<!-- Generated from website/src/content/docs/hooks/globals.md by scripts/sync-agent-docs.ts. -->

# Global hooks

A global stores one shared document, such as site settings, navigation, or a homepage. Global
hooks work like [collection hooks](./collections.md): the same `ridu.CollectionHooks`
lists, the same `ridu.HookContext`, and the same order. A helper written for a collection works on
a global when it does not depend on collection-only operations.

## Add hooks to a global {#global-hooks}

This global reuses the [`recordLastEditor` helper](./collections.md#before-change) from
the collection hooks page:

```go title="content/globals.go" focus={14-17}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var SiteSettings = ridu.Global{
	Slug: "site-settings",
	Fields: field.Fields{
		field.Text("siteName"),
		field.Text("lastEditedBy"),
	},
	Hooks: ridu.CollectionHooks{
		// The same helper works: global saves use operation.Update.
		BeforeChange: []ridu.Hook{recordLastEditor},
	},
}
```

Add `SiteSettings` to `Config.Globals`. Save the settings while signed in, and `lastEditedBy`
stores your user ID.

## Available hooks {#lifecycle}

A global can be read and updated, but not created, duplicated, or deleted. These hooks are
available, and each works as described on the collection hooks page:

| Hook                                                           | Runs                                                         |
| -------------------------------------------------------------- | ------------------------------------------------------------ |
| [`BeforeValidate`](./collections.md#before-validate)   | Before built-in field checks. Also runs for reads.           |
| [`BeforeChange`](./collections.md#before-change)       | Before saving an update, publish, or unpublish.              |
| [`BeforeOperation`](./collections.md#before-operation) | Just before the database call. Also runs for reads.          |
| [`BeforeRead`](./collections.md#before-read)           | Before the global is read.                                   |
| [`AfterChange`](./collections.md#after-change)         | After saving, before commit.                                 |
| [`AfterOperation`](./collections.md#after-operation)   | After the database call, before commit. Also runs for reads. |
| [`AfterRead`](./collections.md#after-read)             | Before the global is returned, including after an update.    |
| [`AfterError`](./collections.md#after-error)           | When an operation fails.                                     |
| [`AfterCommit`](./collections.md#after-commit)         | After the transaction commits. Also runs for reads.          |

Ridu rejects a global configured with `BeforeDuplicate`, `BeforeDelete`, or `AfterDelete` hooks.
Global hooks run before field hooks at each stage, except `AfterCommit`, where field hooks run
first.

## How global hooks differ {#hook-context}

- **Even the first save is an update.** A global exists as soon as it is configured, so saving it
  for the first time uses `operation.Update`. `ctx.Operation` is `operation.Read`,
  `operation.Update`, `operation.Publish`, or `operation.Unpublish`.
- **`ctx.Original` is `nil` before the first save.** On later updates, it holds the previous
  settings.
- **`ctx.GlobalID` identifies the global.** It holds Ridu's stable ID for the global, and
  `ctx.CollectionID` is empty.

Everything else in `ctx`, including `ctx.Data`, `ctx.Document`, `ctx.Actor`, and `ctx.Local`,
works as described in [hook arguments](./collections.md#arguments).

## Save related records and send notifications {#next-steps}

Use `AfterChange` for a related database write that must be saved together with the settings, and
`AfterCommit` for an email, webhook, or cache purge after a successful save. A failed
`AfterCommit` hook leaves the settings saved. [Transactions and errors](./transactions-and-errors.md)
covers both in detail.
