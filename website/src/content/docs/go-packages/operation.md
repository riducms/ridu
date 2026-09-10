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
    'operation.DefaultContext',
    'operation.Present',
    'operation.Empty',
    'operation.Keep',
    'operation.Replace'
  ]
relatedSymbolIds:
  - 'go:github.com/riducms/ridu/operation#Value'
  - 'go:github.com/riducms/ridu/operation#Change'
  - 'go:github.com/riducms/ridu/operation#Context'
  - 'go:github.com/riducms/ridu/operation#DefaultContext'
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

Import `github.com/riducms/ridu/operation` when you write a field hook, validator, access rule, or dynamic default.
It provides the arguments Ridu passes to your function and the results you return: the current
operation, a value that may be empty, a replacement value, or a validation message.

You do not need it for a basic field declaration such as `field.Text("title").Required()`.
It becomes useful when you add your own behavior, such as trimming a subtitle before saving
or checking that a sale price is lower than the regular price.

The package does not execute database operations. To create, read, or update a document yourself,
use the [local Go API](/docs/local-api/). For the values in a document's map, see
[Document values](/docs/go-packages/store/).

## Check what is happening {#operation-kinds}

`ctx.Operation` is an `operation.Kind`: a named value such as `operation.Create` or
`operation.Update`. Compare it with these constants when your function should run only for
certain actions.

| Constant                    | What it identifies                                              |
| --------------------------- | --------------------------------------------------------------- |
| `operation.Create`          | Creating a collection document                                  |
| `operation.Duplicate`       | Creating a copy of an existing document                         |
| `operation.Read`            | Reading one document or a list, or reading a global             |
| `operation.Update`          | Updating a document or global, including a global's first save  |
| `operation.Delete`          | Deleting a document; moves it to trash when trash is enabled    |
| `operation.RestoreDeleted`  | Restoring a document from trash                                 |
| `operation.DeletePermanent` | Permanently deleting a trashed document                         |
| `operation.Publish`         | Publishing a document or global                                 |
| `operation.Unpublish`       | Moving a document or global back to draft                       |
| `operation.ReadVersions`    | Reading retained version history                                |
| `operation.Admin`           | Checking whether an authenticated user may enter the admin      |
| `operation.Unlock`          | Checking permission to take over another editor's document lock |

