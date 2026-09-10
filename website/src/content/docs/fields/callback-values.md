---
title: 'Using other field values'
description: 'Read nearby fields, compare a value with its saved version, and use request information in field callbacks.'
product: core
eyebrow: 'Field behavior'
order: 63
aliases:
  [
    'sibling data',
    'previous field value',
    'field callback context',
    'operation.Context.Siblings',
    'ctx.Prior',
    'ctx.Root'
  ]
navigation:
  section: 'Model content'
  parent: fields
  order: 330
  title: 'Using other field values'
---

A field validator, hook, access rule, or dynamic default sometimes needs more than one value. You might check a
checkbox beside a title, compare a product code with its saved value, or look up a selected
supplier. Ridu passes this information in the callback's context, usually named `ctx`.

For an introduction to the context types and `operation.Value[T]`, read
[Operations and callbacks](/docs/go-packages/operation/). [Documents and values](/docs/go-packages/store/)
explains the `store.Value` accessors used to read objects, lists, strings, and numbers below.

## Read nearby and previously saved fields {#sibling-data}

Use these three properties to choose where to read:

| Property       | What it contains                                             | Example                                        |
| -------------- | ------------------------------------------------------------ | ---------------------------------------------- |
| `ctx.Siblings` | This field and the other fields in the same object or row    | Read `locked` beside a variant's `sku`         |
| `ctx.Root`     | Fields at the top of the document                            | Read a product's `title` from inside a variant |
| `ctx.Prior`    | The same object or row as it was saved before this operation | Read the variant's previous `sku`              |

For a top-level title, all three refer to top-level fields, with `Prior` holding the previous
values. For `seo.title`, `Siblings` and `Prior` refer to the current and saved `seo` objects.
Inside an array, they refer to the same row, even after it moves to a different position.

`Prior` is empty when the enclosing object or row has not been saved before. It is also empty
for a standalone read: a read has no previous write to compare with.

## Example: keep a locked product code unchanged {#example}

This validator reads a checkbox beside the SKU and compares the new SKU with the saved one.
Create `content/variants.go`:

```go title="content/variants.go" focus={18-23}
package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var Variants = field.Array("variants", field.Fields{
	field.Text("sku").Required().Validate(validateSKU),
	field.Checkbox("locked"),
})

func validateSKU(
	ctx operation.ValidationContext,
	value operation.Value[string],
) ([]operation.Issue, error) {
	current, present := value.Get()
	// Prior follows this row's key, even after the row moves.
	previous, existed := ctx.Prior.String("sku")
	// Use this row's current checkbox: clearing it unlocks the SKU.
	locked, _ := ctx.Siblings.Get("locked").BooleanValue()
	// A new row has no previous SKU to protect.
	if locked && existed && (!present || current != previous) {
		return []operation.Issue{{
			Code:    "sku_locked",
			Message: "A locked variant's SKU cannot change",
		}}, nil
	}
	return nil, nil
}
```

Add `Variants` to a product collection's `Fields`. Save a variant with SKU `SKU-A` and its
Locked checkbox selected. Change its SKU and save again: the form should show the error.
Move that row and try again; the rule still compares against `SKU-A`, not the value of whichever
row previously occupied its new position.

