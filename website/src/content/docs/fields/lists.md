---
title: 'Text and number lists'
description: 'Add editable lists of tags, sizes, or scores, with limits for each item and the whole list.'
product: core
eyebrow: 'Basic fields'
order: 67
aliases:
  [
    'field.TextList',
    'field.NumberList',
    'primitive lists',
    'tags',
    'list of strings',
    'list of numbers',
    'hasMany text',
    'hasMany number'
  ]
relatedSymbolIds:
  - 'go:github.com/riducms/ridu/field#TextList'
  - 'go:github.com/riducms/ridu/field#NumberList'
navigation:
  section: 'Model content'
  parent: fields
  order: 65
  title: 'Text and number lists'
---

Use `field.TextList` when authors can enter several text values, such as tags or selling points.
Use `field.NumberList` for several numbers, such as available sizes or scores. The API returns
ordinary arrays: `["new", "sale"]` or `[8, 10, 12]`.

## Add tags and sizes {#example}

Define the fields in `content/products.go` and add `Catalog` to `ridu.Config.Collections`:

```go title="content/products.go" focus={8-10,17-19}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Tags = field.TextList("tags").
	MinLength(1).MaxLength(30). // Limit each tag's length.
	MaxRows(8)                  // Allow up to eight tags.

var Catalog = ridu.Collection{
	Slug: "products",
	Fields: field.Fields{
		field.Text("name").Required(),
		Tags,
		field.NumberList("availableSizes").
			Min(0).Max(50). // Limit each size's value.
			MaxRows(20),    // Allow up to twenty sizes.
	},
}

// ... optional tag validation below
```

A saved product looks like this:

```json title="document.json"
{
	"name": "Canvas trainers",
	"tags": ["new", "sale"],
	"availableSizes": [8, 10, 12]
}
```

