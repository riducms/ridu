---
title: 'Schema and identifiers'
description: 'Understand the schema package, read a resolved content model, and distinguish collection names, resource IDs, document IDs, and locales.'
product: core
eyebrow: 'Go packages'
order: 41
relatedSymbolIds:
  - 'go:github.com/riducms/ridu#Resolve'
  - 'go:github.com/riducms/ridu/schema#Manifest'
  - 'go:github.com/riducms/ridu/schema#StableID'
  - 'go:github.com/riducms/ridu/schema#CollectionSlug'
  - 'go:github.com/riducms/ridu/schema#LocaleCode'
  - 'go:github.com/riducms/ridu/schema#ValidationError'
aliases:
  [
    'schema package',
    'schema.Manifest',
    'schema.StableID',
    'schema.CollectionSlug',
    'schema.LocaleCode',
    'schema.ValidationError'
  ]
navigation:
  section: 'Get started'
  parent: go-packages
  order: 40
  title: 'Schema and identifiers'
---

The `schema` package describes a Ridu content model after Ridu has checked your Go configuration.
You will also see its identifier types in callback arguments: `schema.CollectionSlug`,
`schema.StableID`, and `schema.LocaleCode`.

For ordinary content definitions, use `ridu.Collection` and the `field` constructors. Use
`schema` when reading the resolved model, working with typed identifiers, or inspecting
configuration errors. Import it from `"github.com/riducms/ridu/schema"`.

## Config, schema, and documents {#config-and-schema}

| Value             | What it describes                                               | Example                                                |
| ----------------- | --------------------------------------------------------------- | ------------------------------------------------------ |
| `ridu.Config`     | The application you write in Go, including executable callbacks | Posts have a required title and a hook                 |
| `schema.Manifest` | The checked description that Ridu generates from that config    | The title's name, type, required flag, and admin label |
| `store.Document`  | One content record read or saved at runtime                     | Post `abc123` has the title “Hello”                    |

A **manifest** is the complete resolved description of your collections, globals, fields, and
plugins. Ridu uses it to generate types, database migrations, and admin forms. Hooks, access
functions, and secrets remain in the Go application; they are not serialized into the manifest.

`schema.Collection` and `schema.Field` are resolved definitions. They are different types from
the `ridu.Collection` and `field.Text(...)` values you author. You normally obtain them by reading
a manifest, rather than constructing them yourself.

## Inspect a field definition {#inspect-schema}

`ridu.Resolve(config)` checks the config and returns its manifest without opening a database.
`manifest.Snapshot()` returns a copy you can inspect. This complete Go example test defines a
collection and reads its title field:

```go title="content/inspect_test.go" focus={18-19,24-29}
package content

import (
	"fmt"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

func Example_inspectSchema() {
	config := ridu.Config{
		Name: "Blog",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title").Required()},
		}},
	}
	// Resolve checks the Go config without opening a database.
	manifest, err := ridu.Resolve(config)
	if err != nil {
		panic(err)
	}

	// Snapshot gives us a copy of the resolved definitions.
	snapshot := manifest.Snapshot()
	posts := snapshot.Collections[0]
	title := posts.Fields[0]
	fmt.Println(posts.Slug, title.Path.String(), title.ID)
	fmt.Println("Required:", title.Required)

	// Changing the copy does not change the application's schema.
	snapshot.Collections[0].Fields[0].Required = false
	fmt.Println("Still required:",
		manifest.Snapshot().Collections[0].Fields[0].Required)

	// Output:
	// posts title posts-title
	// Required: true
	// Still required: true
}
```

Save this as `content/inspect_test.go` and run `go test ./content`. Go checks that the printed
values match the `Output` comment. In an existing application, use your `Config()` result in
place of the small config in this example.

The manifest remains unchanged when the snapshot is edited. To change the content model, edit
the Go config and regenerate it. Reading a snapshot does not execute hooks or tell you whether
the current user may read a field.

For tooling, `manifest.Bytes()` returns its JSON representation, and `schema.Parse(data)` reads
and validates serialized manifest data. Generated applications already write
`generated/ridu.schema.json`; see [Generated types and files](/docs/generated-contracts/) before
building your own schema export.

## Tell identifiers apart {#identifiers}

Several values look like strings but identify different things:

| Name                                                  | Identifies                                               | Where you use it                                                                |
| ----------------------------------------------------- | -------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Collection slug (`string` or `schema.CollectionSlug`) | An API resource such as `posts`                          | Local API collection arguments, relationship targets, and auth collection names |
| Resource or field ID (`schema.StableID`)              | A resolved collection, global, or field definition       | `ctx.CollectionID`, `ctx.GlobalID`, schema metadata, and migration artifacts    |
| Document ID (`string` or `operation.ID`)              | One saved record                                         | Local API document arguments, relationship values, and `ctx.ID`                 |
| Field name (`string`)                                 | A property inside its object, such as `title`            | Field constructors and direct lookups such as `ctx.Siblings.String("title")`    |
| Field path (`query.Path`)                             | A field reached through its parents, such as `seo.title` | Filters, sorting, selection, and population                                     |
| Locale (`schema.LocaleCode`)                          | A configured content language, such as `en`              | Local API options and localized callbacks                                       |

In the example, the collection's slug and generated resource ID both happen to be `posts`.
They still have different purposes. The title field has the name and path `title`, but its
resolved ID is `posts-title`. Read IDs from Ridu's metadata; do not assemble them from strings or
pass them where an API expects a collection slug.

Stable IDs describe schema definitions, not saved documents. Renaming a collection or field is a
[schema migration](/docs/migrations/); changing a title on one post is a content update. The
generated IDs and migration history account for the schema change.

An array row's `_key` is another identifier: it follows a row when that row moves. A field
callback's `OccurrenceID` identifies the particular field value within its rows and locale.
It is an opaque token, not a field path or a document ID. Read nearby values through
[callback contexts](/docs/go-packages/operation/) instead of parsing that token.

### A type conversion does not validate a string {#typed-strings}

Go allows `schema.CollectionSlug(raw)` and `schema.LocaleCode(raw)` to give a string the named
type expected by an API. Those conversions do not check spelling or whether the resource exists.

Use the type the function expects: ordinary local API methods such as `Create` take a `string`,
while a field context's `Local.FindByID` takes `schema.CollectionSlug`. A literal such as
`"posts"` works in either place; a variable may need the explicit conversion.

`schema.IsValidCollectionSlug(raw)` checks lowercase names such as `blog-posts`.
`schema.IsValidLocaleCode(raw)` checks the supported locale-code format. The application still
checks that the collection or locale is configured when you use it. Use the values passed into
a callback when continuing the same operation.

See [Filters and paths](/docs/go-packages/query/) for the similar distinction between a valid
path string and a field your application can actually query.

## Understand configuration errors {#configuration-errors}

If `ridu.Resolve` rejects a configuration, inspect its returned error. A
`*schema.ValidationError` contains an `Issues` list. Each issue has a `Code`, a `Path` pointing to
the problem in the config, and a `Message` explaining what to fix. Use Go's `errors.As` to check
for this error type when your tooling needs the individual issues; printing the error already
includes their paths and messages.

These are different from the `operation.Issue` values returned by a field validator. A config
error means the application definition needs fixing. A field-validation issue means a submitted
document value needs fixing. Ridu adds the document and field information when it reports a
validation failure to the caller.

Use [Validation](/docs/fields/validation/) to write messages for content authors, and
[Configuration](/docs/configuration/) to define the application. The
[schema API reference](/reference/schema/) lists the exact resolved properties when you need to
inspect more than the field definition shown here.
