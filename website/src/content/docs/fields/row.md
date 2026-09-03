---
title: 'Row field'
description: 'Arrange direct child controls on the admin grid without changing document paths.'
product: core
eyebrow: 'Layout fields'
order: 79
aliases: ['field.Row', 'field columns', 'admin row']
relatedSymbolIds:
  ['go:github.com/riducms/ridu/field#Row', 'go:github.com/riducms/ridu/field#Columns']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Layout'
  order: 190
  title: 'Row'
---

Use `field.Row` to place related controls on one responsive admin grid row. Row is presentation-only:
its children remain at their existing document level and `Row` itself produces no stored property.

## In the admin {#admin-behavior}

![A Row layout in the Ridu admin placing populated First name and Last name controls side by side.](../../../../../docs/assets/fields/row.png)

_The controls share a responsive row but remain top-level properties in JSON._

## Smallest working example {#example}

```go title="content/people.go"
field.Row(
	field.Text("firstName", field.Columns(6)),
	field.Text("lastName", field.Columns(6)),
)
```

The document shape is `{ firstName, lastName }`, not `{ row: { … } }`. `Columns` accepts values from
1 through 12. On narrow screens the admin preserves readable controls instead of forcing an
unusable desktop grid.

## When to use Row {#when-to-use}

Rows are useful for short fields that authors understand together: first/last name, latitude and
longitude labels, or a status beside a date. Keep long text, rich text, arrays, and other dense
controls full width unless the actual authoring task benefits from the pairing.

Each child keeps its own validation, hooks, access, conditions, localization, and generated
contract. Query the child by its normal path. A Row can appear within a tab or other supported
layout, but it should not be mistaken for a stored nested container.

## Common mistakes {#troubleshooting}

- Column totals do not create server validation. They only describe layout.
- Use [Group](/docs/fields/group/) when the API should contain a nested object.
- Changing Row layout does not require a data migration; renaming or moving its child fields does.
- Do not hide an authorization-sensitive field by squeezing it out of the layout—use field access.

See [`field.Row`](/reference/field/row/) and [`field.Columns`](/reference/field/columns/).
