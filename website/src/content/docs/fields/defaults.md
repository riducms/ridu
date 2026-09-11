---
title: 'Set field defaults'
description: 'Give new fields a fixed value or calculate one when saving, using the signed-in user, content language, or nearby input.'
product: core
eyebrow: 'Field behavior'
order: 62
aliases:
  [
    'dynamic defaults',
    'DefaultFrom',
    'defaultValue',
    'default field values',
    'server defaults',
    'initial field value',
    'Context'
  ]
relatedSymbolIds:
  - 'go:github.com/riducms/ridu/field#DefaultFunc'
  - 'go:github.com/riducms/ridu/field#TextField.DefaultFrom'
  - 'go:github.com/riducms/ridu/operation#Context'
navigation:
  section: 'Model content'
  parent: fields
  order: 300
  title: 'Default values'
---

A default supplies an initial value when a caller leaves a field out of a new document. Use
`.Default(value)` for a fixed value, or `.DefaultFrom(callback)` to calculate one on the server.
The callback can use the signed-in user, content language, or input supplied for nearby fields.

| You want to…                                           | Use                                      | Example                                                    |
| ------------------------------------------------------ | ---------------------------------------- | ---------------------------------------------------------- |
| Start every field with the same value                  | `.Default(value)`                        | Give a new article the title `"Untitled"`                  |
| Choose a value when the document is saved              | `.DefaultFrom(callback)`                 | Choose the initial title in the request's content language |
| Change an existing value when saving                   | A [field hook](/docs/hooks/fields/)      | Trim a title each time it changes                          |
| Calculate a value for each response without storing it | A [virtual field](/docs/fields/virtual/) | Calculate a reading time                                   |

