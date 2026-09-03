---
title: 'Group field'
description: 'Model one nested object with child fields, generated types, and dotted query paths.'
product: core
eyebrow: 'Structured fields'
order: 73
aliases: ['field.Group', 'nested object field', 'field.Fields']
relatedSymbolIds:
  ['go:github.com/riducms/ridu/field#Group', 'go:github.com/riducms/ridu/field#Fields']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Structured'
  order: 130
  title: 'Group'
---

Use `field.Group` for one object with a known shape: SEO metadata, an address, dimensions, or a
reusable cluster of settings. Children become a nested type in generated Go and TypeScript
contracts.

## In the admin {#admin-behavior}

![An expanded Search preview group in the Ridu admin with populated SEO title and description fields.](../../../../../docs/assets/fields/group.png)

_Grouped controls become one nested object, with child paths such as `seo.title`._

## Smallest working example {#example}

```go title="content/posts.go"
field.Group(
	"seo",
	field.Fields(
		field.Text("title", field.MaxLength(60)),
		field.Textarea("description", field.MaxLength(160)),
	),
)
```

```json title="document.json"
{
	"seo": {
		"title": "A concise search title",
		"description": "A concise search description."
	}
}
```

At least one child field is required. Child names must be unique within the group and form canonical
paths such as `seo.title` and `seo.description`.

## Validation, queries, and localization {#options}

Group supports `Required`, `Localized`, conditions, and common presentation options. Each child
keeps its own validation, access, hooks, and admin metadata. Query nested stored children with their
dotted path; the generated SDK exposes those canonical keys in its `where` contract. The group
container also supports an `exists` filter.

Localize individual children when only those values vary. Put `Localized` on the Group when the
entire object varies as one locale value. A localized container falls back as a container instead
of mixing child values from different locale objects.

## Common mistakes {#troubleshooting}

- Use [Array](/docs/fields/array/) when there can be several objects and [Blocks](/docs/fields/blocks/)
  when rows have different shapes.
- A Group changes the API path. Use [Row](/docs/fields/row/) or [Collapsible](/docs/fields/collapsible/)
  to organise controls without adding an object wrapper.
- Renaming the group or a child is a stored-data and API migration.

See [`field.Group`](/reference/field/group/) and [`field.Fields`](/reference/field/fields/).
