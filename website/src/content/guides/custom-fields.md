---
title: 'Build a field plugin'
description: 'Create a new field type with Go validation, generated types, and a Svelte editor.'
product: guides
eyebrow: 'Guide'
order: 20
navigation:
  section: 'Extend Ridu'
  parent: 'plugins'
  order: 60
  title: 'Build a field plugin'
---

Use a field plugin when you need a new field type with its own Go validation or data format.
A field plugin has a Go package for the server and a Svelte/TypeScript package for its admin editor.
Ridu loads both when an application installs the plugin.

This guide builds a `color` field that stores a `#RRGGBB` string, rejects invalid colors on the
server, and provides a Svelte editor. It also tells Ridu which Go and TypeScript types to generate
for applications that use the field.

## Change an existing field's editor {#local-editors}

If you only want a different input, a preview, or a character counter, start with
[Field components](/docs/custom-components/field-components/). That tutorial works directly in
your application's `admin.config.ts` and does not require a plugin. Built-in
[TextList and NumberList](/docs/fields/lists/) editors also use this path: register
`type: "text-list"` or `type: "number-list"` and use the matching typed array binding.

For dashboards, navigation, document screens, and other UI changes, use
[Custom components](/docs/custom-components/).

## 1. Create the plugin files {#scaffold}

```sh title="terminal"
bun run ridu plugin new ./ridu-color \
  --key color \
  --module example.com/acme/ridu-color \
  --admin-package @acme/ridu-color-admin
```

The command creates the Go and Svelte packages, their configuration, and tests. Replace the starter
field with the color implementation below. Keep the generated supported Ridu versions until you
have tested the plugin with other versions.

## 2. Define the Go value and field helper {#go-field}

The helper lets an application write `color.Field("accent")` in its collection. The exported
`Value` type tells Go applications that this field contains a color string.

```go title="plugin.go" focus={12-13,20-23}
package color

import (
	"encoding/json"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

const Key = "color"

// Value is the color type used in generated Go documents.
type Value string

type plugin struct{}

func New() ridu.Plugin     { return plugin{} }
func (plugin) Key() string { return Key }

func Field(name string) field.PluginField {
	// This field does not need any per-field configuration.
	return field.Plugin(name, Key, json.RawMessage(`{}`))
}
```

Returning `field.PluginField` lets applications add options such as `.Required()`, `.Localized()`,
`.Access(...)`, `.Hooks(...)`, `.Validate(...)`, or `.Admin(...)` after calling your helper. Those
settings apply wherever the field is used, including inside a group or block. Plugin fields do
not have the built-in fields' `.Unique()` or `.Default()` methods.

The third argument to `field.Plugin` contains public settings for the field. This color field needs
none, so it passes `{}`. Settings are sent to the admin: never put credentials or secrets there.

If your field's stored JSON contains collection names, declare the property names with
`.CollectionReferenceKeys(...)`. For example, the rich-text plugin declares `relationTo`. This lets
Ridu update those names when a collection is renamed and detect unsafe changes to saved references.

## 3. Describe the field and its admin package {#descriptor}

The descriptor tells Ridu which Go and TypeScript types represent the color value, and which admin
package supplies its editor:

```go title="descriptor.go" focus={9-15,25-35}
package color

import "github.com/riducms/ridu"

// Match pairingVersion in the TypeScript admin plugin.
const AdminPluginPairingVersion = 1

func (plugin) Descriptor() ridu.PluginDescriptor {
	// Tell Ridu which admin package and export to import.
	admin := ridu.AdminPluginMetadata{
		Package:        "@acme/ridu-color-admin",
		Export:         "colorAdminPlugin",
		APIVersion:     ridu.AdminPluginAPIVersion,
		PairingVersion: AdminPluginPairingVersion,
	}

	return ridu.PluginDescriptor{
		Version:    "1.0.0",
		GoPackage:  "example.com/acme/ridu-color",
		APIVersion: ridu.PluginAPIVersion,
		Ridu: ridu.RiduCompatibility{
			Minimum: ridu.FrameworkVersion,
		},
		Admin: &admin,
		// Import these value types into generated application code.
		FieldTypes: []ridu.PluginFieldType{{
			Key:               Key,
			TypeScriptPackage: "@acme/ridu-color-admin",
			TypeScriptOutput:  "Color",
			TypeScriptInput:   "ColorInput",
			GoPackage:         "example.com/acme/ridu-color",
			GoType:            "Value",
			JSONSchema: []byte(
				`{"type":"string","pattern":"^#[0-9A-Fa-f]{6}$"}`,
			),
		}},
	}
}
```

