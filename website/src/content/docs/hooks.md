---
title: 'Hooks'
description: 'Run Go code when content changes. Choose field, collection, or global hooks, and learn when to save related data or notify another service.'
product: core
eyebrow: 'Runtime'
order: 80
navigation:
  section: 'Extend Ridu'
  order: 20
  title: 'Hooks'
---

Hooks are Go functions that run when Ridu creates, reads, updates, or deletes content.
Use them to clean up a field value, record who edited a document, save an audit entry,
or send a notification after a successful save.

They run on the server for requests from the admin, REST, the SDK, and the local Go API.

## Types of hooks {#choose-a-hook}

| Hook type                                    | Attach it to                                  | Use it to                                                          |
| -------------------------------------------- | --------------------------------------------- | ------------------------------------------------------------------ |
| [Collection hooks](/docs/hooks/collections/) | A collection's `Hooks`                        | Work with a whole document: record its editor, save an audit entry |
| [Global hooks](/docs/hooks/globals/)         | A global's `Hooks`                            | Do the same for a single document, such as site settings           |
| [Field hooks](/docs/hooks/fields/)           | A field's `.Hooks(...)` and `.AfterRead(...)` | Change one field's value wherever the field is used                |
| [Application hooks](#application-hooks)      | `Config.Hooks`                                | Log failures from every collection and global in one place         |

A collection hook and a field hook can sit side by side:

```go
var Products = ridu.Collection{
	Slug: "products",
	Fields: field.Fields{
		// A field hook changes one value.
		field.Text("sku").Hooks(field.Hooks[string]{
			BeforeChange: []field.Transform[string]{uppercaseSKU},
		}),
		field.Text("lastEditedBy"),
	},
	// A collection hook works with the whole document.
	Hooks: ridu.CollectionHooks{
		BeforeChange: []ridu.Hook{recordLastEditor},
	},
}
```

A field hook follows the field wherever you reuse it, including inside groups, arrays, and blocks.
Use a collection or global hook when several fields need to change together, or when the hook
saves other documents. [Hook context](/docs/hooks/context/) compares what each kind of hook
receives.

## Before or after saving? {#lifecycle}

- **Before saving:** clean up or calculate values with `BeforeValidate` or `BeforeChange`.
- **After saving, before commit:** use `AfterChange` for related database writes that must
  succeed or roll back together with the document.
- **After commit:** use `AfterCommit` to notify another service once the document is saved.
- **When preparing a response:** use `AfterRead` to change what the caller receives without
  changing what is stored. A field registers it with its `.AfterRead(...)` method, because a
  field's returned value can have a different type from its stored value.

Field hooks do not have `BeforeRead` or `AfterError`. The
[field hook arguments](/docs/hooks/fields/#arguments) and
[collection hook table](/docs/hooks/collections/#lifecycle) show what runs for each operation.

<span id="save-order"></span>

See [where validation and field hooks fit](/docs/hooks/collections/#save-order) for the exact
order of a save, including validation, permissions, and field hooks. `BeforeOperation` and
`AfterOperation` also run for reads; check `ctx.Operation` when your code should run only on saves.

Ridu waits for each hook to finish. Use [tasks](/docs/tasks/) for work that needs background
processing or retries, and read [after-commit behavior](/docs/hooks/transactions-and-errors/#after-commit)
before sending email or calling a webhook.

## Find an example {#examples}

- [Fill a required field before validation](/docs/hooks/collections/#before-validate).
- <span id="normalize-input"></span>[Trim a field before validation](/docs/hooks/fields/#before-validate).
- <span id="derive-values"></span>[Record who edited a document](/docs/hooks/collections/#before-change).
- <span id="typed-field-hooks"></span>[Change a typed field before saving](/docs/hooks/fields/#before-change).
- <span id="read-hooks"></span>[Format a returned value](/docs/hooks/fields/#after-read).
- [Clear a unique value when a document is duplicated](/docs/hooks/fields/#before-duplicate).
- [Stop a delete and tell the editor why](/docs/hooks/collections/#before-delete).
- <span id="nested-operations"></span>[Save an audit entry in the same transaction](/docs/hooks/collections/#after-change).
- [Log a field change after it is saved](/docs/hooks/fields/#after-commit).
- <span id="global-hooks"></span>[Add a hook to site settings](/docs/hooks/globals/#global-hooks).
- <span id="hook-context"></span><span id="stored-and-returned-values"></span>[Read and change hook values](/docs/hooks/context/).
- <span id="recursion"></span>[Avoid triggering the same hook again](/docs/hooks/context/#recursion).
- <span id="after-commit"></span><span id="after-commit-errors"></span>[Send a notification after a save](/docs/hooks/transactions-and-errors/#after-commit).
- <span id="handle-errors"></span><span id="hook-errors"></span>[Log and handle failures](/docs/hooks/collections/#after-error).
- <span id="performance"></span>[Keep frequently run hooks fast](/docs/hooks/transactions-and-errors/#performance).

## Handle errors across the application {#application-hooks}

`Config.Hooks.AfterError` runs for failures across the application, including failures that happen
before Ridu knows which collection or global is involved. Use it for a central logger or error
reporting service:

```go
config.Hooks.AfterError = []ridu.Hook{logFailure}
```

Collection and global `AfterError` hooks run before the application's error hooks. See
[the `AfterError` example](/docs/hooks/collections/#after-error) for a logging hook, and
[Log a failed operation](/docs/hooks/transactions-and-errors/#handle-errors) for what happens to
a failed write.