Defaults preserve values a caller supplies, including `false`, `0`, `""`, and `null`. Adding a
default to your config does **not** fill existing documents. New groups and rows can receive
defaults during an update; [the rules below](#initialization) explain when.

## Set a fixed default {#literal-defaults}

```go
field.Text("title").Default("Untitled")
field.Number("priority").Default(0)
field.Checkbox("featured").Default(false)
field.TextList("sellingPoints").Default("Solid oak", "Hand finished")
field.NumberList("availableSizes").Default(8, 10, 12)
```

These values appear immediately in a new admin form. API requests that omit these fields also
receive the defaults when creating a document. A placeholder is only an input hint; it does not
save a value.

## Choose a default for the content language {#dynamic-defaults}

Create `content/defaults.go` and add `ArticleTitle` to your collection's `Fields` list. This
example assumes you have [configured English and French content locales](/docs/localization/).

```go title="content/defaults.go" focus={11,16-21}
package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var ArticleTitle = field.Text("title").
	Required().
	Localized().
	DefaultFrom(initialTitle)

func initialTitle(
	ctx operation.Context) (operation.Value[string], error) {
	// Locale is the content language, not the admin interface language.
	if ctx.Locale == "fr" {
		// Present supplies the value; nil means the callback succeeded.
		return operation.Present("Sans titre"), nil
	}
	return operation.Present("Untitled"), nil
}
```

Ridu calls `initialTitle` when it needs an initial title. You pass the function to
`.DefaultFrom(initialTitle)`; you do not call it yourself with `initialTitle()`.

| Create request                   | Result                                                          |
| -------------------------------- | --------------------------------------------------------------- |
| English, title omitted           | Saves `"Untitled"`                                              |
| French, title omitted            | Saves `"Sans titre"`                                            |
| French, `"title": "Mon article"` | Keeps `"Mon article"`; the callback does not run                |
| `"title": ""` or `"title": null` | The callback does not run; `Required()` rejects the empty title |

## What happens in the admin? {#admin-and-generation}

**Dynamic defaults run when saving, not when opening a form.** Unlike a fixed `.Default(...)`,
a `.DefaultFrom(...)` value is not known until the server handles the request.

1. Open a new document. The field has no calculated value yet.
2. Leave it untouched and save. The admin omits it so the server can run its default.
3. After a successful save, the form displays the saved value.

Typing and then clearing the field submits an explicit empty value, so the default will not
refill it. The same distinction applies to fields inside new groups, array rows, and blocks.

An untouched required field with a dynamic default can reach the server for validation. If the
callback supplies no value, saving fails with the normal required-field message. The default
being configured is not a guarantee that it returns a valid value.

For [rich-text embedded forms](/docs/rich-text/), **Apply** updates the rich-text content in the
form. Dynamic defaults run when you save the parent document.

## Return a value, no default, or an error {#results}

A default callback takes one `operation.Context` argument and returns two results:
`operation.Value[T]` and `error`. `T` is the field's Go value type. It does not receive a current
value argument because it runs only when that field needs an initial value.

| Return                          | Meaning                                                    |
| ------------------------------- | ---------------------------------------------------------- |
| `operation.Present(value), nil` | Use `value` as the default, then validate it               |
| `operation.Empty[T](), nil`     | Supply no default; a required field still fails validation |
| `operation.Empty[T](), err`     | Stop the save because the callback could not finish        |

`Present(false)`, `Present(0.0)`, and `Present("")` all supply values. They do not mean “no
default.” The final `nil` means there was no error while running the function. See
[Operations and callbacks](/docs/go-packages/operation/#value-presence) for these wrappers.

### Supported field types {#supported-fields}

| Fields                                           | Callback's first result      |
| ------------------------------------------------ | ---------------------------- |
| Text, Textarea, Code, Email, Date, Select, Radio | `operation.Value[string]`    |
| Number                                           | `operation.Value[float64]`   |
| Checkbox                                         | `operation.Value[bool]`      |
| MultiSelect, TextList                            | `operation.Value[[]string]`  |
| NumberList                                       | `operation.Value[[]float64]` |

The result must satisfy the field's normal rules: a Date string must match its `Format`, a
Select value must be one of its choices, and an Email value must be a valid email address.
Length limits, number bounds, and custom validators still apply. Ridu copies returned
MultiSelect, TextList, and NumberList slices before using them as document values.

Slug fields use their source and normalization settings and reject defaults, even though
`field.Slug(...)` returns a `TextField`. Relationships, uploads, Group, Array, Blocks, JSON,
Point, plugin values, and virtual fields do not have `DefaultFrom`. Supported fields nested
inside groups, arrays, and blocks can use it.

Field access rules still control what a caller may submit. A server default may fill an
omitted field that the caller cannot write; this does not give the caller permission to supply
it. Read permissions still decide whether the saved value appears in the response.

## Read the user and nearby values {#context}

`operation.Context` describes the request and the values available when Ridu chooses
the default. The most useful properties are:

| Property        | What it contains                                                                                       |
| --------------- | ------------------------------------------------------------------------------------------------------ |
| `ctx.Actor`     | The signed-in user's ID, auth collection, and field data. `Actor.ID` is empty for an anonymous request |
| `ctx.Locale`    | The content language being saved, such as `"fr"`; empty if localization is not configured              |
| `ctx.Siblings`  | Fields in this group or row; for a top-level field, the document's top-level fields                    |
| `ctx.Root`      | The document's top-level field values                                                                  |
| `ctx.Prior`     | The previously saved group or row. Empty when that group or row is new                                 |
| `ctx.Operation` | The document action, such as `operation.Create` or `operation.Update`                                  |
| `ctx.ID`        | The document ID, which may be empty before a new document is saved                                     |
| `ctx.Context`   | Go cancellation and deadlines; pass it to work started for this request                                |
| `ctx.Local`     | A reader for looking up a known document with the same caller's permissions                            |

`Root` and `Siblings` are read-only snapshots. They can include unvalidated input; check the
type and any assumptions you depend on. Do not rely on them containing another dynamic
default's result, regardless of field order.

The context also has collection/global identifiers and field tracking identifiers. See the
[`operation.Context` reference](/reference/operation/context/) for every property, and
[Using other field values](/docs/fields/callback-values/) for reading nested values.

### Use the signed-in user's name {#user-default}

Add `AuthorName` to a collection to suggest a byline when saving. This example assumes your
auth collection has a Text field named `displayName`.

```go title="content/default_author.go" focus={8,13-22}
package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var AuthorName = field.Text("authorName").DefaultFrom(initialAuthorName)

func initialAuthorName(
	ctx operation.Context) (operation.Value[string], error) {
	// Anonymous requests have no signed-in user to copy a name from.
	if ctx.Actor.ID == "" {
		return operation.Empty[string](), nil
	}
	name, present := ctx.Actor.Data.String("displayName")
	if !present || name == "" {
		// Leave this optional field empty when the account has no name.
		return operation.Empty[string](), nil
	}
	return operation.Present(name), nil
}
```

A signed-in user with `displayName: "Sam"` gets `"Sam"` as the initial byline. An anonymous
request, or an account without a name, leaves this optional field empty. A submitted byline
wins over the default. This is editable content, not a record of who performed the operation.

For a related-document lookup, use `ctx.Local.FindByID(ctx.Context, collection, id)` and return
any error that prevents you from choosing a value. The lookup checks read access, shares the
current transaction and cancellation, and reads the exact content locale without fallback.
It cannot write documents or change the caller's permissions.

## Initialize new groups and rows {#nested-defaults}

A child default runs when its group or row is being created. In an array, `ctx.Siblings` means
that particular row, so each row can choose a different label from its own submitted URL.

Add these fields to a collection:

```go title="content/default_rows.go" focus={10,14,21-26}
package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var NavigationFields = field.Fields{
	field.Group("navigation", field.Fields{
		field.Text("label").DefaultFrom(initialLinkLabel),
	}),
	field.Array("links", field.Fields{
		field.Text("url"),
		field.Text("label").DefaultFrom(initialLinkLabel),
	}),
}

func initialLinkLabel(
	ctx operation.Context) (operation.Value[string], error) {
	// Siblings reads this group or row, not another row in the array.
	url, present := ctx.Siblings.String("url")
	if present && url == "/about" {
		return operation.Present("About us"), nil
	}
	return operation.Present("New link"), nil
}
```

Create a document with this JSON input:

```json title="Request body" focus={2-3}
{
	"navigation": {},
	"links": [{ "url": "/about" }, { "url": "/contact" }]
}
```

The empty `navigation` object asks Ridu to initialize the group, giving it the label
`"New link"`. The array rows get `"About us"` and `"New link"`, respectively. Supplying
`"label": "Contact"` in the second row would keep `"Contact"` instead.

Omitting `navigation` entirely leaves the optional group empty: a dynamic child default does
not create its parent. Fixed child defaults can create a group; when they do, dynamic children
run too.

When updating, keep each existing row's `_key`. Ridu uses that key, plus the block type for
blocks, to recognize a saved row even after reordering. New rows receive defaults; retained
rows keep their saved values, including empty values. See
[editing nested data](/docs/go-packages/store/#nested-values) for a complete update example.

## Know when defaults run {#initialization}

Fixed and dynamic defaults follow the same server rules for omitted fields:

| Action                                                         | What happens                                                                             |
| -------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| Create a document                                              | Omitted fields can receive defaults                                                      |
| Save a global for the first time                               | Omitted fields can receive defaults; the operation is `operation.Update`                 |
| Add a new group, array row, or block during an update          | Its omitted children can receive defaults                                                |
| Omit a field from an existing document, group, or retained row | Its saved value or empty state stays unchanged                                           |
| Supply `null`, `""`, `[]`, `false`, or `0`                     | Keep the supplied value and validate it; do not run its default                          |
| Read an existing document                                      | Do not evaluate dynamic defaults or fill stored empty fields                             |
| Read an unsaved global                                         | Return fixed defaults where configured, without running dynamic callbacks or saving data |
| Add a default to a field that already has saved documents      | Do not backfill those documents                                                          |

Do not guard every callback with `ctx.Operation == operation.Create`: new rows and a global's
first save can need defaults during `operation.Update`.

### Save another content language {#localized-defaults}

A default uses the exact content language being saved. Text displayed through locale fallback
does not count as a saved translation.

Switching language on an existing document does not fill an omitted top-level translation or
an omitted translation in a retained row. Initializing a new localized group with `{}` can run
its child defaults in that language. An existing group or row keeps its saved values.

### Duplicate a document or restore a version {#duplicate-and-restore}

Duplicating a document keeps copied values, including empty ones. To choose a fresh value—for
example for a unique field—a [BeforeDuplicate hook](/docs/hooks/collections/) can remove that
field from `ctx.Data` with `delete(ctx.Data, "fieldName")` before defaults run. The new document
then receives a default for that omitted field. At the root, `ctx.Prior` can contain the source
document; copied rows receive new keys and have no prior row.

Restoring a version keeps its recorded values. It does not calculate defaults for fields absent
from the saved version, including fields added to your schema after that version was created.

### Combine defaults with hooks {#hook-order}

Raw `BeforeValidate` hooks run before defaults. If one supplies a value, the default does not
run for that field. Later typed hooks and validators see the chosen default.

Ridu remembers a callback's result for that field during the operation. If a later document
hook deletes the field, Ridu can reapply that result without calling the default again. Set an
explicit `null` when you mean to leave the field empty; normal required validation still applies.

Use a [document hook](/docs/hooks/collections/) when several values must be calculated in order.
Dynamic defaults cannot depend on one another. Keep callbacks free of irreversible side effects,
such as sending an email: a separate request or retry can run them again.

## Replace a default on a reusable field {#reuse}

The last default setting wins. Each setter returns a new field definition:

```go
base := field.Text("title").Default("Untitled")
// Replace the fixed value with a callback on this copy.
dynamic := base.DefaultFrom(initialTitle)
// Replace the callback with a new fixed value on another copy.
fixed := dynamic.Default("New article")
```

`base` still uses `"Untitled"`, `dynamic` uses only `initialTitle`, and `fixed` uses only
`"New article"`. If `initialTitle` returns `Empty`, Ridu does not fall back to `"Untitled"`.
A nil callback is a configuration error.

Defaults stay attached when you reuse a field through a factory, nest it, or refine its
children. See [Reusable fields](/docs/fields/#field-access-and-hooks) for those patterns.

## Understand generated inputs {#generated-inputs}

Generated create inputs allow you to omit a field with a dynamic default, even when the field
is required. Validation still decides whether the result can be saved; the callback may return
no value, an invalid value, or an error.

Resolving configuration, generating files, and building the admin do not run the callback.
The generated schema records that a dynamic default exists, without including the function,
its captured values, or a result from a particular request. See
[Generated types and files](/docs/generated-contracts/).