`TypeScriptOutput` and `TypeScriptInput` name the types you will export in step 5. `GoPackage` and
`GoType` name the Go `Value` type above. `JSONSchema` describes valid colors in OpenAPI.
The `Admin` settings identify the package and exported plugin object from step 6.

`TypeScriptWhere` is optional. Without it, the generated client uses Ridu's standard single-value
query filters (`ScalarWhere<Color>`). Specify it only if the field needs a different filter type.
If you omit both `GoPackage` and `GoType`, Go receives `json.RawMessage` instead of `Value`.

## 4. Validate colors on the server {#validation}

The Go validator runs whenever a value is saved, whether it comes from the admin, an API request,
or application code:

```go title="validation.go" focus={19-29}
package color

import (
	"regexp"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/schema"
)

var hexadecimal = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func (plugin) FieldValidators() map[string]ridu.PluginFieldValidator {
	return map[string]ridu.PluginFieldValidator{Key: validateColor}
}

func validateColor(
	ctx ridu.PluginFieldValidationContext,
) []schema.Issue {
	value, ok := ctx.Value.StringValue()
	if ok && hexadecimal.MatchString(value) {
		// No issues means the value passed validation.
		return nil
	}
	// RuntimePath identifies the exact field, including array rows.
	return []schema.Issue{{
		Code:    "invalid_color",
		Path:    ctx.RuntimePath,
		Message: "color must use the #RRGGBB format",
	}}
}
```

Return `ctx.RuntimePath` with the error so the admin can show it beside the right input, even
inside an array such as `items.2.accent`. Keep the error code stable so API clients can recognize
it. Ridu checks `.Required()` separately; this function checks whether a supplied color is valid.

## 5. Export and check the TypeScript value {#typescript-contract}

The names must exactly match `TypeScriptOutput` and `TypeScriptInput` in the Go descriptor:

```ts title="admin/src/value.ts" focus={5-9}
export type Color = `#${string}`;
// Reads and writes use the same value shape for this field.
export type ColorInput = Color;

