<!-- Generated from website/src/content/docs/generated-contracts.md by scripts/sync-agent-docs.ts. -->

# Generated contracts

`ridu generate` resolves the application config into a manifest and generated contracts. It does
not touch the database, and it installs the output set atomically.

Commit generated contracts. They are reviewable application interfaces, not disposable caches.

## Why Ridu executes config {#execute-config}

A Go configuration can call functions, compose packages, and register compiled plugins. Parsing Go
source would see syntax, not the values the program actually produces, so the CLI runs the
project's configured command to resolve it.

The project command resolves `ridu.Config`, applies plugin transforms, validates the graph, and
returns a deterministic snapshot. Generation does not connect to the database, initialize runtime
storage, start the HTTP server, or make application network requests.

Protocol or framework version mismatches fail with an upgrade diagnostic instead of guessing at a
schema. Executable access callbacks, hooks, task handlers, storage credentials, and secrets remain
runtime-only; they are never serialized into the manifest.

## Files in one generation {#outputs}

The default generated project writes:

| Path                                  | What it contains                                                                                             | Commit it? |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------ | ---------- |
| `generated/ridu.schema.json`          | Canonical, versioned manifest with stable IDs, resources, fields, capabilities, plugin metadata, and digests | Yes        |
| `generated/ridu.openapi.json`         | REST paths, request/response schemas, auth, locale parameters, and stable error envelopes                    | Yes        |
| `generated/ridu.generated.go`         | Output/create/update structs and typed local collection/global handles                                       | Yes        |
| `generated/ridu.generated.ts`         | Exact document/input/query/select/populate types and the generated client factory                            | Yes        |
| `admin/src/ridu.plugins.generated.ts` | Validated static imports for official or packaged admin plugins                                              | Yes        |
| `.ridu/…`                             | Project-command builds, staging files, caches, and intermediate admin output                                 | No         |

