---
title: 'Conditional fields'
description: 'Show or hide a field based on another value in the form.'
product: core
eyebrow: 'Field behavior'
order: 61
aliases:
  [
    'field.Sibling',
    'field.Root',
    'field.Equal',
    'field.Admin.VisibleWhen',
    'conditional logic',
    'show field conditionally'
  ]
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#Sibling',
    'go:github.com/riducms/ridu/field#Root',
    'go:github.com/riducms/ridu/field#Equal',
    'go:github.com/riducms/ridu/field#Admin',
    'go:github.com/riducms/ridu/field#All',
    'go:github.com/riducms/ridu/field#Any',
    'go:github.com/riducms/ridu/field#Not'
  ]
navigation:
  section: 'Model content'
  parent: fields
  order: 340
  title: 'Conditional fields'
---

Use a condition to show a field only when it is relevant. For example, show a video URL when
an author selects “Video” as the post format. The admin updates immediately as values change.

Set `Admin.VisibleWhen` to the condition that must be true:

```go
field.Text("videoURL").Admin(field.Admin{
	VisibleWhen: field.Equal(field.Sibling("format"), "video"),
})
```

Read that expression as: “show `videoURL` when the `format` field beside it equals `video`.”

## Show a field when another value matches {#show-when}

This collection reveals `videoURL` when an author chooses the Video format:

```go title="content/posts.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Posts = ridu.Collection{
	Slug: "posts",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Select("format", "article", "video").Default("article"),
		field.Text("videoURL").Admin(field.Admin{
			VisibleWhen: field.Equal(field.Sibling("format"), "video"),
		}),
	},
}
```

## Use values from groups and repeated rows {#sibling-scope}

`field.Sibling("name")` reads a field beside the one being shown:

- A root field reads another root field.
- A field inside a Group reads from that Group.
- A field inside an Array or Block reads from its own row, not another row.
- A sibling path can descend through non-repeating Groups, such as `settings.kind`.
- A path cannot cross through an Array or Blocks list. Ridu reports an error when you start
  the application.

The following condition is attached to `answers.explanation`. Each answer reads its own
`settings.kind`, while `field.Root("archived")` always reads the root `archived` field:

```go title="content/questions.go"
var Questions = ridu.Collection{
	Slug: "questions",
	Fields: field.Fields{
		field.Checkbox("archived").Default(false),
		field.Array("answers", field.Fields{
			field.Group("settings", field.Fields{
				field.Select("kind", "lesson", "question", "note"),
			}),
			field.Textarea("explanation").Admin(field.Admin{
				VisibleWhen: field.All(
					field.OneOf(field.Sibling("settings.kind"), "lesson", "question"),
					field.Not(field.Equal(field.Root("archived"), true)),
				),
			}),
		}),
	},
}
```

At the document root, `Sibling("status")` and `Root("status")` read the same value. Prefer
`Sibling` when the rule should remain relative if those fields later move together into a Group
or repeated row. Use `Root` when the rule should always read a top-level field. Use
stored field names with dots for nested objects. Layout fields such as Row and Collapsible do
not add a part to the path.

## Compare and combine values {#operators}

Use a comparison value that matches the field type: a string for text or choices, a boolean
for a checkbox, or a number for a number field. Ridu checks the condition when the application
starts.

| Condition                           | Meaning                                           | Values      |
| ----------------------------------- | ------------------------------------------------- | ----------- |
| `field.Equal(reference, value)`     | The current value equals the supplied value       | Exactly one |
| `field.NotEqual(reference, value)`  | The current value differs from the supplied value | Exactly one |
| `field.OneOf(reference, values...)` | The current value equals any supplied value       | One or more |

Strings, booleans, and numbers are supported. For example:

```go
field.Equal(field.Sibling("featured"), true)
field.NotEqual(field.Sibling("score"), 0)
field.OneOf(field.Sibling("status"), "draft", "review")
```

Use `field.All` when every condition must match, `field.Any` when at least one must match, and
`field.Not` to reverse a condition. `All` and `Any` require at least two conditions; `Not` takes
one. The example above combines the answer type with the document’s archived setting.

## Validate hidden fields and protect their values {#server-enforcement}

Hiding a field keeps its existing value. Changing the post format from Video to Article, for
example, does not delete an entered video URL. The field can still be submitted through REST,
the SDK, or the Local API. A visibility condition does not validate data or grant permissions.

Add a server-side rule when you need to enforce a requirement:

| Requirement                                   | Use                                               |
| --------------------------------------------- | ------------------------------------------------- |
| Control access using values from the same row | `ctx.Siblings` in an attached `field.Access` rule |
| Reject an invalid combination                 | `.Validate(...)` on the field                     |
| Normalize or clear a submitted value          | `field.RawTransform` in a `BeforeValidate` hook   |
| Derive a response-only value                  | A Virtual field resolver through `ctx.Root`       |

See [reading sibling values](/docs/fields/#sibling-data) for those contexts and the complete
[row-scoped field access example](/docs/access-control/#sibling-data).

## Troubleshooting {#troubleshooting}

- If a nested condition reads the wrong value, confirm that the controlling field shares the same
  Group, Array row, or Block with the field being shown. Use `field.Root` for a root value.
- If a condition never matches, check that the comparison type matches the referenced field. The
  boolean `true` is different from the string `"true"`.
- Use `All`, `Any`, and `Not` in `Admin.VisibleWhen` to combine conditions.
- If an API caller can still write the hidden field, add field access or a hook. That behavior is
  intentional.

See [`field.Sibling`](/reference/field/sibling/), [`field.Root`](/reference/field/root/),
[`field.Admin.VisibleWhen`](/reference/field/admin/), and [`field.Equal`](/reference/field/equal/)
for their exact signatures.
