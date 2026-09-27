---
title: 'Operations and callbacks'
description: 'Use the operation package to read callback values, change a field, check what is happening, and return validation messages.'
product: core
eyebrow: 'Go packages'
order: 38
aliases:
  [
    'operation package',
    'operation.Value',
    'operation.Change',
    'operation.Context',
    'operation.Context',
    'operation.Present',
    'operation.Empty',
    'operation.Keep',
    'operation.Replace'
  ]
relatedSymbolIds:
  - 'go:github.com/riducms/ridu/operation#Value'
  - 'go:github.com/riducms/ridu/operation#Change'
  - 'go:github.com/riducms/ridu/operation#Context'
  - 'go:github.com/riducms/ridu/operation#Context'
  - 'go:github.com/riducms/ridu/operation#Kind'
  - 'go:github.com/riducms/ridu/operation#Present'
  - 'go:github.com/riducms/ridu/operation#Empty'
  - 'go:github.com/riducms/ridu/operation#Keep'
  - 'go:github.com/riducms/ridu/operation#Replace'
  - 'go:github.com/riducms/ridu/operation#Issue'
navigation:
  section: 'Get started'
  parent: go-packages
  order: 10
  title: 'Operations and callbacks'
---

Import `github.com/riducms/ridu/operation` when you write a field hook, validator, dynamic
default, or field access rule. The package defines the arguments Ridu passes to those functions
and the values they return. You do not need it for a plain field declaration such as
`field.Text("title").Required()`.

The package does not run database operations itself. To create, read, or update a document, use
the [local Go API](/docs/local-api/).

## At a glance {#overview}

A field callback's signature is made of types from this package:

```go
func cleanSubtitle(
	ctx operation.Context, // The operation and nearby values
	value operation.Value[string], // This field's value, if any
) (operation.Change[string], error) // Keep or replace the value
```

| Type                                        | What it is                                              | Where you see it                             |
| ------------------------------------------- | ------------------------------------------------------- | -------------------------------------------- |
| `operation.Context`                         | The current operation and the values around the field   | The first argument of every field callback   |
| `operation.Value[T]`                        | A value of type `T` that may be empty                   | The value argument, and what defaults return |
| `operation.Change[T]`                       | An instruction to keep or replace the field's value     | What field transforms return                 |
| `operation.Kind`                            | The operation, such as `operation.Create`               | `ctx.Operation`                              |
| `operation.Issue`                           | A validation message for the author                     | What validators return                       |
| `operation.ID`, `operation.ReferenceOutput` | A related document's ID, and the reference Ridu returns | Relationship and upload values               |

## operation.Context {#callback-context}

`operation.Context` describes the operation a field callback is part of. Validators, defaults,
transforms, observers, [virtual field resolvers](/docs/fields/virtual/), and field access rules
all receive it:

