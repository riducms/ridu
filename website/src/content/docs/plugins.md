---
title: 'Plugins'
description: 'Install, build, version, migrate, and test trusted Ridu extensions.'
product: plugins
eyebrow: 'Plugins'
order: 170
navigation:
  section: 'Extend Ridu'
  order: 10
  title: 'Plugin system'
---

Ridu plugins are trusted build dependencies. Their Go code is compiled into the server, optional
Svelte/TypeScript code is bundled into the admin, and a descriptor connects both halves for
generation, compatibility checks, and migrations.

Plugins add behavior such as fields, hooks, transports, endpoints, or admin views. Database and
object-storage packages are [adapters](/docs/adapters/) instead: they supply one runtime service and
do not belong in `Config.Plugins`.

> [!WARNING]
> Ridu never downloads plugin code at runtime and does not sandbox compiled dependencies. A plugin
> and its SQL have every capability granted to the application process and database role. Review,
> pin, test, and update them like any other server dependency.

## Install a plugin {#install}

Install both halves together when a package provides an admin integration:

```sh title="terminal"
npm run ridu -- plugin add color \
  --go-package example.com/acme/ridu-color \
  --go-version v1.2.0 \
  --admin-package @acme/ridu-color-admin \
  --admin-version '^1.2.0'
```

`ridu add` is the shorter alias. The command installs dependencies, updates `ridu.plugins.json`,
registers the Go and admin packages, and regenerates application types. If field types come from a
different npm package, install that package at the project root too. Failed setup or generation
restores the changed files.

Removal cleans up the paired admin package only when no remaining plugin uses it. Remove a separately
installed field-type package yourself after confirming nothing else imports it.

Use `--constructor` when the Go package exports a zero-argument constructor other than `New`.
`--no-install` is for an already-resolved package; it does not weaken compatibility checks.

Generated registration keeps application config small:

```go title="content/config.go" add={4}
func Config() ridu.Config {
	return ridu.Config{
		Name:        "Acme Editorial",
		Plugins:     installedPlugins(),
		Collections: []ridu.Collection{Users, Posts},
	}
}
```

Production never installs or discovers new code dynamically. A dependency change requires a new
build of the Go binary and admin assets.

## What a plugin can contribute {#capabilities}

The base `ridu.Plugin` interface contains only `Key() string`. Add the interfaces needed for each
capability:

| Capability               | Public interface            | Purpose                                                                                                               |
| ------------------------ | --------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Deterministic metadata   | `DescriptorProvider`        | Versions, compatibility, generated field types, admin pairing, and exceptional adapter-scoped database contributions. |
| Config transformation    | `ConfigTransformer`         | Add or transform authoring config before final validation. Plugins run in `Config.Plugins` order on defensive copies. |
| Lifecycle hooks          | `HookProvider`              | Append collection or field hooks after application-authored hooks, preserving plugin order.                           |
| Stored field validation  | `FieldValidatorProvider`    | Validate plugin field values with the resolved field, concrete runtime path, and candidate value.                     |
| Namespaced HTTP endpoint | `EndpointProvider`          | Add exact method/path handlers below `/api/plugins/<key>/`.                                                           |
| Protocol transport       | `TransportProvider`         | Bind an established absolute transport such as `/api/graphql` once the manifest and application are ready.            |
| Admin extensions         | descriptor `Admin` metadata | Statically register fields, routes, views, panels, navigation, document actions, shell components, and providers.     |

Executable functions, handlers, hooks, validators, credentials, and secrets never enter the public
manifest. Only deterministic metadata needed by generation and tooling belongs in the descriptor.

## Describe compatibility and generated types {#descriptor}

An advanced plugin returns a `ridu.PluginDescriptor`:

```go title="plugin.go"
func (plugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version:    "1.2.0",
		GoPackage:  "example.com/acme/ridu-color",
		APIVersion: ridu.PluginAPIVersion,
		Ridu: ridu.RiduCompatibility{
			Minimum: ridu.FrameworkVersion,
		},
		FieldTypes: []ridu.PluginFieldType{{
			Key:               "color",
			TypeScriptPackage: "@acme/ridu-color-admin",
			TypeScriptOutput:  "ColorValue",
			TypeScriptInput:   "ColorInput",
			TypeScriptWhere:   "ColorWhere",
			GoPackage:         "example.com/acme/ridu-color",
			GoType:            "Value",
			JSONSchema:        colorJSONSchema,
		}},
	}
}
```

The descriptor's TypeScript exports and optional Go type keep generated models, create/update input,
and query operands aligned. A JSON Schema adds the OpenAPI value schema. Omitting both Go type fields
uses `encoding/json.RawMessage`; a TypeScript value mapping is still required.

Schema resolution validates semantic versions, unique keys, capability/descriptor requirements,
field mappings, route conflicts, migration continuity, and table prefixes before the application
starts.

## Add endpoints {#endpoints}

Plugin endpoints declare an exact HTTP method and a relative path. Ridu mounts them below
`/api/plugins/<key>/`; a known path with the wrong method returns `405`.

The handler receives the request and writer, client IP, authenticated actor and auth collection,
local API, auth-attempt admission, and diagnostic reporting. Use `PluginEndpointContext.Local` for
content operations so access, validation, hooks, transactions, and redaction still apply.

