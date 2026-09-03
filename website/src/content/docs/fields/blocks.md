---
title: 'Blocks field'
description: 'Author an ordered, discriminated list of different content shapes.'
product: core
eyebrow: 'Structured fields'
order: 75
aliases: ['field.Blocks', 'page builder', 'BlockType', 'BlockTypes']
relatedSymbolIds:
  ['go:github.com/riducms/ridu/field#Blocks', 'go:github.com/riducms/ridu/field#BlockType']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Structured'
  order: 150
  title: 'Blocks'
---

Use `field.Blocks` for an ordered list that may contain different authored shapes: a page-builder
layout, email sections, or a portable content stream. Every row stores a stable `blockType`
discriminator and the fields declared by that block type.

## In the admin {#admin-behavior}

![An expanded Page layout Blocks field in the Ridu admin with populated Callout and Quote blocks.](../../../../../docs/assets/fields/blocks.png)

_Each row can use a different configured shape; `blockType` preserves its type in stored data and generated unions._

## Smallest working example {#example}

```go title="content/pages.go"
field.Blocks(
	"layout",
	field.BlockTypes(
		field.BlockType("hero", "Hero",
			field.Text("heading", field.Required()),
			field.Textarea("summary"),
		),
		field.BlockType("quote", "Quote",
			field.Textarea("text", field.Required()),
			field.Text("source"),
		),
	),
)
```

```json title="document.json"
{
	"layout": [
		{ "blockType": "hero", "heading": "Build with Ridu" },
		{ "blockType": "quote", "text": "Content is structured data." }
	]
}
```

At least one block type is required. Keys must be unique lowercase kebab-case. A direct block child
cannot be named `blockType` because Ridu owns that discriminator.

## Contracts, queries, and authoring {#options}

Generated TypeScript exposes a discriminated union, so narrowing on `blockType` gives the exact
fields for that row. Authors choose a block type, edit it, reorder the list, and may use a paired
`RowLabelComponent` for richer row headings.

Nested query paths include the block key, for example `layout.quote.source`; the container supports
`exists`. Put `Localized` on a child for translated values within a shared layout or on Blocks when
each locale owns its complete block selection and order.

## Common mistakes {#troubleshooting}

- A block key is a persisted API discriminator. Renaming/removing one requires a reviewed data
  migration and consumer update.
- Use [Array](/docs/fields/array/) when every row has the same shape; it produces a simpler contract.
- Blocks store structured content. Handle every generated union member in the frontend.

See [`field.Blocks`](/reference/field/blocks/), [`field.BlockType`](/reference/field/block-type/), and
[Generated contracts](/docs/generated-contracts/).
