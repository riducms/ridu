---
title: 'Select field'
description: 'Add a dropdown for one choice or a multi-select for several choices.'
product: core
eyebrow: 'Basic fields'
order: 68
aliases:
  [
    'field.Select',
    'select field',
    'dropdown',
    'multi-select',
    'Options'
  ]
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#Select',
    'go:github.com/riducms/ridu/field#Option',
    'go:github.com/riducms/ridu/field#MultiSelect'
  ]
navigation:
  section: 'Model content'
  parent: fields
  order: 80
  title: 'Select'
---

Use `field.Select` when authors must choose from a fixed list of options. It renders a dropdown for one choice.
Use `field.MultiSelect` for several choices. Generated TypeScript narrows input and output to
the configured string values instead of a free-form `string`.

## In the admin {#admin-behavior}

![An open multiple Status select in the Ridu admin showing In review and Published as selected choices.](../../../../../docs/assets/fields/select.png)

_The menu displays labels; stored and generated values use the configured option values._

## Add a dropdown {#example}

```go title="content/posts.go"
field.Select("status", "draft", "review", "published").Default("draft")
```

Pass strings directly for generated labels: `"in-review"` displays as “In review”, while the
stored value remains `"in-review"`. Use `.Options(...)` with `field.Option` for custom labels or
translations:

```go title="content/posts.go"
field.Select("visibility").Options(
	field.Option{
		Value:             "public",
		Label:             "Everyone",
		LabelTranslations: map[string]string{"fr": "Tout le monde"},
	},
	field.Option{Value: "members", Label: "Signed-in members"},
)
```

`.Options(...)` replaces the entire option list, including any constructor values. Omit an
option’s `Label` to generate it from `Value`. Reuse a typed slice with `.Options(options...)`,
where `options` is a `[]field.Option`; string slices work in the constructor with `values...`.

Values are stable data; labels and `LabelTranslations` are presentation. Changing `members` to
`registered` is a data/API migration, while changing its label is not.

## Configuration {#configuration}

| Constructor or method                                                 | What it controls                                                                |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| `field.Select(name, values...)`                                       | Stores one configured string and renders a dropdown.                            |
| `field.MultiSelect(name, values...)`                                  | Stores an ordered list of configured strings and renders a multi-select.        |
| `.Options(options...)`                                                | Replaces inferred options with explicit values, labels, and label translations. |
| `.Required()`                                                         | Requires one value, or a non-empty list for `MultiSelect`.                      |
| `.Default(...)` / `.DefaultFrom(callback)`                            | Supplies the initial choice or choices when a new scope omits the field.        |
| `.Localized()`                                                        | Stores a separate choice or list for each configured content locale.            |
| `.Unique()` / `.Index()`                                              | Available on singular `Select`; enforces uniqueness or adds a query index.      |
| `.Validate(...)`, `.LiveValidate(...)`, `.Access(...)`, `.Hooks(...)` | Adds application checks, authorization, and lifecycle behavior.                 |

## Allow several choices {#multiple}

```go title="content/posts.go"
field.MultiSelect("channels", "web", "email", "social").Default("web", "email")
```

Multi-selects store an ordered list of unique configured values. Its `Default` method accepts several strings. `Radio` cannot be multiple.

## Choose the initial selection {#defaults}

Use `.Default("draft")` for a fixed choice or `.DefaultFrom(callback)` to choose on the server.
For example, a callback could choose a review status from the signed-in user's role. Select and
Radio callbacks return `operation.Value[string]`; MultiSelect callbacks return
`operation.Value[[]string]`. Return option values such as `"draft"`, rather than display labels.

Defaults fill omitted selections; they preserve an author's explicit selection or empty input.
All returned options must be declared on the field. See
[Set default field values](/docs/fields/defaults/) for a complete callback and when it runs.

## Validate and translate choices {#options}

At least one non-empty unique option is required. Defaults must be in the option list. Select also
supports `Required`, localization, indexing/uniqueness where appropriate, conditions, and common
admin options. Query single choices with equality or inequality and multi-selects with the
list operators accepted by the generated SDK. Localizing the field stores different selected values per content
locale; translating option labels alone uses `Option.LabelTranslations`.

## Common mistakes {#troubleshooting}

- Do not use labels as API values. Submit the exact `Value` string.
- Adding a choice is additive; removing or renaming a value requires a migration for existing data.
- Use [Relationship](/docs/fields/relationship/) when choices are managed documents rather than a
  fixed list compiled into config.

See [`field.Select`](/reference/field/select/), [`SelectField.Options`](/reference/field/select-field-options-method/), and
[`field.MultiSelect`](/reference/field/multi-select/).
