---
title: 'Live server validation'
description: 'Show useful server-side feedback while someone edits a field, and share the rule with validation when saving.'
product: core
eyebrow: 'Field behavior'
order: 62
aliases:
  [
    'live validation',
    'LiveValidate',
    'live field validation',
    'validation while typing',
    'server validation',
    'LiveValidationContext'
  ]
relatedSymbolIds:
  - 'go:github.com/riducms/ridu/field#LiveValidator'
  - 'go:github.com/riducms/ridu/field#NumberField.LiveValidate'
  - 'go:github.com/riducms/ridu/operation#LiveValidationContext'
navigation:
  section: 'Model content'
  parent: fields
  order: 320
  title: 'Live server validation'
---

Use `.LiveValidate(...)` to show feedback from a Go rule while an author edits a field. For
example, check that a product’s sale price is lower than its regular price.
The admin sends the unsaved input to the server and displays any messages beside the field.

**Register `.Validate(...)` too when the rule must prevent an invalid save.** Live checks give
early feedback; saving always runs its own validation. A successful live check does not reserve
a value or guarantee that a later save will pass.

| You need to…                                          | Configure                                                    |
| ----------------------------------------------------- | ------------------------------------------------------------ |
| Require a value or limit its length                   | Built-in options such as `.Required()` and `.MaxLength(...)` |
| Enforce your own rule when saving                     | [`.Validate(...)`](/docs/fields/validation/)                 |
| Run a server check while editing                      | `.LiveValidate(...)`                                         |
| Give early feedback and enforce the same rule on Save | Register the rule with both methods                          |

Adding `.LiveValidate(...)` to your Go config is enough for the framework admin. It handles
requests, timing, messages, retries, and discarding responses to older edits.

## Add feedback to a field {#add-check}

A product has a regular price and an optional sale price. If someone enters a sale price that
is equal to or higher than the regular price, show a message beside the sale price field.

### Define the price fields {#price-fields}

Define the collection in `content/products.go`, then add the collection returned by `Catalog()`
to your existing `ridu.Config.Collections` list:

```go title="content/products.go" focus={9-13}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var SalePrice = field.Number("salePrice").Min(0).
	// Enforce the rule whenever a document is saved.
	Validate(validateSalePrice).
	// Also check unsaved input while the author edits this field.
	LiveValidate(checkSalePrice)

func Catalog() []ridu.Collection {
	return []ridu.Collection{{
		Slug: "products",
		Fields: field.Fields{
			field.Text("name").Required(),
			field.Number("price").Label("Regular price").Required().Min(0),
			SalePrice,
		},
	}}
}

// ... validation functions shown below
```

### Add the price comparison {#price-comparison}

The two functions below receive different contexts because live checks see unfinished input,
while save validation sees values after defaults and write hooks. Both call `salePriceIssues`
so the comparison has one implementation.

Add these functions below `Catalog()` in the same `content/products.go` file:

```go title="content/products.go" focus={10-15,22-32}
// ... imports, price fields, and Catalog() shown above

func validateSalePrice(
	ctx operation.Context,
	value operation.Value[float64],
) ([]operation.Issue, error) {
	return salePriceIssues(ctx.Siblings, value), nil
}

func checkSalePrice(
	ctx operation.LiveValidationContext,
	value operation.Value[float64],
) ([]operation.Issue, error) {
	// Both callbacks use the same rule, so their feedback agrees.
	return salePriceIssues(ctx.Siblings, value), nil
}

func salePriceIssues(
	siblings operation.View,
	value operation.Value[float64],
) []operation.Issue {
	salePrice, hasSalePrice := value.Get()
	price, hasPrice := siblings.Get("price").NumberValue()
	// A sale price is optional. Wait until both numbers are available.
	if !hasSalePrice || !hasPrice || salePrice < price {
		return nil
	}
	// Return a message beside the sale price field.
	return []operation.Issue{{
		Code:    "sale_price",
		Message: "Sale price must be lower than the regular price.",
	}}
}
```

`value.Get()` reads the sale price being checked. `ctx.Siblings` reads the other fields in the
same object; `Get("price").NumberValue()` returns the regular price and whether it is available
as a number. Returning `nil` means there is no message to show.

### Try the example {#try-it}

1. Start your app with `bun run dev` and sign in to the admin.
2. Create a product and enter **100** for Regular price.
3. Enter **120** for Sale price, then pause for half a second or leave the field.
4. It should show **Sale price must be lower than the regular price.**
5. Change Sale price to **80**. After the next check, the message disappears.
6. Fill the product's name and save. The save validator checks the rule again.

After editing Sale price once, changing Regular price refreshes its feedback too. For example,
reducing Regular price to **70** makes a sale price of **80** invalid. Changing Regular price
alone does not start checks for an untouched Sale price.

Leave Sale price empty when the product is not on sale. If Regular price is missing, the
comparison waits for it; `.Required()` still enforces that field when saving.