The admin lets authors add, edit, remove, and reorder items. Use the arrow buttons, or press
**Alt + Up** or **Alt + Down** while an item is focused. **Add item** becomes unavailable at
`MaxRows`. See [editing and translating lists](#admin-behavior) for empty number inputs and locales.

## Configuration {#configuration}

| Constructor or method | Text list                                                  | Number list                                                 |
| --------------------- | ---------------------------------------------------------- | ----------------------------------------------------------- |
| Constructor           | `field.TextList(name)`                                     | `field.NumberList(name)`                                    |
| Item bounds           | `.MinLength(n)` / `.MaxLength(n)`                          | `.Min(value)` / `.Max(value)`                               |
| List bounds           | `.MinRows(n)` / `.MaxRows(n)`                              | `.MinRows(n)` / `.MaxRows(n)`                               |
| Initial list          | `.Default(values...)` / `.DefaultFrom(callback)`           | `.Default(values...)` / `.DefaultFrom(callback)`            |
| Requiredness          | `.Required()` requires a present, non-empty list           | `.Required()` requires a present, non-empty list            |
| Localization          | `.Localized()` stores a list per locale                    | `.Localized()` stores a list per locale                     |
| Validation            | `.Validate(...)` / `.LiveValidate(...)` receive `[]string` | `.Validate(...)` / `.LiveValidate(...)` receive `[]float64` |
| Lifecycle             | `.Hooks(...)` and `.AfterRead(...)` receive the whole list | `.Hooks(...)` and `.AfterRead(...)` receive the whole list  |

## Choose the right list {#choosing-a-list}

| You need                              | Use                                 | Example value                                         |
| ------------------------------------- | ----------------------------------- | ----------------------------------------------------- |
| Text the author can freely enter      | `TextList`                          | `["new", "sale"]`                                     |
| Numbers the author can freely enter   | `NumberList`                        | `[8, 10, 12]`                                         |
| Several choices from a predefined set | [MultiSelect](/docs/fields/select/) | `["small", "large"]`                                  |
| Several fields per item               | [Array](/docs/fields/array/)        | `[{"_key":"a", "label":"Docs", "url":"/docs"}]`       |
| Different kinds of item in one list   | [Blocks](/docs/fields/blocks/)      | `[{"_key":"a", "blockType":"quote", "text":"Hello"}]` |

Text and number lists preserve order and duplicates. Ridu does not trim text or sort the list.
Each item is a value, without its own fields or saved `_key`. Choose Array when each item needs
its own label, image, or other child fields.

## Limit items and list length {#constraints}

Despite their names, `MinRows` and `MaxRows` count items in these lists. They do not create
object rows. The other limits apply separately to every item:

| Option                             | Applies to     | Example                                                              |
| ---------------------------------- | -------------- | -------------------------------------------------------------------- |
| `.Required()`                      | Whole list     | Rejects missing, null, and empty lists                               |
| `.MinRows(2)`                      | Whole list     | Requires at least two items, even if the field is otherwise optional |
| `.MaxRows(8)`                      | Whole list     | Allows at most eight items; `0` means no maximum                     |
| `.MinLength(1)` / `.MaxLength(30)` | Each text item | Allows 1–30 Unicode code points per string                           |
| `.Min(0)` / `.Max(50)`             | Each number    | Allows values from 0 through 50, inclusive                           |

**Required does not mean every string is nonempty.** `TextList("tags").Required()` accepts
`[""]`; add `.MinLength(1)` to reject an empty tag. Whitespace is preserved, so a space also
counts toward the length. Numeric zero is a valid value unless a bound excludes it.

Number lists accept JSON numbers, including decimals. `[8, 10]` is valid; `["8", "10"]` is not.
Null items, objects, mixed types, `NaN`, and infinity are rejected. NumberList has no `Step`
option; use a custom validator if you need whole numbers or particular increments.

## Set defaults and clear a list {#defaults}

```go
field.TextList("tags").Default("new", "sale")
field.NumberList("availableSizes").Default(8, 10, 12)
// Start with a present, empty list rather than no value.
field.TextList("credits").Default()
```

Defaults fill eligible omitted values. They do not replace explicit `null` or `[]`, and they
must satisfy the same item and count limits. For defaults based on the user or locale, use
[`.DefaultFrom(...)`](/docs/fields/defaults/#supported-fields) with `operation.Value[[]string]`
or `operation.Value[[]float64]`.

### Send an empty value or replace the list {#presence}

| Input             | Optional list with no positive `MinRows` | `.Required()` or `.MinRows(1)`                  |
| ----------------- | ---------------------------------------- | ----------------------------------------------- |
| Omitted on create | No value, unless a default supplies one  | Rejected unless a default supplies enough items |
| `null`            | Clears the value                         | Rejected                                        |
| `[]`              | Saves an empty list                      | Rejected                                        |
| Omitted on update | Keeps the saved list                     | Keeps the saved list                            |
| A list on update  | Replaces every item                      | Replaces every item, then checks the limits     |

For example, updating `["new", "sale"]` with `["featured"]` leaves only `"featured"`.
Send the complete desired list when adding or removing an item through the API.

A missing value and `null` may be returned in the same empty form, depending on storage.
An explicit `[]` remains an empty array. With locale fallback, an empty array counts as a
translation; it does not fall back to another locale's list.

## Add a rule for the whole list {#behavior}

Built-in lists allow duplicates. To reject repeated tags in this product, import `strings` and
`github.com/riducms/ridu/operation` in `content/products.go`, then add the following below
`Catalog`. Replace `Tags` with `UniqueTags` in the collection’s `Fields` list:

```go title="content/products.go" focus={3,15-24}
// ... imports, Tags, and Catalog shown above

var UniqueTags = Tags.Validate(noRepeatedTags)

func noRepeatedTags(
	_ operation.Context,
	value operation.Value[[]string],
) ([]operation.Issue, error) {
	tags, present := value.Get()
	if !present {
		return nil, nil // This optional field can be left empty.
	}
	seen := make(map[string]bool)
	for _, tag := range tags {
		// Compare without case; keep the author's original spelling.
		key := strings.ToLower(tag)
		if seen[key] {
			return []operation.Issue{{
				Code:    "duplicate_tag",
				Message: "Each tag must be unique, regardless of case.",
			}}, nil
		}
		seen[key] = true
	}
	return nil, nil
}
```

Entering `"Sale"` and `"sale"` now shows **Each tag must be unique, regardless of case.**
The callback compares lowercase versions but keeps the original text. Returning `nil, nil`
means the list passes this rule.

A TextList validator receives `operation.Value[[]string]`; NumberList receives
`operation.Value[[]float64]`. Hooks and read hooks use the same slice types. Each callback
receives the **whole list**, rather than running once for every item.

Return an issue on the list field by leaving its target unset, as above. Built-in errors name
the invalid item's position, starting at 1. These positions can change when the author
reorders the list; they are not item IDs. See [custom validation](/docs/fields/validation/)
for callback arguments and issue handling.

### Reuse a configured list

`Tags.Validate(noRepeatedTags)` returns a new field definition. It leaves `Tags` unchanged,
including its existing length and count limits. Use `.Rename("keywords")` to reuse a definition
under another field name. [Field factories](/docs/fields/#field-access-and-hooks) can return
`field.TextListField` or `field.NumberListField` just like other concrete field types.

## Check a list while editing {#live-validation}

Use `.LiveValidate(...)` for [live server feedback](/docs/fields/live-validation/). The callback
receives `operation.LiveValidationContext` and the same whole-list value used by save validation:
`operation.Value[[]string]` or `operation.Value[[]float64]`.

Register `.Validate(...)` as well when the rule must prevent a save. A live check alone only
provides feedback. Return issues on the list field; there are no per-item issue targets.
Editing, removing, or reordering an item clears outdated live feedback and checks the current
list. An unfinished number pauses live checking until it can be sent as a number.

These rules also apply when a list is inside a Group, Array, Block, or rich-text editor.
`.ReplaceLiveValidators()` removes live checks while keeping save validators in place.

## Edit and translate lists {#admin-behavior}

A newly added number item starts blank. Enter a valid number or remove the item before saving.
Blank or unfinished numeric input, such as a lone minus sign, is not silently converted to zero
or dropped. It prevents Save and an embedded editor's **Apply** action.

Add `.Localized()` to give each locale its own complete list and order. Locale fallback chooses
one list; it does not merge individual items. Lists can be nested in Groups, Arrays, Blocks,
and rich-text blocks. In an embedded editor, **Apply** copies the list into the parent form;
**Cancel** discards those edits.

For a [custom field component](/docs/custom-components/field-components/), register an editor
with `type: "text-list"` or `type: "number-list"`. `FieldEditorProps<"text-list">` reads and
writes `string[]`; `FieldEditorProps<"number-list">` uses `number[]`. Call `field.set(...)`
with the new complete array and display `field.issues`. The framework handles live feedback
and the usual read-only and field access rules.

## Find documents containing a value {#queries}

Use the `in` operator to find a document containing any of the supplied values. For example,
this SDK filter matches products tagged `"sale"` **or** `"featured"`:

```json
{ "tags": { "in": ["sale", "featured"] } }
```

Matching is exact: `"sale"` does not
match `"wholesale"`. Order and duplicates do not affect membership. Use `query.Not(...)` to
exclude matches, or combine two `In` filters with `query.And(...)` to require both tags.
See [querying lists](/docs/querying/#primitive-list-membership) for a complete Go example.

Existence and null checks are also supported. Whole-list equality, scalar equality, substring
search, range comparisons, sorting, indexes, and uniqueness are unavailable for these fields.
Reject duplicate items with a validator; there is no `.Unique()` list option.

### Query nested lists {#nested-queries}

| Adapter    | Supported list-query locations                                                                                             |
| ---------- | -------------------------------------------------------------------------------------------------------------------------- |
| PostgreSQL | Root and Group fields, and paths through one enclosing Array or Block                                                      |
| SQLite     | Root and nested fields using its normal query paths                                                                        |
| MongoDB    | Root and Group fields, including localized list fields; paths through Arrays, Blocks, or localized containers are rejected |

All three adapters can **store** lists at those nested locations. The restrictions above apply
to querying them. See [generated contracts](/docs/generated-contracts/#primitive-lists) for Go,
TypeScript, and GraphQL list types.
