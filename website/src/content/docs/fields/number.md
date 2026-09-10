---
title: 'Number field'
description: 'Add a number input with optional minimum, maximum, and allowed increments.'
product: core
eyebrow: 'Basic fields'
order: 66
aliases: ['field.Number', 'numeric field', 'Min Max Step']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#Number']
navigation:
  section: 'Model content'
  parent: fields
  order: 60
  title: 'Number'
---

Use `field.Number` for a finite JSON number: a price, score, quantity, percentage, or measurement.
Generated TypeScript uses `number`; numeric strings are not accepted.

## In the admin {#admin-behavior}

![A populated Priority number field in the Ridu admin with bounded numeric input.](../../../../../docs/assets/fields/number.png)

_Bounds constrain the editor and server; the API accepts JSON numbers, not numeric strings._

## Add a number input {#example}

```go title="content/products.go"
field.Number("price").Required().Min(0)
```

Submitting `"19.99"` as a string is invalid. Send `19.99` as a JSON number.

## Configuration {#configuration}

| Constructor or method                                    | What it controls                                                              |
| -------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `field.Number(name)`                                     | Creates a stored finite JSON number.                                          |
| `.Required()`                                            | Rejects a missing or null number. Zero remains a present value.               |
| `.Min(value)` / `.Max(value)`                            | Sets inclusive numeric bounds.                                                |
| `.Step(value)`                                           | Requires values on the allowed increment and configures the admin input step. |
| `.Default(value)` / `.DefaultFrom(callback)`             | Supplies a fixed or request-aware initial number.                             |
| `.Unique()` / `.Index()`                                 | Enforces distinct values or adds a query index.                               |
| `.Localized()` / `.Validate(...)` / `.LiveValidate(...)` | Enables translated values and application-specific checks.                    |

## Set limits and allowed increments {#options}

```go title="content/reviews.go"
field.Number("rating").Required().Min(1).Max(5).Step(0.5).Default(3)
```

`Min(1)` allows 1 and higher; `Max(5)` allows 5 and lower. `Step(0.5)` allows increments of
0.5 and sets the step used by the admin input. All submitted values must be finite; `NaN` and infinities are not JSON values and are
rejected before storage. Number also supports localization, uniqueness/indexing, conditions, and
the shared presentation options.

`.Default(3)` supplies the initial rating when it is omitted. To calculate an initial number on
the server, use `.DefaultFrom(callback)` and return `operation.Value[float64]`. The result still
has to satisfy `Min`, `Max`, and `Step`. See [Set default field values](/docs/fields/defaults/).

Number filters include equality and ordered comparisons, and numeric fields can be sorted. A
localized number has an independent value per content locale; use it for locale-specific
content such as a regional price, not display formatting.

## Common mistakes {#troubleshooting}

- JSON numbers are floating-point values in JavaScript and TypeScript. For currency that requires
  exact minor units, store an integer amount (for example cents) and document that unit.
- A placeholder such as `0` is not a default. Use `.Default(0)` to store zero when the field is
  omitted on create. An explicitly supplied null does not trigger a default.
- `Step` is a value constraint, not a rounding instruction. Normalize application calculations
  before submission.

See [`field.Number`](/reference/field/number/) and [query operators](/docs/querying/).

For an ordered list of numeric values, use [NumberList](/docs/fields/lists/).
Its callbacks receive `[]float64`; scalar Number continues receiving `float64`.
