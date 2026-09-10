<!-- Generated from website/src/content/docs/hooks/globals.md by scripts/sync-agent-docs.ts. -->

# Global hooks

A global stores one shared document, such as site settings, navigation, or a homepage. Global
hooks let you change its values before saving, adjust a read response, or react after a save.
They run for the admin, REST, the SDK, and the local Go API.

Globals use [`ridu.CollectionHooks`](https://riducms.com/reference/ridu/collection-hooks/) and
[`ridu.HookContext`](https://riducms.com/reference/ridu/hook-context/), the same types as collection hooks. You can
reuse a helper on both when it does not depend on collection-specific fields or operations.

## Record who last updated your settings {#global-hooks}

Create the [`recordLastEditor` helper](./collections.md#derive-values) in
`content/last_editor.go`, then add this global in the same package:

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

Add `SiteSettings` to `Config.Globals`. Save the settings while signed in: `lastEditedBy` stores
your user ID. Anonymous writes leave it unchanged.

**Even the first save is an update.** A global already has its identity in your configuration;
Ridu uses `operation.Update` when saving it for the first time. The helper's update check
therefore covers both the first save and later edits.

## Choose when your hook runs {#lifecycle}

Add functions to the relevant list in `SiteSettings.Hooks`. Ridu runs functions in declaration
order and waits for each one to finish.

| Hook              | When it runs                                    | Use it to                                    |
| ----------------- | ----------------------------------------------- | -------------------------------------------- |
| `BeforeValidate`  | Before built-in field checks                    | Clean up submitted settings                  |
| `BeforeChange`    | After built-in checks, before custom validators | Calculate values or record the editor        |
| `BeforeOperation` | Before final validation and storage             | Inspect the operation or make a final change |
| `BeforeRead`      | Before reading the global                       | Prepare data needed while reading            |
| `AfterChange`     | After storage, before commit                    | Save related records in the same transaction |
| `AfterOperation`  | After the operation, before commit              | Run follow-up database work                  |
| `AfterRead`       | Before unreadable fields are removed            | Change the returned settings                 |
| `AfterCommit`     | After the transaction commits                   | Send a notification or refresh a cache       |
| `AfterError`      | When an operation fails                         | Log the failure or record a metric           |

`BeforeChange` and `AfterChange` run for updates, publishing, and unpublishing. `BeforeValidate`,
`BeforeOperation`, `AfterOperation`, and `AfterCommit` can also run for reads. Check
`ctx.Operation` if your hook should act only on a save. `AfterRead` also prepares update
responses; a failure there can still roll back an update that has not committed.

Globals do not support duplication or deletion. Ridu rejects `BeforeDuplicate`, `BeforeDelete`,
and `AfterDelete` hooks configured on a global.

The [save order](./collections.md#save-order) is the same as a collection update, with
global hooks in place of collection hooks. Global hooks run before field hooks at each stage,
except `AfterCommit`, where field hooks run first. Custom `.Validate(...)` callbacks run after
the before-save hooks.

## Read the global and previous settings {#hook-context}

Use `ctx.GlobalID` to identify the global. It contains its stable resource ID; `ctx.CollectionID`
is empty. `ctx.Operation` is `operation.Read`, `operation.Update`, `operation.Publish`, or
`operation.Unpublish` as appropriate. `ctx.Original` is `nil` before the first save and contains
the previous settings on later updates.

Change entries in `ctx.Data` before saving to update stored settings. Changes to
`ctx.Document.Values` after storage affect only the response. The signed-in user, locale, local
API, and error information work as described in [Hook context](./context.md).

## Save related records and send notifications {#next-steps}

Use `AfterChange` for a related database write that must succeed with the settings update. Use
`AfterCommit` for an email, webhook, or cache refresh that should happen only after a successful
commit. A failed after-commit hook leaves the settings saved.

Follow [Transactions and errors](./transactions-and-errors.md) to share a transaction,
forward the user and locale to related operations, and handle failures.
