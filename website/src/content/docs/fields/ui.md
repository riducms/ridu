---
title: 'UI field'
description: 'Add help text or a custom component to the form without storing a value.'
product: core
eyebrow: 'Layout fields'
order: 81
aliases: ['field.UI', 'presentation field', 'admin UI field']
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#UI',
    'go:github.com/riducms/ridu/field#PluginComponent'
  ]
navigation:
  section: 'Model content'
  parent: fields
  order: 210
  title: 'UI'
---

Use `field.UI` to add guidance or a plugin’s custom component to the form. UI fields appear only
in the admin; they do not add a value to the document or API response.

## In the admin {#admin-behavior}

![An Author guidance UI field in the Ridu admin showing presentation-only help.](../../../../../docs/assets/fields/ui.png)

_The guidance appears only in the admin; no `guidance` property enters storage, inputs, or SDK output._

## Built-in guidance {#example}

```go title="content/posts.go"
field.UI("publishingGuide").
	Label("Before you publish").
	Admin(field.Admin{
		Description: "Check the preview, links, and search description.",
	})
```

The name identifies the component in the form; it does not become a JSON property. Use translated labels and
descriptions when the admin interface supports several languages.

## Configuration {#configuration}

| Constructor or method                                        | What it controls                                                              |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------- |
| `field.UI(name)`                                             | Adds a named presentation-only position to the form.                          |
| `.Label(...)` / `.LabelTranslations(...)`                    | Supplies author-facing guidance text.                                         |
| `.Admin(field.Admin{Description: ...})`                      | Adds built-in explanatory copy and layout metadata.                           |
| `.Admin(field.Admin{Component: field.PluginComponent(...)})` | Selects a statically registered plugin component and its JSON-safe config.    |
| `.EditAdmin(callback)`                                       | Changes selected admin metadata without replacing the complete `Admin` value. |

UI is a layout field. It has no stored value, requiredness, localization, access rule, validator,
or lifecycle hook.

## Use a component from a plugin {#custom-component}

```go title="content/posts.go"
field.UI("contentScore").Admin(field.Admin{
	Editor: field.PluginComponent(
		"acme-editorial",
		"ContentScore",
		store.Object(store.Values{"minimum": store.Number(80)}),
	),
})
```

Register the plugin’s Go package and its matching admin package. `ContentScore` must be a
component registered by that package. The config object is included in the schema sent to the
admin, so it must contain JSON values and must not contain secrets.

Use the admin SDK to read or update data. Enforce permissions and validation in Go; hiding or
disabling a component cannot prevent API requests.

## Common mistakes {#troubleshooting}

- Do not expect a UI field to submit a value. Use a [custom field component](/docs/custom-components/field-components/) for a stored
  field, or a [Plugin field](/docs/fields/plugin/), when data belongs in the document.
- Do not place secrets in component config; the manifest is public to authorized tooling and admin
  clients.
- A component hidden or disabled in the admin cannot protect an API operation.

See [`field.UI`](/reference/field/ui/), [`field.PluginComponent`](/reference/field/plugin-component/),
and [Build a field plugin](/guides/custom-fields/).
