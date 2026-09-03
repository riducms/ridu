---
title: 'Text field'
description: 'Store a short string with typed length, uniqueness, localization, and admin options.'
product: core
eyebrow: 'Scalar and choice fields'
order: 61
aliases: ['field.Text', 'text input', 'string field']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#Text']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Scalar & choice'
  order: 10
  title: 'Text'
---

Use `field.Text` for a short, unformatted string: a title, name, reference code, or single-line
summary. It produces a `string` in generated Go, TypeScript, REST, and OpenAPI contracts and a
single-line control in the admin.

## In the admin {#admin-behavior}

![A populated Title text field in the Ridu admin, with its required marker and field description.](../../../../../docs/assets/fields/text.png)

_Changing its label affects only the editor; changing `title` changes the stored and API path._

## Smallest working example {#example}

```go title="content/posts.go"
field.Text("title", field.Required())
```

The document property is `title`. Authors see a label derived from that name unless you provide
`field.Label("Post title")`.

## Validation and options {#options}

```go title="content/products.go"
field.Text(
	"sku",
	field.Required(),
	field.MinLength(3),
	field.MaxLength(32),
	field.Unique(),
	field.Index(),
	field.Description("The identifier used by fulfilment."),
)
```

`MinLength` and `MaxLength` count the submitted string. `Required` rejects missing, null, and empty
values. `Unique` is collection-wide and should describe a real business invariant; `Index` is for a
field you regularly filter or sort. `Default`, `Localized`, `ReadOnly`, `Hidden`,
`Sidebar`, `Columns`, conditions, labels, descriptions, placeholders, and a paired
`AdminComponent` are also compatible.

Presentation options do not grant access. Protect sensitive strings with field access rules even
when the admin hides or disables the control.

## Querying and localization {#querying}

Text fields support string operators such as equality, containment, and `like`, plus sorting and
selection. Use the canonical field path (`title` or `seo.title`) in Local API and REST queries; the
generated SDK exposes that path in its `where` type.

`field.Localized()` stores one value per configured content locale. Required and unique checks are
evaluated per locale, and reads obey the request's locale and fallback chain. Do not add
`Localized` merely to translate the admin label—use `LabelTranslations` for interface copy.

## Common mistakes {#troubleshooting}

- Use [Textarea](/docs/fields/textarea/) for multi-line plain text and [Rich text](/docs/rich-text/)
  for formatted documents.
- A field rename changes stored data and generated APIs. Create a reviewed migration instead of
  changing the string literal casually.
- `ReadOnly` and `Hidden` affect only the admin. They do not prevent API writes.
- For an automatically generated URL identifier, use [Slug](/docs/fields/slug/) rather than
  rebuilding slug hooks around a plain text field.

See [`field.Text`](/reference/field/text/) and the shared [field options](/docs/fields/#field-options).