## Know when checks run {#live-timing}

The admin starts checking a field after you edit it. If a check is on a Group, Array, or Blocks
field, editing one of its children starts the container's check too.

| Action                                              | Result                                                             |
| --------------------------------------------------- | ------------------------------------------------------------------ |
| Open or reset a form                                | No live checks run                                                 |
| Edit a field with `.LiveValidate(...)`              | Wait 500 ms after the last edit, then check                        |
| Leave an edited field while a check is waiting      | Send the pending check immediately                                 |
| Edit another field after a check has been activated | Refresh the active checks, since their rules may read other fields |
| Save                                                | Cancel pending live work and run normal save validation            |
| Change locale, leave the document, or remove a row  | Discard feedback that no longer belongs to the current input       |

The admin ignores older responses after further edits. Reordering rows triggers new checks
while keeping messages associated with the correct row. Live messages do not disable Save and
do not replace messages returned by a save attempt.

An unfinished number or JSON input pauses all active live checks in that form. For example,
while a JSON value is missing its closing brace, Ridu cannot send a faithful snapshot of the
current input. Finish or correct it to resume checking.

## Understand the feedback {#feedback}

Custom field bindings expose these `liveValidation.status` values:

| Status    | Meaning                                                    | What the standard admin shows                                        |
| --------- | ---------------------------------------------------------- | -------------------------------------------------------------------- |
| `idle`    | No active check yet                                        | No live status message                                               |
| `pending` | Waiting to send a check or waiting for its response        | “Checking…”                                                          |
| `checked` | The callbacks completed; **they may have returned issues** | Any returned messages beside their fields; no separate success label |
| `skipped` | The current input could not be checked                     | “This input could not be checked yet.”                               |
| `failed`  | The request or callback could not complete                 | “This check could not be completed.” and **Retry check**             |

An empty issue list only means this check reported no problems. A skipped or failed check makes
no claim about whether the value is valid. Retry uses the current form values.

## Read the callback arguments {#live-context}

A live validator receives `operation.LiveValidationContext` and `operation.Value[T]`. `T` is
the field's writable Go type, the same type used by its save validator.

| Fields                                                                      | Value argument                    |
| --------------------------------------------------------------------------- | --------------------------------- |
| Text, Textarea, Code, Email, Date, Select, Radio, Slug                      | `operation.Value[string]`         |
| Number                                                                      | `operation.Value[float64]`        |
| Checkbox                                                                    | `operation.Value[bool]`           |
| MultiSelect, TextList                                                       | `operation.Value[[]string]`       |
| NumberList                                                                  | `operation.Value[[]float64]`      |
| Relationship or Upload                                                      | `operation.Value[operation.ID]`   |
| Multiple relationships or uploads                                           | `operation.Value[[]operation.ID]` |
| Group, Array, Blocks, JSON, Point, polymorphic relationships, plugin fields | `operation.Value[store.Value]`    |

UI, Virtual, and Join fields do not have a writable value and do not support live validators.
For wrappers and accessors, see [Operations and callbacks](/docs/go-packages/operation/) and
[Documents and values](/docs/go-packages/store/).

### Compare current input with saved values {#context-values}

The server supplies the context; your callback reads it without changing the document.

| Property    | What it contains                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------------------- |
| `Root`      | Readable top-level document values, combining submitted edits with unchanged saved values on an update  |
| `Siblings`  | Readable values in this group or row, including unchanged saved values                                  |
| `Input`     | What was submitted for this group or row, before filling in unchanged values                            |
| `Prior`     | The previously saved group or row in this locale; empty for a new object, row, or translation           |
| `Actor`     | The signed-in user's identity and data; `Actor.ID` is empty for an anonymous caller                     |
| `Locale`    | One content locale, without fallback translations                                                       |
| `Operation` | `Create` for a new collection document; `Update` for edits and globals, including a global's first save |
| `ID`        | The existing document ID; empty for a new collection document                                           |
| `Context`   | Request cancellation and deadlines                                                                      |
| `Local`     | Read-only document lookup using the same caller, locale, and transaction                                |

For example, if an update omits `salePrice`, `Siblings.Get("salePrice").NumberValue()` can
return its saved value, while `Input.Lookup("salePrice")` reports that it was not submitted.
An explicit `null` is present in `Input` but has no typed number value.

These values have not passed save validation. Ridu has not applied server defaults or run save
transforms, so a defaulted field can still be empty and a title may still contain untrimmed
spaces. Handle absent or invalid dependencies instead of assuming the form is complete.

Context views omit fields the caller cannot read. `Local.FindByID` also enforces read access.
During live checks it returns stored values without read hooks, computed fields, or locale
fallback. It cannot write another document. See the
[`LiveValidationContext` reference](/reference/operation/live-validation-context/) for every
property and [Using other field values](/docs/fields/callback-values/) for nested reads.

