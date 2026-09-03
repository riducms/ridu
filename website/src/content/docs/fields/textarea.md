---
title: 'Textarea field'
description: 'Store multi-line plain text as a string.'
product: core
eyebrow: 'Scalar and choice fields'
order: 62
aliases: ['field.Textarea', 'multiline text', 'plain text area']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#Textarea']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Scalar & choice'
  order: 20
  title: 'Textarea'
---

Use `field.Textarea` for multi-line **plain text** such as a synopsis, internal note, or address. It
stores a string—the line breaks are part of that string—and renders a larger text control in the
admin.

## In the admin {#admin-behavior}

![A populated Summary textarea in the Ridu admin showing a focused multi-line plain-text control.](../../../../../docs/assets/fields/textarea.png)

_Line breaks remain part of one string; the control does not create rich-text structure._

## Smallest working example {#example}

```go title="content/posts.go"
field.Textarea("summary", field.MaxLength(280))
```

Create and update inputs accept `summary: string`; optional fields may be omitted. Reads return the
same plain string through REST, the Local API, and the generated SDK.

## A realistic configuration {#options}

```go title="content/reviews.go"
field.Textarea(
	"editorNote",
	field.Required(),
	field.MinLength(10),
	field.MaxLength(2_000),
	field.Placeholder("Explain what must change before publication…"),
	field.Sidebar(),
)
```

Textarea fields accept the common string options: `Required`, `MinLength`, `MaxLength`, `Default`,
`Localized`, `Unique`, `Index`, and the shared admin options. Use uniqueness only for a genuine
invariant; long prose is rarely an appropriate indexed or unique key.

Localization stores an independent string per content locale. Admin-language translations for the
label, description, or placeholder are separate and do not localize content.

## Constraints and querying {#querying}

String filters work as they do for [Text](/docs/fields/text/), including equality, containment,
`like`, selection, and sorting. Long text queries are not relevance-ranked search. Build a search
index outside this field when your
application needs stemming, ranking, or language analysis.

Textarea does not parse Markdown or HTML and does not sanitize it for rendering. Escape plain text
in your frontend. If authors need formatting, links, uploads, or structured blocks, use the
[Rich-text plugin](/docs/rich-text/) or [Blocks](/docs/fields/blocks/).

## Common mistakes {#troubleshooting}

- Do not store an object or arbitrary array in a textarea; use [JSON](/docs/fields/json/) or model
  the shape with fields.
- `MaxLength` limits characters accepted by Ridu; it does not visually truncate the control.
- Hiding a note in the admin is not a field access rule.

See [`field.Textarea`](/reference/field/textarea/) and [access control](/docs/access-control/).
