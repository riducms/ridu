<!-- Generated from website/src/content/docs/fields/validation.md by scripts/sync-agent-docs.ts. -->

# Custom validation

Use `.Validate(...)` when a field needs a rule beyond built-in options such as `.Required()` or
`.MinLength(...)`. A validator checks a value before Ridu saves it and returns messages the
author can act on. The same rule applies to saves from the admin, REST, the SDK, and the Go API.

For messages while someone is typing, see [Live server validation](./live-validation.md).
Register both `.Validate(...)` and `.LiveValidate(...)` when the rule should run at both times.

[Operations and callbacks](../go-packages/operation.md) explains `operation.Value[T]`,
`operation.Issue`, and the context passed to your validator, including what an empty value means.

## Add a rule to a field {#add-rule}

This example accepts only links that start with `https://`. Create `content/links.go`:

```go title="content/links.go" focus={10,17-26}
package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var Link = field.Text("url").Required().Validate(validateHTTPS)

func validateHTTPS(
	// This rule only needs the value, so Go's _ ignores the context.
	_ operation.Context,
	value operation.Value[string],
) ([]operation.Issue, error) {
	url, present := value.Get()
	// Leave missing or empty values to Required; HTTPS values pass.
	if !present || url == "" || strings.HasPrefix(url, "https://") {
		return nil, nil
	}
	// Return a message the author can fix beside this field.
	return []operation.Issue{{
		Code:    "https_required",
		Message: "Use a URL that starts with https://",
	}}, nil
}
```

Add `Link` to your collection's `Fields` list. Ridu gives a text validator an
`operation.Value[string]`; `Get()` returns the string and whether a value is present.
This rule leaves empty values to `.Required()` and checks the prefix when a URL is supplied.
It does not check whether the destination exists.

Run `bun run dev`, open the collection, and save a document with `http://example.com` as its URL.
The form should show “Use a URL that starts with https://”. Change it to `https://example.com`
and save again. The rule should pass.

You can reuse `Link` inside a Group, Array, or Block. The validator still receives that URL's
value, and Ridu displays its message beside the correct input.

## Return a useful message {#issues}

Return `nil, nil` when the value passes. Otherwise, return one or more `operation.Issue` values:

- `Code` is a short, stable name that API clients can recognize, such as `https_required`.
- `Message` tells the author what to fix.
- `Target` is optional. Leave it out to put the message on this field.

The separate `error` return is for a failure that prevents the check from running, such as an
unavailable external service. Use an issue for an invalid value the author can correct.

Save validators receive the values that would be saved, after field transformations and after an
update has been combined with the existing document. Each validator runs once per operation
attempt. It can read other values through its context; see
[Using other field values](./callback-values.md).

To change a value before saving, use a [field hook](../hooks/fields.md#normalize-input).
Validators report problems; they do not change the document.

## Put a message on a child field {#validation-issue-targets}

Attach a validator directly to the child when the rule only concerns that child. A validator on
a Group, Array, or Blocks field is useful when the rule compares several children or rows.
Its `Target` can identify the child that needs attention.

For example, a rule on the `details` group can point to its nested SEO title:

```go focus={4-5}
operation.Issue{
	Code:    "title_required",
	Message: "Add an SEO title before publishing",
	// Start inside the group this validator is attached to.
	Target: operation.At("seo.title"),
}
```

The target starts inside `details`, so this message belongs to `details.seo.title`.
The `seo` object must exist. If the object itself is missing or invalid, use
`operation.At("seo")` to put the message on the group instead.

### Point to an array or block row {#rows}

Use the row's `_key` when a rule on a container needs to identify a row. Ridu gives each row a
key so the message can follow it when rows are reordered.

| Field being validated               | Target                                                                    | Where the message appears            |
| ----------------------------------- | ------------------------------------------------------------------------- | ------------------------------------ |
| `sections` array                    | `operation.At().Row(sectionKey).Field("heading")`                         | The chosen section's heading         |
| `sections` array, with nested links | `operation.At().Row(sectionKey).Field("links").Row(linkKey).Field("url")` | The chosen link's URL                |
| `layout` blocks                     | `operation.At().Block(heroKey, "hero").Field("heading")`                  | The heading in the chosen Hero block |

Read keys from the rows the validator receives, including newly added rows. Do not use an array
index as a key. `Block` also takes the expected block type, such as `"hero"`.
Use `Field` for a direct child or a dotted path through groups; use `Row` or `Block` each time
the path enters a repeated list.

Ridu reports an error if a target names an unknown child, a missing or duplicate row key, or a
different block type. Leave `Target` unset to put the message on the container itself.
The admin keeps server messages on unchanged values after reordering and removes messages whose
value or row has changed.

### Validate translations and rich-text blocks {#locales-and-embeds}

A validator editing a French value already works in French. Use the same targets as above;
you do not need to include a locale.

If the validator's value contains all translations, select one explicitly:
`operation.At("copy").Locale("fr").Field("title")`. That translation must exist. A target
cannot request a fallback translation or select a different locale from a callback already
working in one exact locale.

Arrays inside rich-text blocks use the same targets. Ridu supplies the surrounding rich-text
location, so your validator does not need to know how the editor stores its document.

For target methods and types, see the [operation API reference](https://riducms.com/reference/operation/).

## Get feedback before saving {#live-validation}

Use `.LiveValidate(...)` when an author should see a server check while editing. Keep
`.Validate(...)` registered to enforce the rule on every save. Live feedback does not disable
Save or replace save validation.

The [Live server validation guide](./live-validation.md) walks through a complete
supplier/SKU example and explains when the admin checks a field.

### Read unsaved input {#live-context}

Live checks receive `operation.LiveValidationContext`: values may be incomplete, and defaults
and save hooks have not run. See [callback arguments](./live-validation.md#live-context).

### Understand when feedback appears {#live-timing}

Checks start after editing a field and wait 500 ms after the last edit. See
[timing and feedback](./live-validation.md#live-timing) for retries, skipped checks,
and how Save handles pending work.

### Validate embedded fields {#live-embedded}

Rich-text blocks and plugin embedded forms also support live checks. See
[rows and embedded forms](./live-validation.md#live-embedded) for context and Apply behavior.

### Build a custom client {#live-transport}

The admin handles requests automatically. For a separate application, see the
[SDK example](../typescript-sdk.md#live-validation) and
[permissions and limits](./live-validation.md#live-transport).

## Validate a text or number list {#lists}

[TextList and NumberList](./lists.md#behavior) validators receive the whole list as
`operation.Value[[]string]` or `operation.Value[[]float64]`. Use them for rules that compare
items, such as rejecting duplicate tags. Item length and number bounds can use the built-in
field options instead.

Return an issue on the list field. Items do not have their own issue targets or stable row
keys. A position named in a message refers to the submitted order, starting at 1.
