---
title: 'Plugin field'
description: 'Use a compiled plugin’s storage, validation, generated type, and paired admin control.'
product: core
eyebrow: 'Computed and plugin fields'
order: 84
aliases: ['field.Plugin', 'custom field', 'plugin field']
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#Plugin',
    'go:github.com/riducms/ridu/field#CollectionReferenceKeys'
  ]
navigation:
  section: 'Model content'
  parent: fields
  group: 'Computed & plugin'
  order: 240
  title: 'Plugin'
---

`field.Plugin` is the low-level constructor for a compiled field extension. Most applications use
a plugin-owned helper such as `richtext.Field`; that helper serializes valid config and prevents
callers from mistyping the plugin key.

## In the admin {#admin-behavior}

![The official Rich text plugin field in the Ridu admin with populated portable editor content.](../../../../../docs/assets/fields/plugin.png)

_The Svelte control authors the value; its paired Go plugin defines server validation and the stored shape._

## Prefer the plugin helper {#example}

```go title="content/posts.go"
import "github.com/riducms/ridu/plugins/richtext"

richtext.Field("content", field.Required())
```

The matching plugin must also be present in `Config.Plugins`, and its paired admin package must be
statically registered when the field needs a custom control. Run `ridu add …` to install both sides
and update their registration.

## Low-level constructor {#low-level}

```go title="color/field.go"
func Field(name string, config Config, options ...field.PluginOption) field.Definition {
	encoded, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return field.Plugin(name, Key, encoded, options...)
}
```

Plugin config must be deterministic public JSON. Never put secrets, callbacks, or request-specific
data in it. If config properties contain collection slugs, declare those exact property names with
`CollectionReferenceKeys` so schema-addressed migration checks can follow renames safely.

Define Go/TypeScript/OpenAPI mappings, runtime validation, admin pairing, and any supported query
type in the plugin descriptor. Without those mappings generated values fall back to
`json.RawMessage`/`unknown`; without a server validator a pretty control is not a finished field.
Only common safe options such as required, localized, labels, descriptions, layout, and conditions
are accepted by `PluginOption`.

## Common mistakes {#troubleshooting}

- Do not call `field.Plugin` directly when the package provides a typed helper.
- Installing only the npm control or only the Go package creates an incomplete pair.
- Config is schema metadata, not the stored value.
- Test a custom field through resolution, generation, storage, Local API/REST, and the admin.

See [`field.Plugin`](/reference/field/plugin/), [Plugin system](/docs/plugins/), and the complete
[custom field guide](/guides/custom-fields/).
