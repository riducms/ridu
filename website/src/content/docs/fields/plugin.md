---
title: 'Plugin field'
description: 'Add a field type supplied by a plugin, such as rich text.'
product: core
eyebrow: 'Computed and plugin fields'
order: 84
aliases: ['field.Plugin', 'custom field', 'plugin field']
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#Plugin',
    'go:github.com/riducms/ridu/field#PluginField.CollectionReferenceKeys'
  ]
navigation:
  section: 'Model content'
  parent: fields
  order: 240
  title: 'Plugin'
---

Use a plugin field when you need a field type beyond the built-in options, such as rich text.
The plugin defines how to validate, store, and edit its value. Most applications use the
function supplied by the plugin, such as `richtext.Field`.

## In the admin {#admin-behavior}

![The official Rich text plugin field in the Ridu admin with populated portable editor content.](../../../../../docs/assets/fields/plugin.png)

_The rich-text plugin provides both the editor and the Go code that validates its content._

## Add a field from a plugin {#example}

```go title="content/posts.go"
import "github.com/riducms/ridu/plugins/richtext"

richtext.Field("content").Required()
```

Add the Go plugin to `Config.Plugins` and register its matching admin package. The `ridu add`
command can install both packages and update their registration; see the plugin’s installation
guide for the command to use.

## Configuration {#configuration}

| Constructor or method                            | What it controls                                                                                  |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------- |
| A plugin helper such as `richtext.Field(name)`   | Preferred application API; supplies the plugin key, config, runtime bindings, and admin pairing.  |
| `field.Plugin(name, key, config)`                | Low-level field constructor for plugin authors; `config` must be valid JSON for the named plugin. |
| `.EmbeddedTrees(trees...)`                       | Declares finite nested field shapes inside an otherwise opaque plugin value.                      |
| `.CollectionReferenceKeys(keys...)`              | Declares direct collection-reference keys owned by the plugin value.                              |
| `.Required()` / `.Localized()`                   | Requires the plugin payload or stores one payload per content locale.                             |
| `.Validate(...)` / `.LiveValidate(...)`          | Adds application rules after the plugin's own value checks.                                       |
| `.Access(...)`, `.Hooks(...)`, `.AfterRead(...)` | Controls authorization and lifecycle behavior around the plugin value.                            |

## Build your own plugin field {#low-level}

```go title="color/field.go"
func Field(name string, config Config) field.PluginField {
	encoded, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return field.Plugin(name, Key, encoded)
}
```

When writing a plugin, wrap `field.Plugin` in a function like the one above so applications can
pass a Go config struct. The config is included in the schema sent to the admin: use JSON values
and keep secrets, callbacks, and request-specific data out of it.

If config properties refer to collections, list their names with `CollectionReferenceKeys` so
Ridu can track those references when a collection is renamed.

The plugin descriptor defines the generated Go and TypeScript types, OpenAPI schema, server
validation, admin component, and supported filters. Without type definitions, generated values
use `json.RawMessage` in Go and `unknown` in TypeScript.

The returned `field.PluginField` supports methods such as `.Required()` and `.Admin(...)`, just
like built-in fields. Follow [Build a field plugin](/guides/custom-fields/) for a complete example.

## Common mistakes {#troubleshooting}

- Do not call `field.Plugin` directly when the package provides a typed helper.
- Install and register both the Go package and its matching admin package.
- The config controls how the field works; the document stores the value entered by the author.
- Test a custom field through resolution, generation, storage, Local API/REST, and the admin.

See [`field.Plugin`](/reference/field/plugin/), [Plugins](/docs/plugins/), and the complete
[custom field guide](/guides/custom-fields/).