These names describe the action, not the hook phase. An `AfterRead` hook that prepares a create
response still sees `operation.Create`. Access checks use these kinds too; a kind's presence
does not mean every hook runs for it. See the [hook lifecycle](/docs/hooks/collections/#lifecycle)
for the stages that run on reads, writes, and deletes.

**Restoring a saved version is different from restoring trash.** A version restore checks
`ReadVersions` permission, then runs `Publish` for a published snapshot or `Unpublish` for a
draft snapshot. Restoring explicitly as a draft uses `Unpublish`. Use `RestoreDeleted` only for
the trash action; it does not run `AfterChange`.

## Read a value that may be empty {#value-presence}

A field callback reads or returns `operation.Value[T]`. `T` is the Go type of the field value: `string`
for Text, `float64` for Number, or `bool` for Checkbox, for example. The wrapper records whether
a value is present separately from the value itself.

Call `value.Get()` to read both results, as in `subtitle, present := value.Get()`. Check
`present` before using the value. When it is false, the first result is Go's zero value for
that type; it is not a value supplied by the author.

| Value you construct          | What `Get()` returns | Meaning                                 |
| ---------------------------- | -------------------- | --------------------------------------- |
| `operation.Empty[bool]()`    | `false, false`       | No boolean value                        |
| `operation.Present(false)`   | `false, true`        | An explicit false                       |
| `operation.Present(0.0)`     | `0, true`            | An explicit number zero                 |
| `operation.Present("")`      | `"", true`           | An explicit empty string in the wrapper |
| `operation.Present("Hello")` | `"Hello", true`      | A supplied string                       |

`Present(...)` wraps a value. `Empty[T]()` wraps no value and needs the type in square brackets
because there is no argument from which Go can infer it. An uninitialized `operation.Value[T]`
is also empty. Normal field validation still applies: a present empty string does not bypass
a Text field's `Required()` rule.

A dynamic default returns this same wrapper. `operation.Present(value)` supplies the initial
value; `operation.Empty[T]()` supplies no default, and required validation still applies.
The callback receives `operation.DefaultContext`, with request information and the input
available before validation. It has no separate current-value argument because it only runs
for omitted fields that need an initial value. See [Set default field values](/docs/fields/defaults/).

### Missing input and explicit null {#raw-input}

A raw field hook, such as `BeforeValidate`, receives `operation.Value[store.Value]` before the
input has passed type checks. At that stage:

- `operation.Empty[store.Value]()` means the caller omitted the field.
- `operation.Present(store.Null())` means the caller explicitly supplied null.
- Other present values may still have the wrong type for the field.

Typed callbacks run later. An omitted field in an update may already have its saved value,
and an optional field's null or missing value can become the same empty typed value. Do not
use a typed callback's `present` flag to decide whether the caller submitted the field.

Use a raw hook when that distinction affects your rule. `ctx.Root` and `ctx.Siblings` are
snapshots of values at the current phase; their membership is not a record of submitted keys.
See [missing input and null](/docs/fields/callback-values/#missing-input) for more detail.

## Keep, replace, or clear a field {#field-changes}

A field transform returns an `operation.Change[T]` and an error. Its return value tells Ridu
what to do with this field:

| Return                                          | Result                                                                   |
| ----------------------------------------------- | ------------------------------------------------------------------------ |
| `operation.Keep[string]()`                      | Leave the current value unchanged                                        |
| `operation.Replace(operation.Present("Hello"))` | Set the field to `"Hello"`                                               |
| `operation.Replace(operation.Empty[string]())`  | Clear the field                                                          |
| A non-nil error                                 | Stop the operation; a replacement returned with the error is not applied |

`Keep` and `Replace(Empty)` are different. The first preserves a value; the second clears it.
For an ordinary optional field, clearing produces null. It does not remove the field property
from stored documents. A required field still has to pass validation.

This reusable field trims a subtitle and clears it when the author enters only spaces:

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
	_ operation.WriteContext,
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

Add `Subtitle` to a collection's `Fields`. Saving `"  Hello  "` stores `"Hello"`; saving only
spaces clears it. An update that omits `subtitle` retains the saved value. The `_` parameter
means this function does not need its context.

`BeforeChange` receives typed values and runs before custom validators. Ridu checks the
replacement again before saving. `AfterRead` transforms use the same keep-or-replace results
but change only the returned value, leaving storage unchanged. See
[Field hooks](/docs/hooks/fields/) for both kinds of hook and their registration.

## Read the user and surrounding fields {#callback-context}

Field callbacks receive a context whose type describes the job the function does:

| Context                       | Where you use it                                                      |
| ----------------------------- | --------------------------------------------------------------------- |
| `operation.WriteContext`      | A raw or typed hook that can return a field replacement               |
| `operation.ReadContext`       | A response transform or a Virtual field's value resolver              |
| `operation.ValidationContext` | A `.Validate(...)` callback that returns messages                     |
| `operation.DefaultContext`    | A `.DefaultFrom(...)` callback that chooses an initial field value    |
| `operation.AccessContext`     | A field access rule that allows or denies access                      |
| `operation.EventContext`      | A field event hook, such as `AfterChange`, that returns only an error |

These contexts expose the same named properties, with values appropriate to their phase:

- `ctx.Operation` identifies the action, and `ctx.ID` identifies the document when it has an ID.
- `ctx.Actor.ID` identifies the signed-in user; an empty ID means an anonymous request.
- `ctx.Siblings` contains this field and nearby fields in the same group or row.
- `ctx.Root` contains top-level fields.
- `ctx.Prior` contains the previously saved version of this field's group or row. It is empty
  when there was no previous value there, including a new row or a standalone read.
- `ctx.Local.FindByID` reads a related document with the current user, exact locale, and active
  transaction. Pass `ctx.Context` so cancellation travels with the call.

For example, `ctx.Siblings.String("title")` reads a nearby title without a Go type assertion.
These views share immutable values: ordinary reads do not copy maps or lists. Each view keeps
its snapshot when later hooks make changes. A transform must return `operation.Replace(...)`
for its own field to save a change. See
[Using other field values](/docs/fields/callback-values/) for nested rows, previous values,
translations, and related lookups.

### Collection and global hooks use a different context {#resource-context}

Collection and global hooks receive `ridu.HookContext`, which works with the whole document.
Change entries in its `ctx.Data` map before saving to update several fields together. Its
`ctx.Actor` is a document pointer and is `nil` for an anonymous request. Its `ctx.Local` is the
full local API, so it can also write related records.

Field transforms and validators receive their value as a separate argument. Field contexts
provide a read-only local reader. Use the field
context for one field's rule; use a collection or global hook for a change involving several
fields or related writes. [Hook context](/docs/hooks/context/) explains when values are
available and how to forward the user and locale to related operations.

A relationship also illustrates why the callback phase matters: its write value is an
`operation.ID`, while its read value is an `operation.ReferenceOutput`. On a read, call `ID()`
for the referenced ID and `Document()` to check whether the related document was populated.
Do not assume every read includes the related document.

## Return a validation message {#validation-issues}

Return `[]operation.Issue` from a field validator when a value is invalid. Each issue has a
stable `Code` for API clients and a `Message` for the author. Returning `nil, nil` accepts the
value. The separate error return means the check could not run, such as a failed lookup.

Leave `Target` unset to show the message on the field being validated. When a rule compares
children of a group, use `operation.At("childName")` to show it on the child that needs fixing.
This pricing group rejects a sale price that is not lower than the regular price:

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
	_ operation.ValidationContext,
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

Add `Pricing` to a collection's `Fields`. A price of `10` with a sale price of `0` is valid:
zero is a real price. A sale price of `12` produces a message beside `pricing.salePrice`, and
the invalid change is not saved. `Target` starts inside the field being validated, so it uses
`"salePrice"`, not `"pricing.salePrice"`.

### Put a message on a repeated row {#row-targets}

When validating an entire Array or Blocks field, select a row by its `_key` before selecting
its child field. Ridu gives each row a stable key so the message can follow it after reordering.

| Validator location      | Target                                                  | Message belongs to             |
| ----------------------- | ------------------------------------------------------- | ------------------------------ |
| A `details` group       | `operation.At("seo.title")`                             | The title inside its SEO group |
| A `variants` array      | `operation.At().Row(rowKey).Field("sku")`               | The SKU in that variant        |
| A `layout` Blocks field | `operation.At().Block(rowKey, "hero").Field("heading")` | The heading in that Hero block |

Read the key from the candidate row; an array index is not a row key. `Block` also checks the
block type. A target can stay on the current field or go into its children; it cannot point to
an unrelated field. Ridu checks the target against the submitted candidate and fills in the
document, field, and locale information for you.

For nested lists and translations, see [validation targets](/docs/fields/validation/#rows).
For additional types and methods, see the [operation reference](/reference/operation/).

## Check a field while the author edits {#live-validation}

`.LiveValidate(...)` callbacks receive `operation.LiveValidationContext` and an
`operation.Value[T]`, and return the same `[]operation.Issue, error` results as a save validator.
For a text field, `T` is `string`; a missing or null value is empty.

This context describes the unsaved form. `Root` and `Siblings` include submitted values and
retained update values, while `Prior` contains saved values. `Input` lets you distinguish an
omitted property from an explicit null. Defaults and save hooks have not run, and `ctx.Local`
can only read documents.

Use the [Live server validation guide](/docs/fields/live-validation/) for commented examples,
the complete context table, and how to share a rule with `.Validate(...)` on save.