Each endpoint may set `MaxBodyBytes`. Zero inherits the application handler bound; a negative value
opts trusted streaming code out, so the endpoint must supply its own explicit work and byte limits.
Authentication transports must call the provided admission function before amplifiable work.

Use `TransportProvider` only for an established absolute protocol location. It binds once from the
immutable manifest and application runtime; it should not rebuild schema state per request. The
[GraphQL plugin](/docs/graphql/) is the reference example.

## Pair the admin half {#admin-pairing}

`AdminPluginMetadata` names the installed npm package and named export, declares
`AdminPluginAPIVersion`, and carries a `PairingVersion`. It also lists authenticated
admin routes and package-relative assets in deterministic order.

Generation writes static imports. Before the admin is used, pairing resolution verifies
the backend/admin key, API versions, pairing version, field declarations, route and asset lists, and
route collisions. An admin route is mounted inside the authenticated shell, but route visibility is
presentation—not authorization. Its API calls still need server access rules.

Increment `PairingVersion` whenever separately published backend and admin versions cease to be
interchangeable. Publish both packages together for such a change.

## Plugin data and database escape hatches {#migrations}

Plugins should normally add stored records as ordinary collections and fields through
`ConfigTransformer`. The selected adapter then plans their storage while Ridu keeps access,
validation, hooks, transactions, REST, generated contracts, and admin behavior on the normal path.

Only a feature that genuinely needs private dialect-specific schema should use
`DatabaseContributions`. Each bundle names `PluginDatabaseAdapterPostgres` or
`PluginDatabaseAdapterSQLite`; Ridu executes only the matching bundle and fails clearly when the
active adapter is unsupported. SQL is never translated between adapters.

Each adapter's migration history starts at version 1. Every entry has a stable lowercase kebab-case
name and non-empty `UpSQL` and `DownSQL` arrays. Ridu copies the chosen direction into the
application's immutable migration artifact with a checksum and validates it again before execution.
Each SQL entry is one statement and cannot take control of the migration transaction or
connection. SQLite rejects `ATTACH`, `DETACH`, and `PRAGMA` entries.

Plugin tables must use `ridu_plugin_<key>_...`. This lets schema verification distinguish plugin
objects from Ridu tables. The prefix does not sandbox SQL.

Migration entries are immutable, including after removal and later reinstallation.
Changing a version, name, or SQL direction is rejected. Declared private tables must exist after
replay; SQLite also rejects changes to Ridu-managed schema and objects outside the plugin prefix.
Plugin downgrade steps run newest-first and require destructive approval when an application
artifact removes the plugin version. This reversible plugin history does not add automatic down
migrations for application schema; follow [Migrations](/docs/migrations/) for recovery and forward
corrections.

## Remove a plugin {#remove}

```sh title="terminal"
npm run ridu -- plugin remove color
npm run ridu -- migrate create --name remove-color
```

Removal unregisters generated code, removes Go and configured frontend dependencies, and regenerates contracts. The
CLI rolls those file changes back if the workflow fails. It never silently changes the database.

Review the resulting plugin down steps, data loss, and table removal in the application migration.
Back up and rehearse the removal before accepting a destructive artifact. If data must survive,
export or migrate it while the plugin and its handlers are still installed.

## Scaffold and test your own plugin {#create-and-test}

```sh title="terminal"
npm run ridu -- plugin new ./ridu-color \
  --key color \
  --module example.com/acme/ridu-color \
  --admin-package @acme/ridu-color-admin
```

The scaffold includes a Go module, field helper, descriptor, validator, Svelte field, exported value
type, and tests.

Call `plugintest.Run` from an external-package Go test:

```go title="plugin_test.go"
func TestPluginConformance(t *testing.T) {
	plugintest.Run(t, plugintest.Fixture{
		Plugin:      color.New(),
		Fields:      []field.Definition{color.Field("accent")},
		ValidData:   store.Values{"accent": "#663399"},
		InvalidData: store.Values{"accent": "not-a-color"},
		Compatibility: []plugintest.CompatibilityCase{
			{RiduVersion: ridu.FrameworkVersion, Compatible: true},
		},
	})
}
```

The suite checks deterministic resolution, manifest round-trip, exact field mappings, capability
versioning, compatibility rows, in-memory storage, local API, REST, and validation. Also compile the
generated Go/TypeScript/OpenAPI contracts, run admin checks and tests, and exercise both migration
directions plus full replay against every adapter the plugin declares.

## Version compatibility {#versioning}

| Version                   | Changes when                                                                |
| ------------------------- | --------------------------------------------------------------------------- |
| Plugin semantic `Version` | Every plugin publication.                                                   |
| `PluginAPIVersion`        | Ridu makes a breaking change to compiled Go capability interfaces.          |
| `AdminPluginAPIVersion`   | Ridu makes a breaking change to TypeScript/admin extension contracts.       |
| Plugin `PairingVersion`   | This plugin's backend and admin packages are no longer mutually compatible. |

Keep compatibility test rows for every Ridu version you support, including rejected lower and upper
boundaries. Silent behaviour changes under an existing API or pairing version are defects.

Official examples include [PostgreSQL](/docs/postgres/), [SQLite](/docs/sqlite/), [Rich
text](/docs/rich-text/), [SEO](/docs/seo/), [Form Builder](/docs/form-builder/),
[GraphQL](/docs/graphql/), and [local/S3 object storage](/docs/storage/). For a paired stored field,
follow [Build a custom field](/guides/custom-fields/).
