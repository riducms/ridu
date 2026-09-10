<!-- Generated from website/src/content/docs/hooks/fields.md by scripts/sync-agent-docs.ts. -->

# Field hooks

Field hooks run for one field value. Use them to trim a title, uppercase a product code, or format
what the API returns. They follow the field wherever you use it, including inside groups, arrays,
and blocks.

To supply an initial value only when a field is omitted, use
[`.DefaultFrom(...)`](../fields/defaults.md). Use a hook when existing or explicitly supplied
values also need to change.

New to `operation.Value[T]`, `Keep`, or `Replace`? [Operations and callbacks](../go-packages/operation.md)
explains what those values mean and how to return them from a field hook. For raw
`store.Value` data, see [Documents and values](../go-packages/store.md).

Choose the hook based on when the change should happen:

| Hook             | Receives                                        | Changes                                 |
| ---------------- | ----------------------------------------------- | --------------------------------------- |
| `BeforeValidate` | Raw input in `operation.Value[store.Value]`     | Input before built-in checks            |
| `BeforeChange`   | The field's Go type in `operation.Value[T]`     | The value to save                       |
| `AfterRead`      | The field's output type in `operation.Value[T]` | The response, leaving storage unchanged |

`BeforeValidate` is a shared stage and can also run on reads and deletes. Check `ctx.Operation`
when a rule should apply only to saves. For the full sequence, see
[when hooks run](./collections.md#lifecycle).

## Change a field before validation {#normalize-input}

Use a field's `BeforeValidate` hook to clean up input before Ridu validates it. This example trims
leading and trailing spaces from a title:

```go title="content/posts.go" focus={26-29,35-37}
package content

import (
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func trimText(
	_ operation.WriteContext,
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
	// Present wraps the new value; Replace applies it to this field.
	return operation.Replace(
		operation.Present(store.String(strings.TrimSpace(text))),
	), nil
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

Before validation, a submitted value might be missing or have the wrong type. That is why this
hook receives `store.Value` and checks both `present` and `valid`. `operation.Keep` leaves the
value alone so normal validation can handle it; `operation.Replace` supplies the trimmed value.
Submitting `"  Hello, Ridu  "` therefore stores `"Hello, Ridu"`.

`BeforeValidate` runs before field defaults. If the hook supplies a value for an omitted field,
the default is no longer needed and will not run for that field.

Use `ctx.Siblings` to read another field in the same group or row, `ctx.Root` for top-level
fields, and `ctx.Prior` for the saved values before the change. These are read-only; return
`operation.Replace(...)` to change this field. See
[Using other field values](../fields/callback-values.md) for nested fields, previous values,
translations, and related-document lookups.

To reject a value with a message shown beside the field, use
[custom validation](../fields/validation.md).

## Change a typed field before saving {#typed-field-hooks}

Use `BeforeChange` when you want to work with the field's Go type. For a text field, the value
is `operation.Value[string]`. This reusable SKU field converts a supplied value to uppercase:

```go title="content/sku.go" focus={18-21,24-26}
package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func uppercaseSKU(
	_ operation.WriteContext,
	value operation.Value[string],
) (operation.Change[string], error) {
	sku, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	// Replace updates this field; Ridu validates the result again.
	return operation.Replace(
		operation.Present(strings.ToUpper(sku)),
	), nil
}

var SKU = field.Text("sku").Required().Hooks(field.Hooks[string]{
	BeforeChange: []field.Transform[string]{uppercaseSKU},
})
```

Add `SKU` to a collection's `Fields`. Saving `ab-12` stores `AB-12`. The same hook works if you
put the field inside a group, array, or block. `Get()` reports whether a value is available;
check that result before using the value.

`BeforeChange` runs after defaults and the first built-in checks. It sees unchanged values from
an update along with changes from earlier hooks. Custom `.Validate(...)` rules run after write
hooks finish.

Field hooks change only their own value:

| Return value                                   | Effect                                                                   |
| ---------------------------------------------- | ------------------------------------------------------------------------ |
| `operation.Keep[string]()`                     | Keep the current text unchanged                                          |
| `operation.Replace(operation.Present("ABC"))`  | Replace it with `"ABC"`                                                  |
| `operation.Replace(operation.Empty[string]())` | Clear it; ordinary validation still applies                              |
| A non-nil `error`                              | Stop the operation; a replacement returned with the error is not applied |

Use the corresponding type for the field, such as `float64` for Number or `bool` for Checkbox.
[TextList and NumberList](../fields/lists.md#behavior) use `[]string` and `[]float64`;
one hook receives and can replace the whole list, rather than running once per item.
A required field cannot be saved empty just because a hook cleared it. Raw hooks such as
`BeforeValidate` use the same keep-or-replace pattern with `store.Value`.

Field event hooks such as `AfterChange` receive the value but return only an error. They cannot
replace it. See [`field.Hooks`](https://riducms.com/reference/field/hooks/),
[`field.Transform`](https://riducms.com/reference/field/transform/), and
[`field.Observer`](https://riducms.com/reference/field/observer/) for their types.

## Change a returned value without changing storage {#read-hooks}

Use `.ReadHooks(...)` and `AfterRead` to format a field for a response. This example displays a
code in uppercase while leaving its stored value alone:

```go title="content/display_code.go" focus={18-21,24-27}
package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func formatCode(
	_ operation.ReadContext,
	value operation.Value[string],
) (operation.Change[string], error) {
	code, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	// AfterRead replacements change the response, not storage.
	return operation.Replace(
		operation.Present(strings.ToUpper(code)),
	), nil
}

var DisplayCode = field.Text("displayCode").ReadHooks(
	field.ReadHooks[string]{
		AfterRead: []field.OutputTransform[string]{formatCode},
	},
)
```

Add `DisplayCode` to a collection and save `ab-12`. The response contains `AB-12`, but the database
still stores `ab-12`. Filtering and sorting use the stored value. Read hooks also affect the
admin and mutation responses, so use a before-save hook if you want the same normalized value
everywhere.

The replacement must fit the field's output type. A relationship's write hook receives an ID,
while its read hook receives `operation.ReferenceOutput`, which can include a populated document.
The read and write types are therefore separate. A required field cannot return explicit null.

Ridu removes unreadable fields after read hooks finish. A hook may see values that the caller
cannot read; do not copy protected data into a public field. A failing read hook rejects the
response and can still roll back a create or update that has not committed yet.

## Extend a reusable field {#reuse}

Add hooks when defining a field, then include that field in your collection, group, array, or block.
Each callback runs for the value at that location. It does not need an array index to know which
row it is editing.

`.Hooks(...)` replaces the field's existing hooks. Use `.AppendHooks(...)` to run new hooks after
its existing ones, or `.PrependHooks(...)` to run them first. In each list, hooks run in declaration
order. `.ReadHooks(...)` separately configures hooks that transform the response.

A field hook can change only its own value. Use a
[collection or global hook](./context.md#stored-and-returned-values) when a change needs
to update several fields together. See [Hook context](./context.md) for the arguments
available to each kind of hook.

## Show feedback before saving {#live-checks}

Attach `.LiveValidate(...)` to check a field while the author edits it. Live checks receive
unsaved input before defaults and the hooks above run. For example, a `BeforeValidate` hook that
trims a title has not run when its live check reads that title.

Keep the rule in `.Validate(...)` too so it runs when saving. The
[Live server validation guide](../fields/live-validation.md) shows how to share the rule between
both callbacks.

For large arrays, Blocks, or plugin-declared embedded content, continue with
[Hook and value performance](https://riducms.com/docs/performance/hooks-and-values/) to keep per-occurrence callbacks
linear and avoid copying immutable containers just to read them.
