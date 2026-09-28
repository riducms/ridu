<!-- Generated from website/src/content/docs/hooks/fields.md by scripts/sync-agent-docs.ts. -->

# Field hooks

Field hooks run for one field's value. Use them to trim a title, uppercase a product code, or
format what the API returns. A field keeps its hooks wherever you use it, including inside groups,
arrays, and blocks. When a change involves several fields, use a
[collection hook](./collections.md) instead.

A field has two places for hooks. `.Hooks(...)` holds the hooks that work with the stored value,
and `.AfterRead(...)` holds the hooks that change the value Ridu returns:

```go
field.Text("title").
	// Hooks that work with the value Ridu stores.
	Hooks(field.Hooks[string]{
		BeforeValidate: []field.RawTransform{cleanInput},
		BeforeChange:   []field.Transform[string]{formatValue},
		AfterCommit:    []field.Observer[string]{notifyChange},
	}).
	// Hooks that change the value Ridu returns.
	AfterRead(formatForDisplay)
```

`T` in `field.Hooks[T]` is the Go type of the stored value: `string` for a Text field. `AfterRead`
is a method rather than a `Hooks` entry, because some fields return a different type from the one
they store. [Why AfterRead is registered separately](#after-read-registration) explains the
difference.

To supply a value only when a field is left empty, use a
[default value](../fields/defaults.md) instead of a hook.

## Hook arguments {#arguments}

Every field hook receives two arguments: `ctx`, which describes the operation, and `value`, this
field's current value.

```go
func formatValue(
	ctx operation.Context,
	value operation.Value[string],
) (operation.Change[string], error) {
	text, present := value.Get()
	// ...
}
```

| Argument        | Description                                                                                                            |
| --------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `value`         | This field's value. `value.Get()` returns the value and whether one is present.                                        |
| `ctx.Operation` | What is happening, such as `operation.Create`, `operation.Update`, or `operation.Read`.                                |
| `ctx.ID`        | The document's ID. On a create it is empty until the document is saved; on a duplicate it is the copy's ID once saved. |
| `ctx.Actor.ID`  | The signed-in user's ID, or empty for an anonymous request. `ctx.Actor.Data` holds the user's fields.                  |
| `ctx.Siblings`  | The fields next to this one: the same group, the same array row, or the top level. Includes this field.                |
| `ctx.Root`      | The document's top-level fields.                                                                                       |
| `ctx.Prior`     | The saved values of this field's group or row before the operation. Empty on create.                                   |
| `ctx.Locale`    | The content locale, or empty without [localization](../localization.md).                                              |
| `ctx.Local`     | Reads another document: `ctx.Local.FindByID(ctx.Context, "users", id)`. It cannot write.                               |
| `ctx.Context`   | Cancellation and deadline. Pass it to `ctx.Local` and network calls.                                                   |

Read another field with `ctx.Siblings.String("title")`, or
`ctx.Siblings.Get("price").NumberValue()` for other types. These views are read-only: a hook
changes only its own field, through its return value. [Operations and callbacks](../go-packages/operation.md)
explains `operation.Value` and `operation.Context` in more detail, and
[Using other field values](../fields/callback-values.md) covers nested rows and translations.

## Return values {#return-values}

Field hooks come in two kinds:

- **Transforms** can change the value. They return an `operation.Change[T]` and an error:
  `BeforeValidate`, `BeforeDuplicate`, `BeforeChange`, `BeforeOperation`, and `AfterRead`.
- **Observers** react to the value. They return only an error: `AfterChange`, `BeforeDelete`,
  `AfterDelete`, `AfterOperation`, and `AfterCommit`.

A transform returns one of these:

| Return value                | Effect                                                              |
| --------------------------- | ------------------------------------------------------------------- |
| `operation.Keep[string]()`  | Keep the current value                                              |
| `operation.Set("ABC")`      | Set it to `"ABC"`                                                   |
| `operation.Clear[string]()` | Clear it; `Required()` and other validation still apply             |
| A non-nil `error`           | Stop the operation; a change returned with the error is not applied |

An error from any hook that runs before the transaction commits stops the operation and rolls
back its writes. Return `ridu.Reject("…")` to tell the editor why: the admin shows the message,
and an `operation.Issue` passed to it marks a field, with its `Target` starting at this field. Any
other error is reported as an internal error. An error from `AfterCommit` cannot undo the save.

## BeforeValidate {#before-validate}

<span id="normalize-input"></span>

Runs before Ridu checks the value when a document is created, duplicated, updated, published, or
unpublished. The input may be missing or have the wrong type, so the hook receives the raw input
as a `store.Value`. Use it to clean up input before validation.

```go title="content/posts.go" focus={26-27,33-35}
package content

import (
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func trimText(
	_ operation.Context,
	value operation.Value[store.Value],
) (operation.Change[store.Value], error) {
	raw, present := value.Get()
	if !present {
		// An update may omit this field; leave it unchanged.
		return operation.Keep[store.Value](), nil
	}
	text, valid := raw.StringValue()
	if !valid {
		// Let Ridu validate values that are not strings.
		return operation.Keep[store.Value](), nil
	}
	// Set replaces this field's value with the trimmed text.
	return operation.Set(store.String(strings.TrimSpace(text))), nil
}

var Posts = ridu.Collection{
	Slug: "posts",
	Fields: field.Fields{
		field.Text("title").Required().Hooks(field.Hooks[string]{
			BeforeValidate: []field.RawTransform{trimText},
		}),
	},
}
```

Submitting `"  Hello, Ridu  "` stores `"Hello, Ridu"`. A value that is not a string is left for
Ridu's own validation to reject, and an update that does not send the title leaves it alone.

## BeforeChange {#before-change}

<span id="typed-field-hooks"></span>

Runs after the built-in checks and before the value is saved, for create, duplicate, update,
publish, and unpublish. The value has the field's Go type: `operation.Value[string]` for a Text
field. Use it to change a valid value before saving.

```go title="content/sku.go" focus={18-19,22-24}
package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func uppercaseSKU(
	_ operation.Context,
	value operation.Value[string],
) (operation.Change[string], error) {
	sku, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	// Set changes this field; Ridu validates the result again.
	return operation.Set(strings.ToUpper(sku)), nil
}

var SKU = field.Text("sku").Required().Hooks(field.Hooks[string]{
	BeforeChange: []field.Transform[string]{uppercaseSKU},
})
```

Add `SKU` to a collection's `Fields`. Saving `ab-12` stores `AB-12`. On an update, the hook
receives the saved value when the caller did not send this field. Your
[custom `.Validate(...)` rules](../fields/validation.md) run after this hook.

## AfterRead {#after-read}

<span id="read-hooks"></span>

Runs for each document Ridu returns, including the responses to creates and updates. It changes
the response without changing what is stored. Register it with the field's `.AfterRead(...)`
method.

```go title="content/display_code.go" focus={18-19,22}
package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func formatCode(
	_ operation.Context,
	value operation.Value[string],
) (operation.Change[string], error) {
	code, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	// In an AfterRead hook, Set changes the response, not storage.
	return operation.Set(strings.ToUpper(code)), nil
}

var DisplayCode = field.Text("displayCode").AfterRead(formatCode)
```

Save `ab-12`, and the response contains `AB-12` while the database still stores `ab-12`. Filters
and sorting use the stored value, and the admin shows the returned one. To store the uppercase
value everywhere, use `BeforeChange` instead.

Ridu removes fields the caller cannot read after this hook runs. A hook may see values that the
caller is not allowed to read, so do not copy them into a field the caller can read.

### Relationship and upload fields {#reference-read-hooks}

A relationship stores a document ID but returns an `operation.ReferenceOutput`, which also holds
the related document when the request asks for it. That is why its two kinds of hooks receive
different types:

```go title="content/reviewer.go" focus={12,25,43-47}
package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

// Write hooks receive the stored value: the reviewer's document ID.
func clearSelfReview(
	ctx operation.Context,
	value operation.Value[operation.ID],
) (operation.Change[operation.ID], error) {
	id, present := value.Get()
	if !present || ctx.Actor.ID == "" || id != ctx.Actor.ID {
		return operation.Keep[operation.ID](), nil
	}
	// Authors cannot review their own work; save it unreviewed.
	return operation.Clear[operation.ID](), nil
}

// Read hooks receive the response value, which may be the populated reviewer.
func showReviewerName(
	_ operation.Context,
	value operation.Value[operation.ReferenceOutput],
) (operation.Change[operation.ReferenceOutput], error) {
	reference, present := value.Get()
	if !present {
		return operation.Keep[operation.ReferenceOutput](), nil
	}
	reviewer, populated := reference.Document()
	if !populated {
		// An unpopulated response holds only the ID; leave it alone.
		return operation.Keep[operation.ReferenceOutput](), nil
	}
	// Return the reference with only the reviewer's name.
	return operation.Set(operation.Populated(store.Document{
		ID:     reviewer.ID,
		Values: store.Values{"name": reviewer.Values["name"]},
	})), nil
}

var Reviewer = field.Relationship("reviewer", "users").
	Hooks(field.Hooks[operation.ID]{
		BeforeChange: []field.Transform[operation.ID]{clearSelfReview},
	}).
	AfterRead(showReviewerName)
```

An author who picks themself as the reviewer saves the document without a reviewer. A read that
populates `reviewer` returns only the reviewer's ID and name. A read without population returns
the ID unchanged.

## BeforeDuplicate {#before-duplicate}

Runs when a document is duplicated, before `BeforeValidate`. It receives the copied value as a
raw `store.Value`. Use it for values that must not be copied as they are.

Ridu already gives a copied [`field.Slug`](https://riducms.com/docs/fields/slug/) the next free value, such as
`about-us-copy`. Other unique fields need a hook, or the copy fails with a `unique` error on that
field. This field links a product to Stripe, and a copy must not claim the same Stripe product:

```go title="content/external_id.go" focus={13-14,19}
package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func unlinkCopy(
	_ operation.Context,
	_ operation.Value[store.Value],
) (operation.Change[store.Value], error) {
	// The copy is a new product that Stripe does not know about yet.
	return operation.Clear[store.Value](), nil
}

var StripeProductID = field.Text("stripeProductId").Unique().Hooks(
	field.Hooks[string]{
		BeforeDuplicate: []field.RawTransform{unlinkCopy},
	},
)
```

Duplicating a product saves the copy without a `stripeProductId`, ready to be linked to a new
Stripe product.

## AfterCommit {#after-commit}

Runs after the transaction commits, for every change, including deletes and restores, but never
after a read. The value is already saved, so an error here cannot undo it. Use it for side effects
that should only happen after a successful save, such as logging a change or notifying another
service.

```go title="content/status.go" focus={15-17,20-22}
package content

import (
	"log"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func logStatusChange(
	ctx operation.Context,
	value operation.Value[string],
) error {
	status, _ := value.Get()
	// Prior holds the values saved before this operation.
	previous, _ := ctx.Prior.String("status")
	if status == previous {
		return nil
	}
	// The change is committed, so this never logs a rolled-back save.
	log.Printf("status %q -> %q by user %q",
		previous, status, ctx.Actor.ID)
	return nil
}

var Status = field.Select("status", "draft", "review", "approved").
	Hooks(field.Hooks[string]{
		AfterCommit: []field.Observer[string]{logStatusChange},
	})
```

Changing a page's status from `draft` to `review` logs one line. Saving the page without changing
its status, or reading it, logs nothing. Field `AfterCommit` hooks run before the collection's
`AfterCommit` hooks.

## Other field hooks {#other-hooks}

These hooks follow the same patterns as the examples above:

| Hook              | Kind      | When it runs                                                                                                              |
| ----------------- | --------- | ------------------------------------------------------------------------------------------------------------------------- |
| `BeforeOperation` | Transform | Just before the database call, for every operation, including reads and deletes. On a save, it runs after `BeforeChange`. |
| `AfterChange`     | Observer  | After a create, duplicate, update, publish, or unpublish is saved, before commit. An error rolls back the save.           |
| `BeforeDelete`    | Observer  | Before a document moves to the trash or is deleted permanently. It receives the saved value. An error stops the delete.   |
| `AfterDelete`     | Observer  | After the delete, before commit. An error restores the document.                                                          |
| `AfterOperation`  | Observer  | After the database call for every operation, including reads and deletes, before commit.                                  |

`BeforeOperation` and `AfterOperation` also run for reads, where the value can be empty. Check
`ctx.Operation` when they should act only on saves. The
[collection hook order](./collections.md#save-order) shows where each field hook runs.

## Types {#types}

The type of `value` depends on the field and on whether the hook works with the stored value or
the returned one:

| Field                                                            | Stored value in `field.Hooks[T]` | Returned value in `.AfterRead(...)` |
| ---------------------------------------------------------------- | -------------------------------- | ----------------------------------- |
| Text, Textarea, Email, Select, Date                              | `string`                         | `string`                            |
| Number                                                           | `float64`                        | `float64`                           |
| Checkbox                                                         | `bool`                           | `bool`                              |
| TextList, NumberList                                             | `[]string`, `[]float64`          | `[]string`, `[]float64`             |
| Relationship, Upload                                             | `operation.ID`                   | `operation.ReferenceOutput`         |
| Relationships, Uploads                                           | `[]operation.ID`                 | `[]operation.ReferenceOutput`       |
| Group, Array, Blocks, JSON, Point, and polymorphic relationships | `store.Value`                    | `store.Value`                       |
| Join, Virtual                                                    | No stored value                  | `store.Value`                       |

`BeforeValidate` and `BeforeDuplicate` always receive a raw `store.Value`, because they run
before Ridu has checked the type. A list field's hook receives the whole list at once, not one
item at a time. The function types are
[`field.RawTransform`](https://riducms.com/reference/field/raw-transform/),
[`field.Transform`](https://riducms.com/reference/field/transform/),
[`field.OutputTransform`](https://riducms.com/reference/field/output-transform/), and
[`field.Observer`](https://riducms.com/reference/field/observer/).

### Why AfterRead is registered separately {#after-read-registration}

Writing `field.Hooks[string]{AfterRead: ...}` fails to compile with
`unknown field AfterRead in struct literal`. Every list in `field.Hooks[T]` works with the stored
type `T`, but a field's returned value can have a different type: a relationship stores an
`operation.ID` and returns an `operation.ReferenceOutput`. Join and Virtual fields store nothing
at all, so they have `.AfterRead(...)` but no `.Hooks(...)`.

For most fields the two types are the same, so one function can be used as a `BeforeChange`
transform or with `.AfterRead(...)`. Where you register it decides when it runs.

## Reuse and extend a field {#reuse}

Define a field with its hooks once, then use it in any collection, group, array, or block. Each
hook runs for the value at that location, so it never needs an array index.

To add hooks to an existing field, use these builders. They return a new field and never modify
the one you started from:

| Stored-value hooks (`field.Hooks[T]`)          | Returned-value hooks                   |
| ---------------------------------------------- | -------------------------------------- |
| `.Hooks(...)` replaces all of them             | `.ReplaceAfterRead(...)` replaces them |
| `.AppendHooks(...)` adds after existing ones   | `.AfterRead(...)` adds after them      |
| `.PrependHooks(...)` adds before existing ones |                                        |

Hooks in the same list run in the order they were added. `.ReplaceAfterRead()` with no arguments
removes the returned-value hooks.

## Compared with collection hooks {#compared-with-collections}

Collection hooks receive the whole document through `ctx.Data` and return only an error. Field
hooks receive one value and return a change for it. Most phases exist in both, with these
differences:

| Phase             | Collection or global         | Field                                |
| ----------------- | ---------------------------- | ------------------------------------ |
| `BeforeRead`      | `CollectionHooks.BeforeRead` | Not available                        |
| `AfterRead`       | `CollectionHooks.AfterRead`  | The field's `.AfterRead(...)` method |
| `AfterError`      | `CollectionHooks.AfterError` | Not available; use the resource hook |
| Every other phase | A `CollectionHooks` list     | A `field.Hooks[T]` list              |

In a shared phase, collection hooks run before field hooks, except `AfterCommit`, where field
hooks run first.

## Live checks run before these hooks {#live-checks}

A `.LiveValidate(...)` check runs while an author edits, on unsaved input, before any of these
hooks. A title that a `BeforeValidate` hook would trim is still untrimmed when its live check
reads it. See [Live server validation](../fields/live-validation.md).

For large arrays and blocks, [Hook and value performance](https://riducms.com/docs/performance/hooks-and-values/)
explains how to keep hooks that run once per row fast.
