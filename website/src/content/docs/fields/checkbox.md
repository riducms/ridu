---
title: 'Checkbox field'
description: 'Store an explicit boolean and present it as an authoring toggle.'
product: core
eyebrow: 'Scalar and choice fields'
order: 67
aliases: ['field.Checkbox', 'boolean field', 'toggle']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#Checkbox']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Scalar & choice'
  order: 70
  title: 'Checkbox'
---

Use `field.Checkbox` for a boolean property such as `featured`, `archived`, or `requiresReview`.
The document value is the JSON boolean `true` or `false`, and the admin shows a checkbox/toggle
control.

## In the admin {#admin-behavior}

![A checked Feature this example checkbox in the Ridu admin.](../../../../../docs/assets/fields/checkbox.png)

_The control writes a JSON boolean, so API callers must send `true` or `false` rather than form strings._

## Smallest working example {#example}

```go title="content/posts.go"
field.Checkbox("featured", field.Default(false))
```

Use a boolean default when callers that omit the property should receive a concrete initial value.
Without `Required` or a default, omission and an explicit `false` have different create-input
meanings.

## Conditional authoring {#options}

```go title="content/posts.go"
field.Checkbox("sponsored", field.Default(false)),
field.Text(
	"sponsorName",
	field.ShowWhenCondition(
		field.Sibling("sponsored", field.ConditionEquals, true),
	),
),
```

Conditions only change what the admin displays. A hidden `sponsorName` still exists in submitted
data and remains subject to validation, access, and hooks. Enforce a cross-field business rule in
validation or a hook when `sponsorName` must be empty unless `sponsored` is true.

Read [Conditional fields](/docs/fields/conditional-fields/) for sibling scope, typed operators,
root-document conditions, compound expressions, and server-side enforcement.

Checkbox supports `Required`, boolean `Default`, localization, indexing/uniqueness where meaningful,
and common presentation options. Query it with boolean equality rather than the strings `"true"`
or `"false"`.

## Common mistakes {#troubleshooting}

- HTML form libraries often submit strings or omit unchecked controls. Convert them to an explicit
  boolean before calling the SDK.
- `ReadOnly` does not prevent an API mutation. Use field access for server authorization.
- Do not use a checkbox for three states. Model an explicit [Select](/docs/fields/select/) choice
  when `unknown` is a real business value.

See [`field.Checkbox`](/reference/field/checkbox/) and the shared [field options](/docs/fields/#field-options).
