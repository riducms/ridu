<!-- Generated from website/src/content/docs/plugins.md by scripts/sync-agent-docs.ts. -->

# Plugins

Plugins add reusable features to your application, such as rich text, SEO fields, or a GraphQL API.
A plugin has a Go package and may also include a Svelte/TypeScript package for its admin components.
Both become part of your application when you build it.

If you want to change an input, add a dashboard panel, or customize another part of the admin,
start with [Custom components](./custom-components.md). You can register application components
directly. The guide below is for installing or building plugins that add server behavior or new
field types. For database connections and file storage, use [adapters](https://riducms.com/docs/adapters/).

Install plugins you trust: their Go code runs with the same permissions as your application.
Choose package versions explicitly and test updates before deploying them.

## Install a plugin {#install}

If a plugin includes admin components, install its Go and admin packages together:

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

Use `--constructor` when the Go package exports a zero-argument constructor other than `New`.
Use `--no-install` when you have already installed the dependencies; Ridu still checks compatibility.

Keep the generated `installedPlugins()` call in your Go config. The CLI updates the function when
you add or remove a plugin:

```go title="content/config.go" add={4-5}
func Config() ridu.Config {
	return ridu.Config{
		Name: "Acme Editorial",
		// Load the plugins registered by ridu plugin add.
		Plugins:     installedPlugins(),
		Collections: []ridu.Collection{Users, Posts},
	}
}
```

Rebuild and deploy your application after changing its plugins.

## Choose what your plugin adds {#capabilities}

Every plugin implements `Key() string` to give it a unique name. Add the interfaces needed for
the features you want to provide:

| I want to…                                     | Use                               | What it does                                                                                |
| ---------------------------------------------- | --------------------------------- | ------------------------------------------------------------------------------------------- |
| Declare versions and generated types           | `DescriptorProvider`              | Describes supported Ridu versions, field types, and the admin package.                      |
| Add collections or change application settings | `ConfigTransformer`               | Changes the Go config before Ridu validates it.                                             |
| Add or change fields                           | `FieldGraphTransformer`           | Edits a collection or global's fields, including their access rules, hooks, and validation. |
| Check field configuration                      | `FieldGraphValidator`             | Checks the final field definitions after plugins have changed them.                         |
| Run code when documents change                 | `HookProvider`                    | Adds collection hooks after the application's own hooks.                                    |
| Validate a custom field's data                 | `FieldValidatorProvider`          | Checks the value being saved and reports errors at the affected field.                      |
| Add an HTTP endpoint                           | `EndpointProvider`                | Registers a method and path below `/api/plugins/<key>/`.                                    |
| Add an API such as GraphQL                     | `TransportProvider`               | Registers an API at a fixed path such as `/api/graphql`.                                    |
| Add admin components                           | The descriptor's `Admin` property | Loads the plugin's fields, pages, dashboard panels, and other components.                   |

Plugins run in the order of `Config.Plugins`. Descriptors are public: include package and type
information, but keep executable functions, credentials, and secrets in Go code or server settings.

## Describe compatibility and generated types {#descriptor}

Implement `Descriptor()` when a plugin supplies generated types, an admin package, endpoints,
or other features that need build configuration. For a custom color field, it describes which
types the application will use:

```go title="plugin.go" focus={9-19}
func (plugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version:    "1.2.0",
		GoPackage:  "example.com/acme/ridu-color",
		APIVersion: ridu.PluginAPIVersion,
		Ridu: ridu.RiduCompatibility{
			Minimum: ridu.FrameworkVersion,
		},
		// Name the types that generated Go and TypeScript code imports.
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

`TypeScriptOutput` names the type returned when reading a document. `TypeScriptInput` names the
type accepted when creating or updating it, and `TypeScriptWhere` describes its query filters.
`GoPackage` and `GoType` name the Go value type. If you omit both Go settings, generated Go code
uses `encoding/json.RawMessage`.

`JSONSchema` describes the value in OpenAPI. Ridu checks these settings, plugin versions, and route
conflicts before the application starts. [Build a field plugin](./custom-fields.md) shows a
complete descriptor, editor, and validator working together.

## Add endpoints {#endpoints}

Plugin endpoints declare an exact HTTP method and a relative path. Ridu mounts them below
`/api/plugins/<key>/`; a known path with the wrong method returns `405`.

The handler receives the HTTP request, response writer, and information about the signed-in user.
Use `PluginEndpointContext.Local` to read or change content with Ridu's normal permissions,
validation, and hooks.

Set `MaxBodyBytes` to limit request size, or leave it at zero to use the application's limit.
A negative value disables that limit; a streaming handler must then enforce its own limits.
If your endpoint handles authentication, call `AdmitAuthAttempt` before checking credentials or
performing other expensive work. See the [endpoint context reference](https://riducms.com/reference/core/plugin-endpoint-context/).

Use `TransportProvider` for an API with a fixed top-level path, such as GraphQL. Set it up once
when the application starts. The [GraphQL plugin](./graphql.md) shows this approach.

## Connect the admin package {#admin-pairing}

Set the descriptor's `Admin` property to identify the JavaScript package and its exported plugin
object. `APIVersion` must match `ridu.AdminPluginAPIVersion`. `PairingVersion` is your own version
number for the connection between the Go and admin packages: increase it when an older version
of either package would no longer work with the other.

Ridu generates the imports and checks that both packages agree on their key, versions, fields,
routes, and assets. Publish compatible Go and admin packages together.

Create the JavaScript export with [defineAdminPlugin](https://riducms.com/reference/plugin/define-admin-plugin/).
Use [definePluginField](https://riducms.com/reference/plugin/define-plugin-field/) for each new field type's default
editor, or [defineFieldComponent](https://riducms.com/reference/plugin/define-field-component/) for an alternative
editor selected by an application. Each reference page lists its options and includes a working
registration example. [Build a field plugin](./custom-fields.md) connects them to a Go plugin
from start to finish.

Plugin pages require sign-in. Their API requests still need server access rules, just like requests
from the built-in admin.

## Store plugin data {#migrations}

Store plugin data in ordinary collections and fields when possible. Add them through
`ConfigTransformer` to get Ridu's usual permissions, validation, API, generated types, and admin.

If a feature needs its own SQL tables, use `DatabaseContributions`. Write a separate set of
migrations for PostgreSQL (`PluginDatabaseAdapterPostgres`) and SQLite
(`PluginDatabaseAdapterSQLite`). Ridu runs only the set for the selected adapter; it cannot
translate one database's SQL for another.

Number each adapter's migrations from 1 without gaps. Give each a name such as `add-audit-log`,
SQL statements to apply it in `UpSQL`, and SQL statements to undo it in `DownSQL`. Each array entry
must contain one statement. Ridu manages the connection and transaction; SQLite also rejects
`ATTACH`, `DETACH`, and `PRAGMA` statements.

Start table names with `ridu_plugin_<key>_`, replacing hyphens in the plugin key with underscores.
For example, a plugin named `audit-log` could create `ridu_plugin_audit_log_entries`. This lets
Ridu check which tables the plugin owns. The prefix does not restrict what the plugin's SQL can do.

Ridu includes the plugin's SQL in the application's generated migration and checks that it has not
changed before running it. Once published, do not edit a migration's version, name, or SQL. Add a
new migration to make further changes, even after removing and reinstalling a plugin.

Removing a plugin version runs its undo steps in reverse order and requires approval for the
destructive migration. This applies to the plugin's tables; it does not add automatic rollback for
the rest of the application's schema. See [Migrations](./migrations.md) for recovery options.

## Remove a plugin {#remove}

```sh title="terminal"
npm run ridu -- plugin remove color
npm run ridu -- migrate create --name remove-color
```

Removal updates the generated imports and types, removes the Go dependency, and removes the admin
package if no remaining plugin uses it. Remove any separately installed type package yourself after
checking that nothing else imports it. The CLI restores changed files if removal fails; it does not
change the database.

Review which tables and data the generated migration will remove. Back up the database and try
the migration on a copy before applying it in production. Export or migrate any data you need to
keep while the plugin is still installed.

## Create and test a plugin {#create-and-test}

```sh title="terminal"
npm run ridu -- plugin new ./ridu-color \
  --key color \
  --module example.com/acme/ridu-color \
  --admin-package @acme/ridu-color-admin
```

The scaffold includes a Go module, field helper, descriptor, validator, Svelte field, exported value
type, and tests.

Call `plugintest.Run` from an external-package Go test:

```go title="plugin_test.go" focus={7-9}
func TestPluginConformance(t *testing.T) {
	plugintest.Run(t, plugintest.Fixture{
		Plugin: color.New(),
		Fields: field.Fields{
			color.Field("accent"),
		},
		// Exercise acceptance and rejection through the plugin test suite.
		ValidData:   store.Values{"accent": store.String("#663399")},
		InvalidData: store.Values{"accent": store.String("not-a-color")},
		Compatibility: []plugintest.CompatibilityCase{
			{RiduVersion: ridu.FrameworkVersion, Compatible: true},
		},
	})
}
```

The test checks that the plugin loads, generates its declared types, accepts valid data, and rejects
invalid data through both the local API and REST. It also checks the Ridu versions you list under
`Compatibility`.

Compile a generated application that uses the plugin and run its admin checks. If the plugin adds
database tables, test applying and reversing its migrations on every supported adapter.

## Choose version numbers {#versioning}

| Version                   | Changes when                                                                |
| ------------------------- | --------------------------------------------------------------------------- |
| Plugin semantic `Version` | Every plugin publication.                                                   |
| `PluginAPIVersion`        | Ridu makes a breaking change to its Go plugin API.                          |
| `AdminPluginAPIVersion`   | Ridu makes a breaking change to its admin plugin API.                       |
| Plugin `PairingVersion`   | This plugin's backend and admin packages are no longer mutually compatible. |

Test every Ridu version you claim to support, along with versions just outside that range.

Official plugins include [Rich text](./rich-text.md), [SEO](./seo.md),
[Form Builder](./form-builder.md), and [GraphQL](./graphql.md).
For a complete new field type, follow [Build a field plugin](./custom-fields.md).
