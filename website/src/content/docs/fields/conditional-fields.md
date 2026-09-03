---
title: 'Conditional fields'
description: 'Show an admin field from typed sibling or document values without confusing presentation with validation or access control.'
product: core
eyebrow: 'Field behavior'
order: 61
aliases:
  [
    'field.Sibling',
    'field.Document',
    'field.ShowWhen',
    'field.ShowWhenCondition',
    'conditional logic',
    'show field conditionally'
  ]
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#Sibling',
    'go:github.com/riducms/ridu/field#Document',
    'go:github.com/riducms/ridu/field#ShowWhen',
    'go:github.com/riducms/ridu/field#ShowWhenCondition',
    'go:github.com/riducms/ridu/field#All',
    'go:github.com/riducms/ridu/field#Any',
    'go:github.com/riducms/ridu/field#Not'
  ]
navigation:
  section: 'Model content'
  parent: fields
  group: 'Field behavior'
  order: 10
  title: 'Conditional fields'
---

Use a field condition when one authoring control should appear only after another value makes it
relevant. Conditions react immediately in the admin. They do not remove data, reject API input, or
grant access.

`field.Sibling` creates one typed condition. Attach it to the field being shown with
`field.ShowWhenCondition`:

```go
field.Text(
	"videoURL",
	field.ShowWhenCondition(
		field.Sibling("format", field.ConditionEquals, "video"),
	),
)
```

Read that expression as: “show `videoURL` when the `format` field beside it equals `video`.”

## Start with string equality {#show-when}

`field.ShowWhen(path, value)` is the concise form for one sibling string equality. This complete
collection reveals `videoURL` when an author chooses the Video format:

```go title="content/posts.go" focus={19}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Posts = ridu.Collection{
	Slug: "posts",
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Select(
			"format",
			field.OneOf("article", "video"),
			field.Default("article"),
		),
		field.Text(
			"videoURL",
			field.ShowWhen("format", "video"),
		),
	},
}
```

Use `ShowWhen` for this common string case. Use `Sibling` with `ShowWhenCondition` for booleans,
numbers, several accepted values, negation, or combined rules.

## Understand sibling scope {#sibling-scope}

A sibling path starts at the parent of the field being shown:

- A root field reads another root field.
- A field inside a Group reads from that Group.
- A field inside an Array or Block reads from its own row, not another row.
- A sibling path can descend through non-repeating Groups, such as `settings.kind`.
- It cannot cross through an Array or Blocks field. Ridu rejects that path while resolving the
  configuration.

The following condition is attached to `answers.explanation`. For the third answer row,
`field.Sibling("settings.kind", ...)` reads `answers.2.settings.kind`. The separate
`field.Document("archived", ...)` predicate always reads the root `archived` field:

```go title="content/questions.go" focus={18-21}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Questions = ridu.Collection{
	Slug: "questions",
	Fields: []field.Definition{
		field.Checkbox("archived", field.Default(false)),
		field.Array("answers", field.Fields(
			field.Group("settings", field.Fields(
				field.Select("kind", field.OneOf("lesson", "question", "note")),
			)),
			field.Textarea(
				"explanation",
				field.ShowWhenCondition(field.All(
					field.Sibling("settings.kind", field.ConditionOneOf, "lesson", "question"),
					field.Not(field.Document("archived", field.ConditionEquals, true)),
				)),
			),
		)),
	},
}
```

At the document root, `Sibling("status", ...)` and `Document("status", ...)` read the same value.
Prefer `Sibling` when the rule should remain relative if those fields later move together into a
Group or repeated row. Use `Document` when the rule intentionally depends on a root setting.

## Choose an operator {#operators}

The comparison value must have the same scalar type as the referenced field. Ridu checks paths,
types, and operand counts before the application starts.

| Operator                   | Meaning                                           | Values      |
| -------------------------- | ------------------------------------------------- | ----------- |
| `field.ConditionEquals`    | The current value equals the supplied value       | Exactly one |
| `field.ConditionNotEquals` | The current value differs from the supplied value | Exactly one |
| `field.ConditionOneOf`     | The current value equals any supplied value       | One or more |

Strings, booleans, and numbers are supported. For example:

```go
field.Sibling("featured", field.ConditionEquals, true)
field.Sibling("score", field.ConditionNotEquals, 0)
field.Sibling("status", field.ConditionOneOf, "draft", "review")
```

Combine predicates with `field.All`, `field.Any`, and `field.Not`. `All` and `Any` require at least
two children; `Not` wraps exactly one.

## Conditions are presentation only {#server-enforcement}

A hidden field keeps its current form value and can still be submitted through REST, the SDK, or
the Local API. This prevents toggling a controller field from silently deleting work, but it also
means a condition is never a security or validation rule.

Use the corresponding server contract when behavior must be enforced:

| Requirement                              | Server-side API                                        |
| ---------------------------------------- | ------------------------------------------------------ |
| Allow or redact a field based on its row | `ctx.SiblingData` in a `ridu.FieldAccess` rule         |
| Reject an invalid combination            | `BeforeValidate` or a typed plugin-field validator     |
| Normalize or clear a submitted value     | `BeforeValidate` through `ctx.Data`                    |
| Derive a response-only value             | A Virtual field resolver through `ctx.Document.Values` |

See [reading sibling values](/docs/fields/#sibling-data) for those contexts and the complete
[row-scoped field access example](/docs/access-control/#sibling-data).

## Troubleshooting {#troubleshooting}

- If a nested condition reads the wrong value, confirm that the controlling field shares the same
  Group, Array row, or Block with the field being shown. Use `field.Document` for a root value.
- If a condition never matches, check that the comparison type matches the referenced field. The
  boolean `true` is different from the string `"true"`.
- If `ShowWhen` cannot express the comparison, switch to `ShowWhenCondition` and a typed `Sibling`
  predicate.
- If an API caller can still write the hidden field, add field access or a hook. That behavior is
  intentional.

See [`field.Sibling`](/reference/field/condition-sibling/),
[`field.Document`](/reference/field/condition-document/),
[`field.ShowWhenCondition`](/reference/field/show-when-condition/), and
[`field.ShowWhen`](/reference/field/show-when/) for their exact signatures.
