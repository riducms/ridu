<!-- Generated from website/src/content/docs/hooks/context.md by scripts/sync-agent-docs.ts. -->

# Hook context

Ridu passes information about the current operation to each hook. This argument is usually named
`ctx`. Collection and global hooks receive one `ridu.HookContext`; field hooks receive a field
context and a separate value argument.

## Read the document and signed-in user {#hook-context}

Collection and global hooks receive [`ridu.HookContext`](https://riducms.com/reference/ridu/hook-context/).
Use it to read the signed-in user, inspect the previous document, or change values before a save:

| Value                       | What it contains                                                             |
| --------------------------- | ---------------------------------------------------------------------------- |
| `Operation`                 | The create, duplicate, read, update, delete, publish, or unpublish operation |
| `Actor`                     | The authenticated document, or `nil` for an anonymous operation              |
| `ActorCollection`           | The auth collection that owns the signed-in user                             |
| `CollectionID` / `GlobalID` | The stable resource ID; only the matching one is set                         |
| `Data`                      | Values you can change before the document is saved                           |
| `Document`                  | The document being returned, once it is available                            |
| `Original`                  | The saved document before an update or delete                                |
| `Context`                   | Request cancellation and deadline; pass it to `ctx.Local` calls              |
| `Local`                     | The local API for reading or writing related documents                       |
| `Error`                     | The original failure while `AfterError` runs                                 |
| `Locale` / `AllLocales`     | The locale view selected for this operation                                  |

Values in `Data` use [`store.Value`](https://riducms.com/reference/store/value/). Read a string with
[`StringValue()`](https://riducms.com/reference/store/value-string-value/) and write one with
[`store.String(...)`](https://riducms.com/reference/store/string/). [Documents and values](../go-packages/store.md)
explains the complete pattern, including missing values, nested data, and changes to a returned
document. For collection IDs, document IDs, and locales, read
[Schema and identifiers](../go-packages/schema.md#identifiers).

### Change stored values or response values {#stored-and-returned-values}

Before saving, update entries in `ctx.Data`. After checking that `ctx.Actor` is not `nil`, for
example, set `ctx.Data["lastEditedBy"] = store.String(ctx.Actor.ID)`. Returning `nil` keeps those changes and
continues the operation. The hook returns only an error; it does not return a replacement document.
Assigning a new map to `ctx.Data` itself will not replace Ridu's map.

After storage, `ctx.Document` contains the returned document. Changes to
`ctx.Document.Values` in `AfterChange`, `AfterOperation`, or `AfterRead` affect the response only;
they do not update the saved record. Use a before-save hook for a stored change.

`ctx.Original` contains the saved document before an update or delete. It is `nil` for a create,
and `ctx.Document` is unavailable before a result exists. General hooks on a list do not receive
a single result document; use `AfterRead` to work with each document.

`ctx.Operation` names the original operation. An `AfterRead` hook preparing a newly created post
still sees `operation.Create`, not `operation.Read`.

`CollectionID` and `GlobalID` are Ridu's stable identifiers, not the slugs in your config.
Use `Locale` and `AllLocales` to understand the values supplied to this hook. Write hooks use
the write locale; `AfterRead` uses the response locale, which may include fallback text.

## Work with a field context {#field-context}

A [field hook](./fields.md) receives `ctx` plus a separate `operation.Value[T]` for its
own value. The context contains read-only views:

- `ctx.Siblings` contains this field and other fields in the same object or row.
- `ctx.Root` contains the top-level fields.
- `ctx.Prior` contains the previously saved version of the same object or row.

In a field transform, return `operation.Replace(...)` to change the field. Editing a map or slice read from these
views does not change the document. Unlike a collection hook's `Actor` pointer, a field context
uses `ctx.Actor.ID` for the user ID; an empty ID means the request is anonymous.

See [Using other field values](../fields/callback-values.md) for complete examples covering
nested rows, previous values, translations, and related-document lookups.

## Pass the user and locale to related operations {#nested-operations}

A collection or global hook's `ctx.Local` is the ordinary local API. When a nested operation
should use the same user, pass `Actor: ctx.Actor` and `ActorCollection: ctx.ActorCollection` in
its options. Pass `Locale: ctx.Locale` when it should use the same content language. These
options are not filled in automatically; a nil actor makes an anonymous request.

Pass `ctx.Context` to share the active transaction and request cancellation. See
[Save related documents together](./transactions-and-errors.md#nested-operations) for
the complete audit-log example and rollback behavior.

The field context's `ctx.Local.FindByID` reader already carries the user, auth collection, exact
locale, and transaction. It only supports reads; use a collection or global hook for related
writes.

## Avoid triggering the same hook again {#recursion}

A local API call runs the target collection's hooks too. If a posts hook writes an audit entry,
attach it to posts only; attaching it to the audit collection would make each log entry create
another log entry. Similarly, updating a post from its own `AfterChange` hook can start a loop.

Ridu rejects excessive recursion, but design the hook so it does not call itself. Change
`ctx.Data` before saving when the change belongs to the same document. Use a separate collection
when the hook creates a related record, and check what that collection's hooks will do.

Calls made from `AfterCommit` use new transactions and can trigger hooks again. Keep notification
hooks focused on delivery instead of updating the same document after every save.

## Read unsaved values during a live check {#advisory-context}

A `.LiveValidate(...)` callback receives `operation.LiveValidationContext`. It has the unsaved
form values and saved values for comparison, but no writable `ctx.Data`. Defaults and save hooks
have not run, and related lookups through `ctx.Local` are read-only.

See [Live server validation](../fields/live-validation.md) for its context properties and lookup
limits. These checks run separately from document and field hooks.