### Handle missing or malformed values {#incomplete-input}

For a simple field in an available object, missing or null input can reach the callback as
`operation.Empty[T]()`. Check `value.Get()` before using it.

If a value cannot be decoded as the field's Go type, Ridu skips that check. It also skips a
Group, Array, or Blocks check when the container is missing, null, or malformed. An empty
object or list can be checked. A container callback must check its children itself: receiving
`store.Value` does not mean those children are valid or complete.

Live validation does not run every required-field check across an unfinished form. Decide what
your custom rule can reasonably check now, and leave the rest to save validation.

## Return messages or report a failed check {#results}

| Return                        | Meaning                                         |
| ----------------------------- | ----------------------------------------------- |
| `nil, nil`                    | The rule ran and has no messages                |
| `[]operation.Issue{...}, nil` | Show the returned field messages                |
| `nil, err`                    | The check could not run; fail this live request |

Use an issue for something the author can fix, such as a sale price that is too high. Use an error
for a failed lookup or unavailable service. An error fails the request rather than returning
a successful check with no issues. The browser receives generic failure feedback, not the
private Go error text.

Keep live checks read-only and safe to repeat. Pass cancellation to database or service calls.
Do not send emails or perform other lasting actions from a callback that can run after every
edit. Use [hooks](/docs/hooks/) for work tied to saving.

## Show messages in rows and rich-text blocks {#live-embedded}

A check on a child field returns messages beside that child. A container check can target a
child or row with the same [`operation.Issue` targets](/docs/fields/validation/#validation-issue-targets)
used when saving, including `operation.At().Row(rowKey).Field("salePrice")` for an Array validator.
Use row `_key` values, not positions. Blocks also need the expected block type.

Rich-text blocks and plugin embedded forms support the same checks. While an embedded form is
open, `Root` is the parent document before **Apply**; `Siblings` and `Input` describe the pending
embedded fields. `Prior` comes from the matching saved item. This lets a block check its own
values without pretending the pending block has already been applied to the parent.

**Apply** runs the embedded form's local checks and copies its draft into the parent form. It
does not save a document or turn live feedback into a save-time rule. Apply, Cancel, and closing
the editor discard pending live work; reopening starts fresh. Saving the parent runs the
embedded fields' `.Validate(...)` rules.

## Add or replace checks on a reusable field {#reuse}

`.LiveValidate(check)` appends a check. `.ReplaceLiveValidators(...)` replaces only the live
checks and leaves save validators, hooks, defaults, and access rules in place.

```go
// Reuse SalePrice with its save validator, but disable live requests here.
saveOnlyPrice := SalePrice.ReplaceLiveValidators()
// Replace its live checks without changing the original field.
customPrice := SalePrice.ReplaceLiveValidators(checkSalePrice)
```

These setters return updated field definitions. Live checks stay attached when you reuse or
nest a field, or edit it through a field factory. A nil callback is a configuration error.
The generated schema records that live checks exist; it does not include or run the Go function.

## Use a custom field editor {#custom-editors}

The framework also handles live checking for [custom field components](/docs/custom-components/field-components/)
and [plugin fields](/guides/custom-fields/). Continue to update the value through `field.set(...)`
and display `field.issues` with the normal Field wrapper. The surrounding admin shows pending,
skipped, and retry feedback automatically.

For a component that needs to react to checking, read `field.liveValidation.status`. Its
`field.liveValidation.retry()` requests a retry using the current values. Use these bindings
rather than starting another validation request or timer inside the component. See
[`FieldLiveValidation`](/reference/plugin/field-live-validation/) for the contract.

Live checking is available in document and account editing forms; first-user setup is not a
document validation flow.

## Check from your own client {#live-transport}

The framework admin needs no client setup. A separate application can use
`collectionLiveValidation` or `globalLiveValidation`; the
[SDK example](/docs/typescript-sdk/#live-validation) shows the request, cancellation, and results.
The REST routes are `POST /api/collections/{slug}/validate` and
`POST /api/globals/{slug}/validate`.

Live requests must pass the resource's Admin, Read, and Create or Update rules, plus read/write
access for the checked field and its parents. Anonymous checks are possible only when those
rules permit them. The server reads saved values itself; the request cannot claim a different
previous value or bypass permissions.

Each request accepts at most 1 MiB of input, 64 field paths, and eight nested embedded editors.
The server gives checks a five-second deadline; callbacks must cooperate with cancellation.
Use one exact content locale. All-locales and fallback options are unavailable. Live requests
return evaluations and issues without saving, applying defaults, or running document hooks.

## Check text and number lists {#lists}

[`TextList` and `NumberList`](/docs/fields/lists/#live-validation) use the same opt-in API.
Their callbacks receive the complete ordered `[]string` or `[]float64`, including duplicates.
Return field-level issues; primitive list items do not have durable `_key` identities.
The admin discards outdated feedback after item edits or reorders and checks the current list.
