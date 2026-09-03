---
title: 'Radio field'
description: 'Show every singular configured choice while storing the same finite string contract as Select.'
product: core
eyebrow: 'Scalar and choice fields'
order: 69
aliases: ['field.Radio', 'radio buttons', 'radio field']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#Radio']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Scalar & choice'
  order: 90
  title: 'Radio'
---

Use `field.Radio` for a small set of mutually exclusive choices that authors should see without
opening a dropdown. Its stored and generated contract is the same singular string union as
[Select](/docs/fields/select/); only the admin control differs.

## In the admin {#admin-behavior}

![A Tone radio field in the Ridu admin with Friendly selected from three visible choices.](../../../../../docs/assets/fields/radio.png)

_Labels remain interface copy; the document stores one configured choice value._

## Smallest working example {#example}

```go title="content/posts.go"
field.Radio(
	"priority",
	field.OneOf("low", "normal", "high"),
	field.Default("normal"),
)
```

For clearer author copy, configure explicit choices:

```go title="content/events.go"
field.Radio(
	"attendance",
	field.Choices(
		field.Choice{Value: "remote", Label: "Join online"},
		field.Choice{Value: "venue", Label: "Attend at the venue"},
	),
	field.Required(),
)
```

Radio supports singular `Default`, `Required`, localization, indexing/uniqueness, conditions, and
common presentation options. It does not accept `Multiple` or `DefaultChoices`; use Select
for multiple values.

Queries and generated types use stored values, never labels. Choice labels may be translated
for the admin without localizing the content value. Add `field.Localized()` only when different
locales may select different business values.

## Common mistakes {#troubleshooting}

- Too many radio choices create a slow form. Switch to Select or a Relationship picker as the set
  grows.
- Renaming a `Choice.Value` is a data migration. Changing `Label` is safe presentation copy.
- `ShowWhen` can improve the form but cannot enforce a cross-field rule or access policy.

See [`field.Radio`](/reference/field/radio/) and [Select](/docs/fields/select/) for the full choice
contract.
