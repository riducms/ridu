---
title: 'Generated types and files'
description: 'Generate Go models, a typed TypeScript client, and API schemas from your content config, and keep them up to date.'
product: sdk
eyebrow: 'Tooling'
order: 135
aliases:
  [
    'generate',
    'codegen',
    'manifest',
    'OpenAPI',
    'generated types',
    'schema drift',
    'ridu generate'
  ]
navigation:
  section: 'Work with data'
  parent: 'data-access'
  order: 70
  title: 'Generated types and files'
---

Ridu generates Go models, a typed TypeScript client, and API schemas from your Go content config.
When you add a field or change a collection, regenerate these files so your application code and
admin know about the change.

During development, `ridu dev` updates them on startup and whenever your Go config changes. Run
`ridu generate` for a one-time update without starting the development server. Generation does
not change your database.

Commit the generated files with the config change. Edit your Go config to change them; edits made
directly to a generated file will be replaced the next time generation runs.

## How Ridu reads your config {#execute-config}

Your config is Go code: it can call functions, reuse field definitions, and register plugins.
Ridu runs your project's configured command to load that config and check it for errors.

The result is a JSON description of your collections, globals, fields, and plugins, called the
**schema manifest**. Access rules, hooks, task handlers, and secrets stay in your Go application;
they are not included in the generated JSON.

[Schema and identifiers](/docs/go-packages/schema/) explains how this manifest differs from
your Go config and saved documents, and how to inspect its field definitions in Go.

If the CLI and application use incompatible versions, generation stops with an upgrade message.
Use the CLI version pinned to your project.

## Which files Ridu generates {#outputs}

A new Ridu project uses these paths:

| Path                                  | What it contains                                                       | Commit it? |
| ------------------------------------- | ---------------------------------------------------------------------- | ---------- |
| `generated/ridu.schema.json`          | A JSON description of your content model and admin settings            | Yes        |
| `generated/ridu.openapi.json`         | Your REST endpoints, request and response formats, and errors          | Yes        |
| `generated/ridu.generated.go`         | Go document types and methods for working with collections and globals | Yes        |
| `generated/ridu.generated.ts`         | TypeScript document types and a client for your application            | Yes        |
| `admin/src/ridu.plugins.generated.ts` | Imports for the admin plugins enabled in your config                   | Yes        |
| `.ridu/…`                             | Temporary builds, caches, and intermediate files                       | No         |

`ridu.toml` lets you change these paths. Keep your content definitions, access rules, and
database connection in your Go config. See [Project structure](/guides/project-structure/) for
the directory layout.

Keep `@riducms/sdk` and packages referenced by generated TypeScript field types in the root
`package.json`. Install admin plugin packages in `admin/package.json` too, so the generated
plugin imports can find them.

## What the schema file contains {#manifest}

`generated/ridu.schema.json` describes your content model for the admin, generators, migrations,
and plugins. The same config produces the same file, making changes easy to review in Git.

Ridu records stable IDs for collections, globals, and fields. These IDs let migrations recognize
a renamed field instead of treating it as a deletion followed by a new field.

The file also includes admin settings, such as which custom component edits a field. Replacing a
text input with a custom component still generates a text value in Go and TypeScript. To change
how a value is stored, change the field type.

Fields protected by read access rules are marked `queryRestricted`, so the generated client and
admin can avoid offering filters and sorting for them. The server still checks access on every
request; the access rule itself stays in Go.

## Use the generated Go types {#go}

The Go file gives you separate types for documents, create input, and update input. Bind a
collection to your application's local API, then call its methods with those types:

```go
posts := generated.PostsCollection.With(app.Local())

created, err := posts.Create(ctx, generated.PostCreate{
	Title: core.Set("Hello, Ridu"),
}, actor)
if err != nil {
	return err
}

page, err := posts.List(
	ctx,
	core.TypedListOptions{Page: 1, Limit: 20, Actor: actor},
)
```

This example uses an optional `title` field. For a required text field, pass a plain string
instead of `core.Set(...)`. Generated methods run the same access checks, validation, and hooks
as the rest of the local API.

### Set, clear, or leave a field unchanged {#go-input-values}

On an update, leaving a field out means "keep the saved value." Sending `null` means "clear this
value," which works only for nullable fields. The generated types distinguish these choices:

| Generated field type    | Leave it out | Set a value              | Clear it         |
| ----------------------- | ------------ | ------------------------ | ---------------- |
| `*core.Input[T]`        | `nil`        | `core.Set(value)`        | `core.Null[T]()` |
| `*core.NonNullInput[T]` | `nil`        | `core.SetNonNull(value)` | Not allowed      |
| `core.NonNullInput[T]`  | Not allowed  | `core.NonNull(value)`    | Not allowed      |
| `*T`                    | `nil`        | A pointer to the value   | Not allowed      |
| `T`                     | Not allowed  | The value itself         | Not allowed      |

Use the type generated for your field. `Input` lets you send explicit `null`. `NonNullInput`
rejects values that would encode as JSON `null`, such as a nil slice. Ridu uses it for non-null
lists, maps, JSON, and plugin-defined Go types. Both wrappers have `Get()` to read their value.