// TypeScript's # prefix type does not check the six hex digits.
export function decodeColor(value: unknown): Color {
	if (typeof value !== 'string' || !/^#[0-9a-f]{6}$/i.test(value))
		throw new Error('Expected a six-digit hexadecimal color.');
	return value as Color;
}
```

`Color` gives TypeScript callers a useful type. It cannot check how many hexadecimal digits a
string contains, so `decodeColor` checks the actual value in the browser. Go performs the same
check on the server.

## 6. Build the Svelte editor {#svelte-field}

```svelte title="admin/src/color-field.svelte" focus={10-17,19-21}
<script lang="ts">
	import type { PluginFieldProps } from '@riducms/plugin';
	import { decodeColor, type Color } from './value';
	let { field }: PluginFieldProps<Color> = $props();
</script>

<label for={field.schema.id}>
	{field.schema.admin.label}
</label>
<!-- Black is an empty-state preview; oninput sets the form value. -->
<input
	id={field.schema.id}
	type="color"
	value={field.value ?? '#000000'}
	disabled={field.readOnly}
	oninput={(event) =>
		field.set(decodeColor(event.currentTarget.value))}
/>
{#each field.issues as issue (issue.code)}
	<p>{issue.message}</p>
{/each}
```

The component reads the current value from `field.value` and updates the unsaved form with
`field.set(...)`. Saving the document sends it to Go for validation. `field.readOnly` disables the
control when the user cannot edit it, and `field.issues` supplies messages to display after a
validation failure.

Ridu keeps the editor attached to its row when rows move. If an API request started by your
component finishes after the user closes the editor or changes document, discard the result
when `field.stale` is true. Saving or resetting the form also makes the old field object stale.

The example uses a native color picker. Its black preview for an empty field is only a display
fallback: it does not set a value until the user chooses a color. A required empty field still
fails validation on save.

Export the editor from the admin package:

```ts title="admin/src/index.ts" focus={10-18}
import {
	defineAdminPlugin,
	definePluginField
} from '@riducms/plugin/authoring/v1';
import ColorField from './color-field.svelte';
import { decodeColor } from './value';
export type { Color, ColorInput } from './value';

export const colorAdminPlugin = defineAdminPlugin({
	// Match the Go plugin's Key and AdminPluginPairingVersion.
	key: 'color',
	pairingVersion: 1,
	fields: {
		color: definePluginField({
			component: ColorField,
			// Check incoming data before the editor receives a Color.
			decodeValue: decodeColor
		})
	}
});
```

The plugin's `key` and `pairingVersion` must match the Go descriptor from step 3. The version number
lets Ridu check that the Go and admin packages work together. Under `fields`, `color` matches the Go
field type's key. `decodeColor` checks a value before the editor receives it. The import from
`authoring/v1` selects this version of Ridu's plugin API.

The two helpers serve different purposes:

- [`definePluginField`](/reference/plugin/define-plugin-field/) connects `ColorField` to its value
  decoder and returns a field registration. Its options also let you decode editor settings or
  accept a write value with a different shape from the saved value.
- [`defineAdminPlugin`](/reference/plugin/define-admin-plugin/) collects those field registrations
  and any other plugin UI into the exported `colorAdminPlugin` object. The Go descriptor names
  this export so Ridu can load it when the application installs the plugin.

See [PluginFieldProps](/reference/plugin/plugin-field-props/) for everything passed to `ColorField`,
including `field`, `form`, and document-picking tools. If your plugin needs to offer a second editor
for an existing field, use [defineFieldComponent](/reference/plugin/define-field-component/) in its
`components` map; the application chooses it explicitly with
[field.PluginComponent](/reference/field/plugin-component/).

Run `ridu check` in an application using the plugin to catch a missing component, mismatched
field type, or invalid registration.

## 7. Test the field {#testing}

Put this test in the separate `color_test` package. It checks that the plugin loads, generates the
declared types, and accepts or rejects data through the local API and REST:

```go title="plugin_test.go" focus={19-23}
package color_test

import (
	"testing"

	"github.com/riducms/ridu"
	color "example.com/acme/ridu-color"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugintest"
	"github.com/riducms/ridu/store"
)

func TestConformance(t *testing.T) {
	plugintest.Run(t, plugintest.Fixture{
		Plugin: color.New(),
		Fields: field.Fields{
			color.Field("accent").Required(),
		},
		ValidData:   store.Values{"accent": store.String("#663399")},
		InvalidData: store.Values{"accent": store.String("purple")},
		Compatibility: []plugintest.CompatibilityCase{
			{RiduVersion: ridu.FrameworkVersion, Compatible: true},
		},
	})
}
```

Keep the generated admin test and run `svelte-check`. Also compile an application that uses `Color`,
`ColorInput`, and its generated client. If a plugin adds database tables, test applying and reversing
its migrations on each supported database. See [Testing](/docs/testing/) and [Plugins](/docs/plugins/).

## 8. Install and use it {#install}

After publishing the Go and admin packages, install them into an application:

```sh title="terminal"
bun run ridu plugin add color \
  --go-package example.com/acme/ridu-color \
  --go-version v1.0.0 \
  --admin-package @acme/ridu-color-admin \
  --admin-version '^1.0.0'
```

The CLI installs the Go and admin packages and updates the generated imports. Keep
`Plugins: installedPlugins()` in `content.Config()` and `plugins: generatedAdminPlugins` in
`admin.config.ts` so both packages load.

Use the field helper in a collection:

```go title="content/brands.go" focus={11-13}
package content

import (
	"github.com/riducms/ridu"
	color "example.com/acme/ridu-color"
	"github.com/riducms/ridu/field"
)

var Brands = ridu.Collection{
	Slug: "brands",
	Fields: field.Fields{
		color.Field("accent").Required(),
	},
}
```

Add `content.Brands` to your Go config's `Collections` list. With `ridu dev` running, save the
config and open Brands in the admin. Create a brand, choose an accent color, save, and reload it.
The selected color should remain. Sending `"purple"` through the API should return `invalid_color`.

Before deployment, review the generated types, create and check the database migration, and run
`ridu check`.

## Fix common problems {#failure-modes}

| Symptom                                               | What to check                                                                                     |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Ridu reports an unknown plugin field                  | Check that `color.New()` is registered and the helper uses the same `color` key.                  |
| Generated TypeScript has the wrong type               | Check `FieldTypes` in the descriptor and its named TypeScript exports, then run `ridu check`.     |
| Generated Go uses `json.RawMessage`                   | Supply both `GoPackage` and the exported `GoType`.                                                |
| The admin cannot find the color editor                | Install the admin package and check its export name and field key.                                |
| Ridu reports an admin version mismatch                | Check that the Go and admin packages use the same `PairingVersion` and supported API version.     |
| Bun or Vite cannot find a generated import            | Install the package named in the descriptor and check that it exports the expected value.         |
| Validation appears on the wrong row                   | Set the error's `Path` to `ctx.RuntimePath`, which includes the current row number.               |
| The browser accepts a value the API rejects           | Check that the input, TypeScript types, JSON Schema, and Go validator all accept the same values. |
| A secret appears in `/api/schema`                     | Remove it from the field settings; they are public and sent to the browser.                       |
| A renamed plugin appears as a removal and an addition | Restore its previous key or write a migration for the rename.                                     |

Ridu stores custom field values as JSON through the application's database adapter. A plugin that
needs its own tables or indexes must provide and test the corresponding database migrations.
