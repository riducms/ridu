---
title: 'Build a custom field'
description: 'Ship one exact value contract across Go, OpenAPI, TypeScript, and the Svelte admin.'
product: guides
eyebrow: 'Guide'
order: 20
navigation:
  section: 'Extend Ridu'
  parent: 'plugins'
  order: 60
  title: 'Build a custom field'
---

A reusable Ridu field combines:

- a Go plugin defines schema config, the stored value, server validation, and generated type metadata;
- an npm package exports the TypeScript value types and a Svelte field renderer; and
- a shared plugin key and pairing version keep the server and admin compatible.

This guide builds a `color` field that stores a `#RRGGBB` string, validates it on the server, exports
exact generated types, and renders it in the admin.

This is the full path for adding a new value contract. If a built-in field already has the storage,
validation, access, and generated types you need, keep those semantics and replace only its Svelte
editor with `field.AdminComponent`. See
[Custom components](/docs/fields/#custom-components) for the smaller configuration,
registration example, and the exact comparison with Payload's field-component slots.

## 1. Scaffold the paired plugin {#scaffold}

```sh title="terminal"
npm run ridu -- plugin new ./ridu-color \
  --key color \
  --module example.com/acme/ridu-color \
  --admin-package @acme/ridu-color-admin
```

The command creates Go and Svelte packages with a descriptor, field helper, validator, value export,
and tests. Replace the starter value with the color implementation below. Change the generated Ridu
compatibility range only after testing the new range.

## 2. Define the Go value and field helper {#go-field}

The helper writes field config into the schema manifest. Export a distinct Go value type so generated
application models do not fall back to `json.RawMessage`.

```go title="plugin.go"
package color

import (
	"encoding/json"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

const Key = "color"

type Value string

type Config struct {
	Palette []Value `json:"palette,omitempty"`
}

type plugin struct{}

func New() ridu.Plugin     { return plugin{} }
func (plugin) Key() string { return Key }

func Field(name string, config Config, options ...field.PluginOption) field.Definition {
	encoded, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return field.Plugin(name, Key, encoded, options...)
}
```

`field.PluginOption` accepts common capabilities such as
`field.Label`, `field.Required`, `field.Description`, `field.ReadOnly`, `field.Columns`,
`field.Tab`, `field.ShowWhen`, and `field.Localized`. It does not accept `field.Unique` or a generic
default because a plugin must define its own storage and indexing semantics before those could be
safe.

If plugin config contains collection slugs, declare their exact JSON property names with
`field.CollectionReferenceKeys(...)`. Ridu can then update references during a schema rename and
reject a change that would leave current values or version snapshots pointing at a removed target.

> [!WARNING]
> Plugin config is public schema metadata delivered to tooling and the admin. Do not put secrets,
> credentials, executable access rules, or request-specific data in it.

## 3. Declare generated types and the admin pair {#descriptor}

`PluginDescriptor.FieldTypes` maps the field to generated Go, TypeScript, and OpenAPI types. The
admin metadata names the renderer export.

```go title="descriptor.go"
package color

import "github.com/riducms/ridu"

const AdminPluginPairingVersion = 1

func (plugin) Descriptor() ridu.PluginDescriptor {
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

Replace the example compatibility values with the range you test. `TypeScriptWhere` is optional;
without it, the generated client uses `ScalarWhere<Color>`. Supply a named where export only when
the field supports a different query shape.

Omitting both `GoPackage` and `GoType` produces `json.RawMessage`. Omitting descriptor field mappings
makes TypeScript output `unknown`. Provide both mappings for a reusable field.

## 4. Enforce the value on the server {#validation}

The Go validator checks local API, REST, SDK, admin, task, and plugin-transport writes:

```go title="validation.go"
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

func validateColor(ctx ridu.PluginFieldValidationContext) []schema.Issue {
	value, ok := ctx.Value.StringValue()
	if ok && hexadecimal.MatchString(value) {
		return nil
	}
	return []schema.Issue{{
		Code:    "invalid_color",
		Path:    ctx.RuntimePath,
		Message: "color must use the #RRGGBB format",
	}}
}
```

Use a stable machine-readable code and `RuntimePath`. Unlike the schema-level `Field.Path`, the
runtime path identifies the concrete occurrence inside arrays and blocks, so the admin can attach
an issue to the correct repeated row. Required/missing validation is still handled by
`field.Required`; the plugin validator owns the value's shape and semantics.

## 5. Export the TypeScript contract {#typescript-contract}

The names must exactly match `TypeScriptOutput` and `TypeScriptInput` in the Go descriptor:

```ts title="admin/src/value.ts"
export type Color = `#${string}`;
export type ColorInput = Color;
```

This template-literal type improves completion but cannot express “exactly six hexadecimal digits.”
The JSON Schema supplies that pattern to OpenAPI and the Go validator enforces it at runtime. Keep
all three aligned; none substitutes for the other two.

## 6. Render through the form controller {#svelte-field}

The Svelte component receives the resolved `SchemaField` and a `FieldForm`. Read and write the field
through the form; do not submit documents or call REST from the component.

```svelte title="admin/src/color-field.svelte"
<script lang="ts">
	import type { FieldComponentProps } from "@riducms/plugin";

	let { field, form }: FieldComponentProps = $props();
	const value = $derived(String(form.get(field.path) ?? "#000000"));
	const issues = $derived(form.issuesFor(field.path));

	$effect(() => form.register(field.path));
</script>

<div class="grid gap-2">
	<label for={field.id}>{field.admin.label}</label>
	<input
		id={field.id}
		type="color"
		value={value}
		disabled={field.admin.readOnly}
		oninput={(event) => form.set(field.path, event.currentTarget.value)}
	/>
	{#each issues as issue (issue.code)}
		<p>{issue.message}</p>
	{/each}
</div>
```

`form.register` returns the unregister function, which the Svelte effect uses for cleanup. Respect
`field.admin.readOnly`, keep the input ID/label association, display server issues, and treat
`field.plugin?.config` as untrusted `unknown` until the admin package narrows it.

Register that renderer and export the paired admin descriptor:

```ts title="admin/src/index.ts"
import { ADMIN_PLUGIN_API_VERSION, defineAdminPlugin, defineFieldPlugin } from '@riducms/plugin';

import ColorField from './color-field.svelte';
export type { Color, ColorInput } from './value';

export const colorFieldPlugin = defineFieldPlugin({
	type: 'plugin',
	key: 'color',
	component: ColorField,
	canRender: (field) => field.plugin?.key === 'color'
});

export const colorAdminPlugin = defineAdminPlugin({
	apiVersion: ADMIN_PLUGIN_API_VERSION,
	key: 'color',
	pairingVersion: 1,
	fields: [colorFieldPlugin]
});
```

The key, API version, pairing version, field registrations, routes, and assets must match the backend
declaration. Increment the pairing version when separately published Go and admin packages stop being
interchangeable.

## 7. Test the paired contract {#testing}

Put the conformance test in the external `color_test` package. It checks resolution, generated
mapping, local API, REST, and validation:

```go title="plugin_test.go"
package color_test

import (
	"testing"

	color "example.com/acme/ridu-color"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugintest"
	"github.com/riducms/ridu/store"
)

func TestConformance(t *testing.T) {
	plugintest.Run(t, plugintest.Fixture{
		Plugin: color.New(),
		Fields: []field.Definition{
			color.Field("accent", color.Config{}, field.Required()),
		},
		ValidData:   store.Values{"accent": store.String("#663399")},
		InvalidData: store.Values{"accent": store.String("purple")},
		Compatibility: []plugintest.CompatibilityCase{
			{RiduVersion: ridu.FrameworkVersion, Compatible: true},
		},
	})
}
```

Keep the scaffolded admin test, run `svelte-check`, and compile generated output that imports
`Color`, `ColorInput`, and the application client. If the plugin adds database changes, test both
migration directions and full replay against every declared adapter. See [Testing](/docs/testing/) and the broader
[Plugin system](/docs/plugins/).

## 8. Install and use it {#install}

After publishing both packages, install the pair into an application:

```sh title="terminal"
npm run ridu -- plugin add color \
  --go-package example.com/acme/ridu-color \
  --go-version v1.0.0 \
  --admin-package @acme/ridu-color-admin \
  --admin-version '^1.0.0'
```

The CLI records the dependencies, emits compiled Go registration and a static admin import, and
regenerates contracts. Use the helper like a built-in field:

Keep `Plugins: installedPlugins()` in `content.Config()` so the generated registration is loaded.

```go title="content/brands.go"
var Brands = ridu.Collection{
	Slug: "brands",
	Fields: []field.Definition{
		color.Field(
			"accent",
			color.Config{Palette: []color.Value{"#663399", "#FFFFFF"}},
			field.Required(),
		),
	},
}
```

With `ridu dev` running, save the config and use the field in the admin. Then inspect the generated
manifest, OpenAPI, Go, and TypeScript diffs.

Before deployment, create and review the required database migration, verify its history, and run
`ridu check`.

## Failure modes {#failure-modes}

| Symptom                                        | Likely boundary and fix                                                                              |
| ---------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Unknown plugin field during resolution         | Register `color.New()` in compiled config and keep the helper key identical.                         |
| Generated TypeScript is `unknown`              | Add the descriptor `FieldTypes` mapping, run `plugintest.Run`, and save while `ridu dev` is running. |
| Generated Go uses `json.RawMessage`            | Supply both `GoPackage` and exported `GoType`, or accept the fallback.                               |
| Admin says no renderer handles `plugin:color`  | Install/export the admin pair, check its key, and let `ridu dev` rebuild the static registry.        |
| Admin pairing/API mismatch                     | Publish compatible halves, align `PairingVersion`, and keep `AdminPluginAPIVersion` current.         |
| Bun/Vite cannot resolve the generated import   | Install the descriptor's exact npm package/export; do not edit the generated registry.               |
| Nested validation appears on the wrong row     | Return `schema.Issue.Path: ctx.RuntimePath`, not only the schema-level path.                         |
| Browser accepts a value REST later rejects     | Align the input control, exported input type, JSON Schema, and authoritative Go validator.           |
| A field-config secret appears in `/api/schema` | Remove it immediately; manifest config is public deterministic metadata.                             |
| A renamed plugin key looks removed and added   | Restore the key or plan a data and plugin migration.                                                 |

Custom field data uses generic JSONB persistence. A plugin that needs indexes, custom tables, or
different query semantics must declare and test those capabilities.
