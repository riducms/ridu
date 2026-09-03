---
title: 'Number field'
description: 'Store a finite number with minimum, maximum, step, default, and query constraints.'
product: core
eyebrow: 'Scalar and choice fields'
order: 66
aliases: ['field.Number', 'numeric field', 'Min Max Step']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#Number']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Scalar & choice'
  order: 60
  title: 'Number'
---

Use `field.Number` for a finite JSON number: a price, score, quantity, percentage, or measurement.
Generated TypeScript uses `number`; numeric strings are not accepted.

## In the admin {#admin-behavior}

![A populated Priority number field in the Ridu admin with bounded numeric input.](../../../../../docs/assets/fields/number.png)

_Bounds constrain the editor and server; the API accepts JSON numbers, not numeric strings._

## Smallest working example {#example}

```go title="content/products.go"
field.Number("price", field.Required(), field.Min(0))
```

Submitting `"19.99"` as a string is invalid. Send `19.99` as a JSON number.

## Bounds and increments {#options}

```go title="content/reviews.go"
field.Number(
	"rating",
	field.Required(),
	field.Min(1),
	field.Max(5),
	field.Step(0.5),
	field.Default(3),
)
```

`Min` and `Max` include their boundary. `Step` declares the allowed increment and informs the admin
control. All submitted values must be finite; `NaN` and infinities are not JSON values and are
rejected before storage. Number also supports localization, uniqueness/indexing, conditions, and
the shared presentation options.

Number filters include equality and ordered comparisons, and numeric fields can be sorted. A
localized number has an independent value per content locale; use it for genuinely locale-specific
content such as a regional price, not display formatting.

## Common mistakes {#troubleshooting}

- JSON numbers are floating-point values at the TypeScript boundary. For currency that requires
  exact minor units, store an integer amount (for example cents) and document that unit.
- A placeholder such as `0` is not a default. Use `field.Default(0)` when the value must be written
  on create.
- `Step` is a value constraint, not a rounding instruction. Normalize application calculations
  before submission.

See [`field.Number`](/reference/field/number/) and [query operators](/docs/querying/).
