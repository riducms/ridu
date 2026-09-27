---
title: 'Hook context'
description: 'Read the user, current and saved values, and request information available inside a hook.'
product: core
eyebrow: 'Hooks'
order: 84
navigation:
  section: 'Extend Ridu'
  parent: hooks
  order: 40
  title: 'Hook context'
---

Every hook receives an argument usually named `ctx` that describes the current operation.
Collection and global hooks receive a `ridu.HookContext`, which holds the whole document. Field
hooks receive an `operation.Context` and their own value as a separate argument.

## Collection and field contexts {#hook-context}

The two contexts answer the same questions in different ways:

| To…                                   | In a collection or global hook                 | In a field hook                                  |
| ------------------------------------- | ---------------------------------------------- | ------------------------------------------------ |
| Check what is happening               | `ctx.Operation`                                | `ctx.Operation`                                  |
| Identify the signed-in user           | `ctx.Actor`, a document; `nil` when anonymous  | `ctx.Actor.ID`; empty when anonymous             |
| Read the values being saved           | `ctx.Data`                                     | The `value` argument, `ctx.Siblings`, `ctx.Root` |
| Read the saved values before a change | `ctx.Original`                                 | `ctx.Prior`, for this field's group or row       |
| Change what is saved                  | Set entries in `ctx.Data`                      | Return `operation.Replace(...)`                  |
| Change the response                   | Set entries in `ctx.Document.Values`           | Return a change from an `.AfterRead(...)` hook   |
| Read another document                 | `ctx.Local.Find(...)`                          | `ctx.Local.FindByID(...)`                        |
| Save another document                 | `ctx.Local.Create(...)`, `Update`, or `Delete` | Not available; use a collection hook             |
| Check the content locale              | `ctx.Locale`                                   | `ctx.Locale`                                     |

The complete lists are in [collection hook arguments](/docs/hooks/collections/#arguments) and
[field hook arguments](/docs/hooks/fields/#arguments).

### Change stored values or response values {#stored-and-returned-values}

In a collection hook, change what is saved by setting entries in `ctx.Data` before the save:
`ctx.Data["lastEditedBy"] = store.String(ctx.Actor.ID)`. The hook returns only an error, never a
replacement document, and assigning a new map to `ctx.Data` does not replace Ridu's map.

After the save, `ctx.Document` holds the document Ridu returns. Changing `ctx.Document.Values`
in `AfterChange`, `AfterOperation`, or `AfterRead` changes the response only; the saved document
stays the same.

`ctx.Operation` names the whole operation, not the current hook. An `AfterRead` hook preparing
the response to a create sees `operation.Create`, not `operation.Read`. Write hooks see the
locale being written; `AfterRead` sees the response locale, whose values may include fallback
text from another locale.

## Read other fields from a field hook {#field-context}

A field hook receives read-only views of the values around it:

- `ctx.Siblings` holds this field and the other fields in the same group or array row.
- `ctx.Root` holds the document's top-level fields.
- `ctx.Prior` holds the saved version of the same group or row before this operation.

```go
title, _ := ctx.Siblings.String("title")
price, _ := ctx.Siblings.Get("price").NumberValue()
previousTitle, _ := ctx.Prior.String("title")
```

Changing a map or list read from these views does not change the document. Return
`operation.Replace(...)` to change the hook's own field. See
[Using other field values](/docs/fields/callback-values/) for nested rows, translations, and
related-document lookups.

## Pass the user and locale to related operations {#nested-operations}

A collection or global hook's `ctx.Local` is the ordinary local API. When a nested operation
should use the same user, pass `Actor: ctx.Actor` and `ActorCollection: ctx.ActorCollection` in
its options. Pass `Locale: ctx.Locale` when it should use the same content language. These
options are not filled in automatically; a nil actor makes an anonymous request.

Pass `ctx.Context` to share the active transaction and request cancellation. The
[`AfterChange` audit example](/docs/hooks/collections/#after-change) passes all of these, and
[Save related documents together](/docs/hooks/transactions-and-errors/#nested-operations)
explains what rolls back when a related write fails.

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

See [Live server validation](/docs/fields/live-validation/) for its context properties and lookup
limits. These checks run separately from document and field hooks.
