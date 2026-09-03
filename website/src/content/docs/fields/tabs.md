---
title: 'Tabs field'
description: 'Organise document editing into named stored objects or presentation-only sections.'
product: core
eyebrow: 'Structured fields'
order: 76
aliases: ['field.Tabs', 'NamedTab', 'UnnamedTab', 'tab field']
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#Tabs',
    'go:github.com/riducms/ridu/field#NamedTab',
    'go:github.com/riducms/ridu/field#UnnamedTab'
  ]
navigation:
  section: 'Model content'
  parent: fields
  group: 'Structured'
  order: 160
  title: 'Tabs'
---

Use `field.Tabs` to divide a long editor into focused sections. A **named** tab stores its children
under an object property; an **unnamed** tab changes only the admin layout and leaves child paths at
their current level.

## In the admin {#admin-behavior}

![A Tabs field in the Ridu admin with the Content tab active and Settings available beside it.](../../../../../docs/assets/fields/tabs.png)

_Unnamed tabs alter layout only; named tabs add an object boundary to document paths._

## Named and unnamed tabs {#example}

```go title="content/pages.go"
field.Tabs(
	field.UnnamedTab("Content",
		field.Text("title", field.Required()),
		field.Blocks("layout", field.BlockTypes(/* … */)),
	),
	field.NamedTab("seo", "Search & sharing",
		field.Text("title", field.MaxLength(60)),
		field.Textarea("description", field.MaxLength(160)),
	),
)
```

The resulting document has root `title` and `layout` properties plus a nested `seo` object. Choose
named tabs when the object boundary is meaningful to API consumers; choose unnamed tabs when only
the authoring experience needs separation.

Tab labels and `LabelTranslations` are admin-interface copy. They do not localize content. Configure
`Localized` on stored child fields or on another nested container according to the value that
actually varies.

## Constraints and common mistakes {#troubleshooting}

- Every tab needs a label and fields. Named tab names must obey field-name rules and be
  unique at that level.
- Presentation-only tabs do not grant access or change validation. Hidden fields still pass through
  the operation engine.
- Changing an unnamed tab to a named tab changes document paths and requires a migration. Changing
  only its label does not.
- For a few direct fields, the concise `field.Tab("Label")` option may be enough. Use Tabs when you
  want an explicit ordered set of sections or stored named objects.

See [`field.Tabs`](/reference/field/tabs/), [`field.NamedTab`](/reference/field/named-tab/), and
[`field.UnnamedTab`](/reference/field/unnamed-tab/).
