---
title: 'Checkbox field'
description: 'Add a checkbox for a true-or-false setting such as featured or archived.'
product: core
eyebrow: 'Basic fields'
order: 67
aliases: ['field.Checkbox', 'boolean field', 'toggle']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#Checkbox']
navigation:
  section: 'Model content'
  parent: fields
  order: 70
  title: 'Checkbox'
---

Use `field.Checkbox` for a boolean property such as `featured`, `archived`, or `requiresReview`.
The document value is the JSON boolean `true` or `false`, and the admin shows a checkbox/toggle
control.

## In the admin {#admin-behavior}

![A checked Feature this example checkbox in the Ridu admin.](../../../../../docs/assets/fields/checkbox.png)

_The control writes a JSON boolean, so API callers must send `true` or `false` rather than form strings._

## Add a checkbox {#example}

```go title="content/posts.go"
field.Checkbox("featured").Default(false)
```

The default stores `false` when the property is omitted on create. An explicit `true` or `false`
keeps the caller's choice. Without a default, an omitted optional checkbox has no boolean value;
it does not automatically become `false`.

Use `.DefaultFrom(callback)` when the initial choice depends on the request. Its result is an
`operation.Value[bool]`; `operation.Present(false)` supplies a value just as `true` does. See
[Set default field values](/docs/fields/defaults/) for callback and admin behavior.

## Configuration {#configuration}

| Constructor or method                             | What it controls                                                                    |
| ------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `field.Checkbox(name)`                            | Creates a stored JSON boolean shown as a checkbox or toggle.                        |
| `.Required()`                                     | Requires a boolean value; `false` still counts as present.                          |
| `.Default(value)` / `.DefaultFrom(callback)`      | Supplies a fixed or request-aware initial boolean.                                  |
| `.Index()` / `.Unique()`                          | Adds an index or uniqueness rule where the data model needs it.                     |
| `.Localized()`                                    | Stores a separate boolean for each configured content locale.                       |
| `.Admin(...)`                                     | Sets label, description, visibility condition, width, or a supported custom editor. |
| `.Validate(callback)` / `.LiveValidate(callback)` | Adds save validation or optional live feedback.                                     |

## Show another field when checked {#options}

```go title="content/posts.go"
field.Checkbox("sponsored").Default(false),
field.Text("sponsorName").Admin(field.Admin{
	VisibleWhen: field.Equal(field.Sibling("sponsored"), true),
}),
```

Conditions only change what the admin displays. A hidden `sponsorName` still exists in submitted
data and remains subject to validation, access, and hooks. Enforce a cross-field business rule in
validation or a hook when `sponsorName` must be empty unless `sponsored` is true.

See [Conditional fields](/docs/fields/conditional-fields/) to combine conditions or read values
from a group or array row.

You can also make the field required, set a boolean default, store a different value per locale,
or customize its appearance with `Admin`. Query it with boolean equality rather than the strings `"true"`
or `"false"`.

## Common mistakes {#troubleshooting}

- HTML form libraries often submit strings or omit unchecked controls. Convert them to an explicit
  boolean before calling the SDK.
- `ReadOnly` does not prevent an API mutation. Use field access for server authorization.
- Do not use a checkbox for three states. Model an explicit [Select](/docs/fields/select/) choice
  when `unknown` is a real business value.

See [`field.Checkbox`](/reference/field/checkbox/) and the shared [field options](/docs/fields/#field-options).
