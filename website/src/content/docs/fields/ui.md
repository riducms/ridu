---
title: 'UI field'
description: 'Place guidance or a paired custom component in the editor without accepting or storing a document value.'
product: core
eyebrow: 'Layout fields'
order: 81
aliases: ['field.UI', 'presentation field', 'admin UI field']
relatedSymbolIds:
  ['go:github.com/riducms/ridu/field#UI', 'go:github.com/riducms/ridu/field#AdminComponent']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Layout'
  order: 210
  title: 'UI'
---

Use `field.UI` for contextual guidance or a statically paired admin component that does not own a
document value. UI fields never appear in stored data, generated inputs, REST bodies, or SDK output.

## In the admin {#admin-behavior}

![An Author guidance UI field in the Ridu admin showing presentation-only help.](../../../../../docs/assets/fields/ui.png)

_The guidance appears only in the admin; no `guidance` property enters storage, inputs, or SDK output._

## Built-in guidance {#example}

```go title="content/posts.go"
field.UI(
	"publishingGuide",
	field.Label("Before you publish"),
	field.Description("Check the preview, links, and search description."),
)
```

The name is a presentation identity, not a JSON property. Use translated labels and
descriptions when the admin interface supports several languages.

## Pair a custom component {#custom-component}

```go title="content/posts.go"
field.UI(
	"contentScore",
	field.AdminComponent(
		"acme-editorial",
		"ContentScore",
		json.RawMessage(`{"minimum":80}`),
	),
)
```

The plugin must be registered with its paired static admin package. The component name must
match an exported registration and config must be a deterministic JSON object because it enters the
public manifest. Generation and startup reject a missing pair.

Custom UI is presentation, not trusted execution or authorization. Read data through the provided
admin/SDK contracts and keep server invariants in access, validation, hooks, or plugin endpoints.

## Common mistakes {#troubleshooting}

- Do not expect a UI field to submit a value. Build a paired custom renderer for a stored
  field, or a [Plugin field](/docs/fields/plugin/), when data belongs in the document.
- Do not place secrets in component config; the manifest is public to authorized tooling and admin
  clients.
- A component hidden or disabled in the admin cannot protect an API operation.

See [`field.UI`](/reference/field/ui/), [`field.AdminComponent`](/reference/field/admin-component/),
and [Build a custom field](/guides/custom-fields/).
