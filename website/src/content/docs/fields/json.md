---
title: 'JSON field'
description: 'Store JSON whose properties are not known in advance, such as metadata from another service.'
product: core
eyebrow: 'Structured fields'
order: 71
aliases: ['field.JSON', 'JSON field', 'unknown data']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#JSON']
navigation:
  section: 'Model content'
  parent: fields
  order: 110
  title: 'JSON'
---

Use `field.JSON` for JSON-compatible data whose properties are not known in advance or are controlled by another
system. It accepts null, booleans, finite numbers, strings, arrays, and objects and generates
`unknown` in TypeScript so consumers must narrow the value before use.

## In the admin {#admin-behavior}

![A populated Metadata JSON editor in the Ridu admin with formatted object data.](../../../../../docs/assets/fields/json.png)

_Authors edit open JSON; generated TypeScript exposes `unknown`, requiring consumers to narrow it._

## Add a JSON editor {#example}

```go title="content/integrations.go"
field.JSON("providerMetadata").
	Admin(field.Admin{
		Description: "Opaque metadata returned by the connected provider.",
	})
```

The admin shows a JSON editor. The server rejects invalid JSON whether it comes from the admin,
REST, the SDK, or the Local API.

## Configuration {#configuration}

| Constructor or method                             | What it controls                                                        |
| ------------------------------------------------- | ----------------------------------------------------------------------- |
| `field.JSON(name)`                                | Creates a stored JSON-compatible value with an open shape.              |
| `.Required()`                                     | Requires a non-null value.                                              |
| `.Localized()`                                    | Stores separate JSON for each configured content locale.                |
| `.Validate(callback)` / `.LiveValidate(callback)` | Adds application shape or business rules at save time or while editing. |
| `.Admin(...)`                                     | Sets label, description, width, visibility, and editor metadata.        |
| `.Access(...)` / `.RestrictAccess(...)`           | Replaces or narrows field create/read/update access.                    |
| `.Hooks(...)` / `.ReadHooks(...)`                 | Transforms the opaque stored value or response.                         |

## Choose JSON or structured fields {#model-the-shape}

Prefer [Group](/docs/fields/group/) when the object has known properties, [Array](/docs/fields/array/)
for a list of similar objects, or [Blocks](/docs/fields/blocks/) for a list of different content
types. Defining the fields lets Ridu validate each property, generate useful types, and provide
individual admin controls.

JSON supports `Required`, `Localized`, conditions, and common presentation options. It
does not accept `Default`, length limits, number limits, choices, or relationship options. Use
`.Validate(...)` for a custom validation rule, or build a plugin field when the value needs a
reusable field type and editor.

## Querying and localization {#querying}

Select or omit the JSON property as one value. The query API does not support JSON path operators.
Promote fields you must filter or sort into the content model.

`Localized` stores the whole JSON value independently per locale. Partial locale fallback applies
to the field value, not recursively to individual object keys.

## Common mistakes {#troubleshooting}

- Do not use JSON to avoid modelling a stable business object; you lose generated
  types and validation for individual properties.
- JSON does not accept functions, `undefined`, `Date`, `Map`, circular values, `NaN`, or infinity.
- Treat provider metadata as untrusted input even after JSON decoding.

See [`field.JSON`](/reference/field/json/) and [Generated contracts](/docs/generated-contracts/).
