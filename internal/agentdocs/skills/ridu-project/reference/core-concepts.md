<!-- Generated from website/src/content/docs/core-concepts.md by scripts/sync-agent-docs.ts. -->

# Core concepts

You author executable Go config. Ridu resolves it into an immutable manifest, generates exact Go,
OpenAPI, and TypeScript contracts, and applies one operation engine to every runtime entry point.

The generated contracts are shared by the server and browser, while the application server remains
a Go binary. See [Go for TypeScript developers](./go-for-typescript.md) for the syntax and tooling.

For the types used in examples, read [Go packages](./go-packages.md). It explains `operation`,
`store`, `query`, and `schema`, including the differences between callback values, document data,
and filter values.

## 1. Config is executable authoring code {#config}

Your application returns a `ridu.Config`. Collections, globals, fields, access rules, hooks, tasks,
storage, and compiled plugins meet here:

```go title="content/config.go"
package content

import "github.com/riducms/ridu"

func Config() ridu.Config {
	return ridu.Config{
		Name:        "Acme Editorial",
		Admin:       ridu.AdminConfig{User: "users"},
		Plugins:     installedPlugins(),
		Collections: []ridu.Collection{Users, Posts, Media},
	}
}
```

Field shape, labels, plugin descriptors, and admin presentation are data. Access rules, hooks,
validators, task handlers, endpoint handlers, storage credentials, and database setup remain Go
values and functions.

Helpers, conditionals, and ordinary Go packages can participate in config. Executable functions
and secrets are not serialized into the manifest. See [Configuration](./configuration.md) for the
complete surface.

## 2. Resolution produces the canonical manifest {#manifest}

`ridu.Resolve` applies plugin config transforms in order, validates the model, derives stable
identities, and returns `schema.Manifest`. It does not open a database or start a server:

```go title="content/config_test.go"
func TestConfigResolves(t *testing.T) {
	manifest, err := ridu.Resolve(Config())
	if err != nil {
		t.Fatal(err)
	}

	snapshot := manifest.Snapshot()
	if snapshot.Application.Name != "Acme Editorial" {
		t.Fatalf("application name = %q", snapshot.Application.Name)
	}
}
```

The manifest is immutable from a consumer's point of view. `Snapshot()` returns a deep copy and
`Bytes()` produces deterministic JSON. It contains the public metadata required by runtime,
generation, migrations, and admin tooling.

Config errors arrive together as path-aware `schema.ValidationError` issues. Change the Go config,
not generated files or the manifest.

## 3. Generate contracts {#generated-contracts}

`ridu generate` resolves the config and atomically writes these contracts:

| Output                                | Consumer                                                          |
| ------------------------------------- | ----------------------------------------------------------------- |
| `generated/ridu.schema.json`          | Review, tooling, migration history, and runtime schema inspection |
| `generated/ridu.openapi.json`         | REST clients and API tooling                                      |
| `generated/ridu.generated.go`         | Typed local collection/global models and handles                  |
| `generated/ridu.generated.ts`         | Application-bound Fetch SDK types and methods                     |
| `admin/src/ridu.plugins.generated.ts` | Validated static imports for paired admin plugins                 |

Commit these generated artifacts with the config that produced them. During local work,
`ridu dev` generates them whenever config or a plugin descriptor changes; review and commit the
diff. Use `ridu generate` as a one-shot command when the development loop is not running. Do not
hand-edit an artifact: the next generation would replace it, and `ridu generate --check` or
`ridu check` fails when tracked output does not match executable config.

Generated types do not authorize requests. The server validates and authorizes each request.
Continue with [Generated contracts](./generated-contracts.md) and the
[TypeScript SDK](./typescript-sdk.md).

## 4. One operation engine owns behaviour {#operation-engine}

The generated Go handle, dynamic local API, REST handler, SDK, admin, and calls made through task or
plugin contexts use the same operation engine. It applies access, validation, and hooks regardless
of entry point.

For a mutation, the useful mental model is:

1. Resolve the collection/global and begin or reuse a transaction.
2. Evaluate access and keep any filtered decision attached to the store operation.
3. Normalize input, run field access, validation, and deterministic hooks.
4. Validate final relationships and perform one semantic store mutation.
5. Compute, populate, and redact the returned document.
6. Commit, then dispatch after-commit effects.

Nested local calls made from hooks reuse the outer transaction. A nested failure rolls the whole
operation back, and after-commit work does not run early. For reads, updates, deletes, version
history, and other filterable operations, `ridu.Where(...)` remains a predicate on the atomic store
query; Ridu does not fetch a row and authorize it afterwards.

> [!IMPORTANT]
> Apply access rules and field redaction to local Go API calls too. Admin visibility and field
> `ReadOnly` presentation do not grant or restrict permission.

Read [Data access](./data-access.md), [Access control](./access-control.md), and
[Hooks](./hooks.md) when you need the detailed phase behaviour.

## 5. Choose a database and build {#adapters}

The [PostgreSQL](./postgres.md), [SQLite](./sqlite.md), and [MongoDB](./mongodb.md) adapters
provide transactional persistence. Use PostgreSQL for networked, multi-host deployments and SQLite
for a local file on one host. MongoDB requires a transaction-capable replica set; see its guide for
the narrower supported production profile. An [object-storage adapter](https://riducms.com/docs/storage/) stores upload
bytes. [Plugins](./plugins.md) add compiled Go capabilities and statically registered admin
capabilities.

`ridu build` compiles the configured Go server, generated contracts, static admin, and installed
plugins into one binary. Node and Bun remain build tools.

Next, model a collection in [Fields](./fields.md), or read [Testing](./testing.md).