| Property        | Description                                                                                                                                |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `ctx.Operation` | What is happening, as an [`operation.Kind`](#operation-kinds).                                                                             |
| `ctx.ID`        | The document's ID. It is empty while a document is being created.                                                                          |
| `ctx.Actor`     | The signed-in user: `ctx.Actor.ID`, their auth collection, and their fields in `ctx.Actor.Data`. The ID is empty for an anonymous request. |
| `ctx.Siblings`  | The fields next to this one: the same group, the same array row, or the top level.                                                         |
| `ctx.Root`      | The document's top-level fields.                                                                                                           |
| `ctx.Prior`     | The saved values of this field's group or row before the operation. Empty on create and for new rows.                                      |
| `ctx.Locale`    | The content locale, or empty without localization.                                                                                         |
| `ctx.Local`     | Reads another document with `ctx.Local.FindByID(...)`, as the same user and in the same transaction.                                       |
| `ctx.Context`   | Cancellation and deadline. Pass it to `ctx.Local` and network calls.                                                                       |

`Siblings`, `Root`, and `Prior` are read-only snapshots. Read a value by its field name:

```go
title, _ := ctx.Siblings.String("title")
price, _ := ctx.Siblings.Get("price").NumberValue()
seoTitle, _ := ctx.Root.Get("seo").Get("title").StringValue()
```

For the field in a nested group, `ctx.Siblings` is that group. In an array, it is the current row,
whatever its position. [Using other field values](/docs/fields/callback-values/) covers nested
rows, translations, and related-document lookups.

### Collection and global hooks use ridu.HookContext {#resource-context}

Collection and global hooks receive [`ridu.HookContext`](/docs/hooks/collections/#arguments)
instead. It holds the whole document: change entries in its `ctx.Data` map to change several
fields at once. Its `ctx.Actor` is a document pointer that is `nil` for an anonymous request, and
its `ctx.Local` is the full local API, which can also save other documents.
[Hook context](/docs/hooks/context/) compares the two.

## operation.Value {#value-presence}

`operation.Value[T]` carries a value of type `T` that may be empty. `T` is the field's Go type:
`string` for Text, `float64` for Number, `bool` for Checkbox. Call `Get()` to read the value and
whether one is present:

```go
subtitle, present := value.Get()
if !present {
	// There is no value. subtitle is "", Go's zero value for a string.
}
```

Create a value with `operation.Present(...)`, or an empty one with `operation.Empty[T]()`. An
empty value needs its type in square brackets, because there is no argument for Go to infer it
from:

| Value                        | `Get()` returns | Meaning           |
| ---------------------------- | --------------- | ----------------- |
| `operation.Present("Hello")` | `"Hello", true` | A string          |
| `operation.Present("")`      | `"", true`      | An empty string   |
| `operation.Present(false)`   | `false, true`   | An explicit false |
| `operation.Empty[bool]()`    | `false, false`  | No value          |

Check `present` before using the value: when it is `false`, the first result is Go's zero value,
not something an author entered. A [dynamic default](/docs/fields/defaults/) returns the same
type: `operation.Present(value)` supplies a default, and `operation.Empty[T]()` supplies none.

### Missing input and explicit null {#raw-input}

A raw callback, such as a `BeforeValidate` hook, receives `operation.Value[store.Value]` before
Ridu has checked the input's type:

- `operation.Empty[store.Value]()` means the caller did not send the field.
- `operation.Present(store.Null())` means the caller sent `null`.
- Any other present value might still have the wrong type for the field.

Later callbacks receive the typed value. By then, an update that did not send the field carries
the saved value, so use a raw hook when you need to know what the caller sent. See
[missing input and null](/docs/fields/callback-values/#missing-input).

## operation.Change {#field-changes}

A field transform returns an `operation.Change[T]` that tells Ridu what to do with the field:

| Return                                          | Result                                                                   |
| ----------------------------------------------- | ------------------------------------------------------------------------ |
| `operation.Keep[string]()`                      | Leave the value unchanged                                                |
| `operation.Replace(operation.Present("Hello"))` | Set the field to `"Hello"`                                               |
| `operation.Replace(operation.Empty[string]())`  | Clear the field                                                          |
| A non-nil error                                 | Stop the operation; a replacement returned with the error is not applied |

`Keep` preserves the value; `Replace(Empty)` clears it. A cleared optional field becomes null,
and a required field still has to pass validation. This field trims a subtitle and clears it
when the author enters only spaces:

```go title="content/subtitle.go" focus={18-21,24-29}
package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var Subtitle = field.Text("subtitle").Hooks(field.Hooks[string]{
	BeforeChange: []field.Transform[string]{cleanSubtitle},
})

func cleanSubtitle(
	_ operation.Context,
	value operation.Value[string],
) (operation.Change[string], error) {
	subtitle, present := value.Get()
	if !present {
		// There is no value to clean up; keep its current empty state.
		return operation.Keep[string](), nil
	}
	subtitle = strings.TrimSpace(subtitle)
	if subtitle == "" {
		// Clear this optional field instead of saving spaces.
		return operation.Replace(operation.Empty[string]()), nil
	}
	// Replace supplies this field's new value for the same save.
	return operation.Replace(operation.Present(subtitle)), nil
}
```

Saving `"  Hello  "` stores `"Hello"`, and saving only spaces clears the subtitle. An update that
does not send `subtitle` keeps the saved value. The same results work in an `AfterRead` hook,
where they change the response instead of the stored value; see
[Field hooks](/docs/hooks/fields/#return-values).

## operation.Kind {#operation-kinds}

`ctx.Operation` is an `operation.Kind`. Compare it with these constants when your code should run
only for some operations:

| Constant                    | What it identifies                                              |
| --------------------------- | --------------------------------------------------------------- |
| `operation.Create`          | Creating a collection document                                  |
| `operation.Duplicate`       | Creating a copy of an existing document                         |
| `operation.Read`            | Reading one document, a list, or a global                       |
| `operation.Update`          | Updating a document or global, including a global's first save  |
| `operation.Publish`         | Publishing a document or global                                 |
| `operation.Unpublish`       | Moving a document or global back to draft                       |
| `operation.Delete`          | Deleting a document, or moving it to the trash when trash is on |
| `operation.RestoreDeleted`  | Restoring a document from the trash                             |
| `operation.DeletePermanent` | Permanently deleting a trashed document                         |
| `operation.ReadVersions`    | Reading a document's version history                            |
| `operation.Admin`           | Checking whether a signed-in user may open the admin            |
| `operation.Unlock`          | Checking whether a user may take over another editor's lock     |

The kind names the whole operation, not the hook that is running: an `AfterRead` hook preparing
the response to a create sees `operation.Create`. Access rules use the same kinds.

Restoring a saved version is not `RestoreDeleted`. It runs as `Publish` for a published version
and as `Unpublish` for a draft; `RestoreDeleted` is only for the trash.

## operation.Issue {#validation-issues}

A field validator returns `[]operation.Issue` when a value is invalid. Each issue has a `Code`
that API clients can check and a `Message` for the author. Return `nil, nil` to accept the value,
and a non-nil error only when the check itself could not run, such as a failed lookup.

An issue appears on the validated field unless you set `Target`. Use `operation.At(...)` to put
it on a child field instead. This group rejects a sale price that is not lower than the price:

```go title="content/pricing.go" focus={24-32}
package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

var Pricing = field.Group("pricing", field.Fields{
	field.Number("price").Required().Min(0),
	field.Number("salePrice").Min(0),
}).Validate(validatePricing)

func validatePricing(
	_ operation.Context,
	value operation.Value[store.Value],
) ([]operation.Issue, error) {
	group, present := value.Get()
	if !present {
		return nil, nil
	}
	// A Group's value is an object containing its child fields.
	price, hasPrice := group.Get("price").NumberValue()
	sale, hasSale := group.Get("salePrice").NumberValue()
	if !hasPrice || !hasSale || sale < price {
		return nil, nil
	}
	return []operation.Issue{{
		Code:    "sale_price_too_high",
		Message: "Set a sale price lower than the regular price",
		// Start inside pricing and place the message on salePrice.
		Target: operation.At("salePrice"),
	}}, nil
}
```

A price of `10` with a sale price of `12` shows a message beside `pricing.salePrice`, and nothing
is saved. A sale price of `0` is valid, because zero is a real price. `Target` starts inside the
validated field, so it is `"salePrice"`, not `"pricing.salePrice"`.

### Put a message on a repeated row {#row-targets}

To target a field inside an array or blocks row, select the row by its `_key` first. Ridu gives
each row a stable key, so the message follows the row even after reordering:

| Validator on            | Target                                                  | Message appears on             |
| ----------------------- | ------------------------------------------------------- | ------------------------------ |
| A `details` group       | `operation.At("seo.title")`                             | The title in its SEO group     |
| A `variants` array      | `operation.At().Row(rowKey).Field("sku")`               | The SKU in that variant        |
| A `layout` blocks field | `operation.At().Block(rowKey, "hero").Field("heading")` | The heading in that Hero block |

Read `rowKey` from the row's `_key` value; an array index is not a row key. A target can point to
the validated field or any field inside it, but not to an unrelated field. See
[validation targets](/docs/fields/validation/#rows) for nested lists and translations.

## operation.ID and operation.ReferenceOutput {#references}

A relationship or upload field stores the related document's ID as an `operation.ID`. When Ridu
returns the field, it uses an `operation.ReferenceOutput` instead, which also holds the related
document if the request asked for it:

```go
id := reference.ID() // Always available.
doc, populated := reference.Document()
if populated {
	name, _ := doc.Values["name"].StringValue()
	// ...
}
```

Write hooks and validators receive the `operation.ID`; `.AfterRead(...)` hooks receive the
`operation.ReferenceOutput`. To return a changed reference, build one with
`operation.Populated(document)` or `operation.Unpopulated(id)`. The
[relationship read hook example](/docs/hooks/fields/#reference-read-hooks) uses both types.

## operation.LiveValidationContext {#live-validation}

A `.LiveValidate(...)` check runs while an author edits, before saving. It receives
`operation.LiveValidationContext` and the field's `operation.Value[T]`, and returns the same
`[]operation.Issue, error` as a save validator.

Its `Root` and `Siblings` hold the unsaved form values, `Prior` holds the saved values, and
`Input` shows exactly which properties the form sent. Defaults and save hooks have not run yet,
and `ctx.Local` can only read. The [Live server validation guide](/docs/fields/live-validation/)
has complete examples, and the [operation reference](/reference/operation/) lists every type.