`ridu.toml` can change the structural paths, but it cannot contain database credentials, access
rules, or schema definitions. See [Project structure](https://riducms.com/guides/project-structure/) for the directory
map.

Keep `@riducms/sdk` and packages referenced by generated TypeScript field types in the root
`package.json`. Keep an admin plugin in `admin/package.json` as well when the generated admin
registry imports its runtime module.

## Canonical manifest {#manifest}

The manifest is the contract shared by migrations, runtime plugins, generators, the admin, and
tooling. Its serialization is deterministic: equivalent resolved configuration produces identical
bytes and digests regardless of map iteration order.

Stable resource and field identities let a migration distinguish a rename from an unrelated drop
and create. Human-facing slugs and labels can change while committed identity records continuity.
Plugin contributions are namespaced and versioned.

A built-in field may carry `admin.component` with a paired plugin key, exact component key, and
deterministic object configuration. This is public presentation metadata, not a new value type:
storage, validation, migrations, OpenAPI, and generated Go/TypeScript values continue to follow the
built-in field kind. Executable callbacks and credentials remain outside the manifest.

The manifest describes that an access rule exists and which operation it protects, but never its
function body or decision. The admin can use this metadata to hide an unavailable action only as a
presentation; every operation re-evaluates authorization on the server.

## Generated Go API {#go}

The Go file contains distinct types for stored output, create input, and partial update input. A
generated collection handle binds those types to the same dynamic local operation engine:

```go
posts := generated.PostsCollection.With(app.Local())

created, err := posts.Create(ctx, generated.PostCreate{
	Title:  "Hello, Ridu",
	Status: "draft",
}, actor)
if err != nil {
	return err
}

page, err := posts.List(ctx, core.TypedListOptions{Page: 1, Limit: 20, Actor: actor})
```

Generated handles do not bypass access, hooks, validation, transactions, localization, or
redaction. Dynamic filters keep using the shared [`query` package](https://riducms.com/reference/query/); generated
document and mutation shapes remove field-name maps from ordinary application code.

Nullable create/update fields use `*core.Input[T]`: leave the pointer nil to omit the field, call
`core.Set(value)` to send a concrete value (including `false`, `0`, or an empty slice), and call
`core.Null[T]()` to send explicit JSON `null`. A non-null slice, map, or fallback
`json.RawMessage` cannot safely use its raw Go type because a nil value would encode as null. The
same conservative rule applies to plugin-owned named Go types, whose underlying type and custom
marshaling cannot be proven from the manifest. Required fields of those shapes therefore use
`core.NonNullInput[T]` and `core.NonNull(value)`; their omittable default/update forms use
`*core.NonNullInput[T]` and `core.SetNonNull(value)`. JSON encoding rejects a nil or otherwise
null-encoding wrapped value. Other non-null fields that are omittable in an update or because they
have a server default use `*T`.

Both wrappers are write-only typed local-API arguments: `core.Input` and `core.NonNullInput` are not
general-purpose JSON-unmarshal contracts. The non-null
wrapper prevents JSON null; ordinary required/minimum-length validation still decides whether an
empty concrete collection is valid. Typed list handles accept `core.TypedListOptions`; population
and all-locale reads remain on the dynamic local API because they change field value shapes at
runtime.

## Generated TypeScript API {#typescript}

The generated module extends the neutral `@riducms/sdk` runtime with application-specific types:

- output, create, and update shapes for every collection and global;
- collection/global/auth/upload/version-capability slug unions;
- field-aware `where`, `select`, and `populate` inputs;
- one `RiduConfig` map connecting slugs to all those types;
- a `createClient` wrapper that supplies the exact config without repeated generics;
- type-only module augmentation for raw SDK inference in programs with one generated config.

Conditional admin presentation does not narrow generated TypeScript inputs. Create and update
types remain ordinary flat field contracts, including each field's required, default, and null
behavior. Hidden fields can still be submitted, and conditions do not perform runtime validation or
authorization; enforce active-shape rules in application validation when needed.

```ts
import { createClient } from '~/generated/ridu.generated';

const client = createClient({ baseURL: 'https://cms.example.com' });
const post = await client.create('posts', {
	title: 'Generated contracts',
	status: 'draft'
});
```

A TypeScript program that contains zero generated configs—or more than one—does not select an
automatic default. Import the intended generated wrapper, or pass its `RiduConfig` explicitly to
the raw SDK. This avoids whichever package happened to load first silently choosing your API.

Selection and population inputs are typed, but they do not yet transform the method's output type.
Sort terms also remain strings. Treat the runtime projection as authoritative and see
[Querying data](./querying.md) for that current boundary.

## OpenAPI and other consumers {#openapi}

The OpenAPI document is generated from the same snapshot as the TypeScript module. Use it for API
inspection, non-TypeScript client generation, request fixtures, or gateway tooling. Do not
hand-edit it: the next generation replaces it. When `generated.graphql.schema` is
configured, the compiled GraphQL plugin also emits its exact runtime SDL for GraphQL clients,
editor tooling, and API review.

The admin loads the runtime schema and generated SDK contract, while its plugin registry imports
static code selected at build time. GraphQL derives its SDL from the same manifest and executable
plugin options.

## Atomic writes and drift checks {#atomic}

`ridu generate` writes the outputs as one set. If generation or installation fails, the previous
contract set remains intact. Byte-identical output keeps its timestamp.

Use the non-writing mode in CI:

```bash
npm run ridu -- generate --check
```

It fails when an expected file is missing or byte-different. During ordinary local development,
keep the complete loop running:

```bash
npm run dev
```

It regenerates contracts on config changes and synchronizes safe additive development schema
changes. After the behavior works, review the generated files:

```bash
git diff -- generated admin/src/ridu.plugins.generated.ts
```

If the change affects stored schema, create the immutable artifact and run the project checks before
deployment:

```bash
npm run ridu -- migrate create --name describe-the-change
npm run ridu -- check
```

Generation describes the target API; migration creation records the reviewed, immutable database
transition from the previous manifest. A migration is not required for ordinary local development.
Read [Migrations](./migrations.md) before
changing an existing schema.

## Common failures {#failures}

| Symptom                             | Likely cause                                                                                                         | Next step                                                       |
| ----------------------------------- | -------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| Project command cannot compile      | Invalid Go config or incompatible package versions                                                                   | Run `go test ./...`; fix the first compiler error               |
| Handshake version mismatch          | CLI and application framework do not share the project-command contract                                              | Use the CLI release pinned to the project                       |
| Manifest validation fails           | Duplicate slug/name, invalid option combination, unknown relationship target, localization cycle, or plugin conflict | Follow the structured path in the error                         |
| `--check` reports drift             | Config or generator version changed without committed outputs                                                        | Run generation locally and review all artifacts together        |
| Raw SDK types fall back to defaults | No generated config, or multiple generated configs, are in the TypeScript program                                    | Import/use the generated wrapper explicitly                     |
| Generated plugin import fails       | Registry metadata and installed Go/admin packages disagree                                                           | Use `ridu plugin` commands; do not patch the generated registry |

The [Troubleshooting](./troubleshooting.md) guide covers recovery in more detail. Exact generator,
manifest, and project-handshake types are in the [Go API reference](https://riducms.com/reference/schema/) and
[CLI reference](https://riducms.com/reference/cli/).