This rule uses the current checkbox value. Clearing Locked allows the SKU to change. Add
[field access rules](/docs/access-control/#field-access) if only certain users should be able
to change the lock.

## Read strings, objects, and lists {#reading-values}

`value.Get()` reads the callback's own value and reports whether it is present. An empty string
can be present; `operation.Empty[string]()` means no value.

The context's field collections have methods for reading direct children:

```go
// The bool reports whether a string exists, even if it is empty.
title, hasTitle := ctx.Root.String("title")
seoTitle, _ := ctx.Root.Get("seo").Get("title").StringValue()
variants := ctx.Root.Get("variants")
variantCount := variants.Len()
// In a nested callback, Siblings reads this object or row.
locked, _ := ctx.Siblings.Get("locked").BooleanValue()
```

`Get("seo.title")` does not walk a path. Read the `seo` object first, then its `title` property.
`Get` reads an immutable child value without copying its container. Use `Lookup` when you need
to distinguish a missing child from explicit null, and `Elements()` to iterate list items in
order. `Entries()` iterates object members in unspecified order. Retained values keep their
snapshot even when a later hook changes the document.

Use `CopyObject()` or `CopyList()` only when you need a mutable map or slice; edits to those copies
do not change the document. A field hook changes its own value by returning a replacement. Use a
[collection or global hook](/docs/hooks/context/#hook-context) when a change spans several fields.

## Which values does a callback see? {#callback-phases}

A validator sees the values that would be saved, including unchanged fields from an update.
Other callbacks run earlier or later, so their values differ:

| Callback                             | Values available                                                                                        |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| `.DefaultFrom(...)`                  | Input available when choosing the initial value; it may be invalid, and other defaults may not have run |
| Raw `BeforeValidate` hook            | Input received so far, which may be invalid or contain only the fields being updated                    |
| Typed write hook                     | The proposed document, including unchanged saved values and changes made by earlier hooks               |
| `.Validate(...)`                     | The proposed document after write hooks have finished                                                   |
| Read hook or computed-field resolver | Values being prepared for this response, including requested related documents and locale fallback      |
| Event hook such as `AfterChange`     | Values at that stage; the hook can return an error but cannot replace a field value                     |

“Typed” means Ridu has converted the input to the field's Go type, such as `string`. A raw hook
instead receives `operation.Value[store.Value]`, so it can handle input before that conversion.

A default callback receives `operation.DefaultContext` without a separate current-value argument:
it chooses an initial value only when the field is omitted. `Prior` describes the previously
saved parent object or row, just as it does in other field callbacks. Defaults can also run during
`operation.Update` when the update adds a new object or row; checking only for `operation.Create`
would skip these new values. See [Set default field values](/docs/fields/defaults/#context) for
the full rules, including translated content.

### Distinguish missing input from an explicit null {#missing-input}

In a raw hook, an empty `operation.Value[store.Value]` means the field was omitted.
`operation.Present(store.Null())` means it was explicitly set to null. Read this separate value
argument when that distinction matters. Earlier hooks may already have changed the input.

Omitting a field from an update keeps its saved value, which typed callbacks still receive.
After defaults and retained update values have been applied, typed string, number, and boolean
callbacks use the same empty state for a missing value and a null value. Their surrounding
values may contain null entries for empty fields, so `ctx.Siblings.Lookup("title")`
does not tell you whether the caller submitted a title. Later callbacks do not receive a separate
copy of the original submitted patch.

Read hooks run before the final removal of protected fields. Seeing a sibling in a read hook
does not mean the user is allowed to receive it.

## Identify the request and signed-in user {#callback-identity}

- `ctx.Operation` names the document operation, such as `operation.Create` or `operation.Update`.
- `ctx.ID` is the document ID. It can be empty before a new document has been assigned one.
- `ctx.Actor.ID` identifies the signed-in user; an empty ID means the request is anonymous.
  `ctx.Actor.Collection` identifies their auth collection, and `ctx.Actor.Data` contains their fields.
- Pass `ctx.Context` to network calls so they stop when the request is cancelled or its deadline expires.

Document IDs and other request information are separate from `Root`, which contains field values.
`CollectionID` and `GlobalID` are Ridu's internal resource identifiers, not the slugs you configured.

For detailed logging, `OccurrenceID` identifies one field value across row moves and locales;
`SchemaOccurrenceID` identifies the configured field shared by those rows and locales. Most
callbacks do not need them. Do not parse these IDs or use them as row indexes. Include the resource
and document IDs when correlating logs from different documents.

## Work with translations {#callback-locales}

Attach validation to a localized field in the usual way:

```go
field.Text("title").Localized().Validate(validateTitle)
```

During a French update, `ctx.Locale` is `"fr"`. The current value is French, and
`ctx.Prior.String("title")` reads the previously saved French title. If only an English title
existed before, there is no prior French title. Fallback text does not count as a saved translation.
This also applies inside groups and array rows.

Reads can use locale fallback. On a French read, `ctx.Locale == "fr"` identifies the requested
locale, but the title might come from English. Use the response's
[localization source metadata](/docs/localization/#read-one) when you need to know its source.
The callback context does not include that source map.

During an all-locales read, a callback visiting a translated field receives one exact translation
and `ctx.AllLocales == false`. A field shared between locales can receive locale-keyed values
in `Root` with `ctx.AllLocales == true`. Check this flag before treating a localized root value
as a string.

## Look up a related document {#callback-reader}

Use `ctx.Local.FindByID(ctx.Context, "suppliers", supplierID)` to read a supplier whose ID you
already have. It uses the same user, transaction, and content locale as the callback and checks
the usual read permissions. It does not use locale fallback: a missing French supplier title
stays missing during a French validation.

Use this reader to check a selected document. It cannot write another document, switch users,
or bypass access rules. Passing a different Go context cannot leave the active transaction or
ignore cancellation of the original request. For coordinated writes, use
[a collection or global hook with the local API](/docs/hooks/transactions-and-errors/#nested-operations).

## Read values during a live check {#live-validation-context}

`.LiveValidate(...)` receives `operation.LiveValidationContext`. It describes unsaved form input,
before defaults and save hooks run. `Root` and `Siblings` include retained update values;
`Input` records only what the request submitted, and `Prior` contains saved values.

The [Live server validation guide](/docs/fields/live-validation/) explains these properties with
examples, including how saved rows are matched after reordering and how values work inside an
embedded editor.