Zero values still count as values: `core.Set(false)` sends `false`, and `core.Set(0)` sends `0`.
An empty list is different from a nil list; required and minimum-row validation decide whether
an empty list is accepted.

### Read related documents and translations {#go-read-options}

`Find` takes `core.TypedReadOptions`, and `List` takes `core.TypedListOptions`. Use these options
to choose fields and populate related documents. A generated relationship uses `Reference[T]`,
which contains an `ID` and, when populated, a `Document`.

Localized projects also generate an `AllLocales` reader and document types with language maps.
Use the ordinary collection reader for a single language. Go field names use familiar
initialisms such as `URL` and `UserID`; the JSON keys keep your configured names, such as `url`
and `userID`.

## Use the generated TypeScript client {#typescript}

Import `createClient` from your generated file. It already knows your collection names, document
fields, and which values are required when creating or updating content:

```ts
import { createClient } from '~/generated/ridu.generated';

const client = createClient({ baseURL: 'https://cms.example.com' });
const post = await client.create('posts', {
	title: 'Hello, Ridu',
	status: 'draft'
});
```

The generated client also types filters, selected fields, and populated relationships. For
example, a literal `select` option narrows the result to the fields you requested, and `populate`
adds the related document's type. This works inside groups, arrays, and blocks, including reads
of every translation. Options held in broadly typed variables may produce broader result types.
Sort terms are strings; see [Querying data](/docs/querying/) for examples.

Fields can be omitted from a response when access rules deny them, so check optional output
values before using them. Hiding a field with an admin condition does not make it optional in a
create request. Use [validation](/docs/fields/validation/) when allowed values depend on another
field, and [access rules](/docs/access-control/) to protect them.

The generated file exports `RiduConfig` for integrations that need your application's types.
For a TypeScript project containing one generated config, the raw `@riducms/sdk` client can infer
it too. With no config or multiple configs, import the intended generated `createClient` wrapper
or pass that application's `RiduConfig` explicitly.

## Use OpenAPI and GraphQL schemas {#openapi}

Use `generated/ridu.openapi.json` to inspect your API, generate a client in another language, or
configure API tooling. It comes from the same content config as your Go and TypeScript types.

If you use the GraphQL plugin, configure `generated.graphql.schema` to write the GraphQL schema
for your client and editor tools. Like the other generated files, it is replaced when you run
generation.

## Keep generated files up to date {#atomic}

`ridu generate` updates all generated files together. If it fails, your previous generated files
remain in place.

Use `--check` in CI to check for missing or outdated files without changing them:

```bash
ridu generate --check
```

During development, keep the development server running to regenerate files when your config
changes:

```bash
npm run dev
```

This also applies supported, non-destructive schema changes to your development database. Review
the generated file changes alongside your config:

```bash
git diff -- generated admin/src/ridu.plugins.generated.ts
```

If the change affects stored data, create a migration and run the project checks before deployment:

```bash
ridu migrate create --name describe-the-change
ridu check
```

Generation updates your application types and API description. A migration records how to change
the database from the previous schema to the new one. Read [Migrations](/docs/migrations/) before
deploying a schema change; you do not need to create a migration for every local edit.

## Fix generation problems {#failures}

| Symptom                             | Likely cause                                                                                                         | Next step                                                       |
| ----------------------------------- | -------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| Project command cannot compile      | Invalid Go config or incompatible package versions                                                                   | Run `go test ./...`; fix the first compiler error               |
| Handshake version mismatch          | The CLI and application use incompatible versions                                                                    | Use the CLI release pinned to the project                       |
| Manifest validation fails           | Duplicate slug/name, invalid option combination, unknown relationship target, localization cycle, or plugin conflict | Fix the field or setting named in the error                     |
| `--check` reports drift             | The generated files no longer match your config or framework version                                                 | Run generation locally and review the changed files             |
| Raw SDK types fall back to defaults | No generated config, or multiple generated configs, are in the TypeScript program                                    | Import/use the generated wrapper explicitly                     |
| Generated plugin import fails       | A generated import does not match your installed plugin packages                                                     | Use `ridu plugin` commands; do not patch the generated registry |

See [Troubleshooting](/docs/troubleshooting/) for more help, the
[schema reference](/reference/schema/) for schema types, and the [CLI reference](/reference/cli/)
for generation commands.

## Primitive list contracts {#primitive-lists}

[TextList and NumberList](/docs/fields/lists/) generate Go slices and TypeScript `string[]` or
`number[]`, with non-null elements. Create inputs require a list when `Required` or positive
`MinRows` requires it, unless a default supplies it. Updates may omit the field; a supplied
list replaces all items. Read fields remain optional because access can hide them.

Go nullable inputs use `core.Set([]string{})` for an explicit empty list and
`core.Null[[]string]()` for null. Required list inputs use `core.NonNull(...)` or
`core.SetNonNull(...)` as the generated declaration indicates. Generated Go decoders reject
null primitive items instead of converting them to zero values. Read slices use nil for absent
or null values, following the existing read model. A nonnil empty slice retains `[]` when
re-encoded; its `omitzero` tag omits nil slices without dropping empty lists. OpenAPI separates item
constraints from list counts and describes write constraints without imposing them on
read-hook results. The optional GraphQL plugin uses `[String!]` and `[Float!]`.
