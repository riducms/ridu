---
title: 'Go for TypeScript developers'
description: 'Read and extend a Ridu application without having to become a Go expert first.'
product: core
eyebrow: 'Start here'
order: 35
navigation:
  section: 'Get started'
  order: 50
  title: 'Go for TypeScript developers'
---

Use Go for Ridu configuration and trusted server behaviour such as access rules and hooks.
Application frontends and admin components remain TypeScript, and Ridu generates TypeScript types
for your content model.

Once the syntax is familiar, [Go packages](/docs/go-packages/) explains the Ridu-specific types:
`operation.Value[T]`, `store.Values`, `query.Path`, callback contexts, and schema identifiers.

## Read a Ridu configuration {#mental-model}

A Ridu project starts with a Go function that returns `ridu.Config`. You describe the collections,
fields, and server behavior there. Ridu checks that configuration and uses it to build the
database changes, API, Go and TypeScript types, and admin forms.

Map common TypeScript constructs to their Go equivalents:

| TypeScript idea                        | Go in a Ridu project                                 |
| -------------------------------------- | ---------------------------------------------------- |
| `export const posts: Collection`       | `var Posts = ridu.Collection{...}`                   |
| `{ slug: 'posts', fields: [...] }`     | `ridu.Collection{Slug: "posts", Fields: ...}`        |
| `Field[]`                              | `field.Fields`                                       |
| `required: true`                       | `.Required()`                                        |
| `async (args) => result`               | `func(ctx context.Context, args T) (Result, error)`  |
| `undefined` or `null`                  | a type's zero value or, where absence matters, `nil` |
| `throw new Error(...)`                 | return an `error`                                    |
| `AbortSignal` / request-scoped context | `context.Context`                                    |

Here is a complete collection definition:

```go title="content/posts.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Posts = ridu.Collection{
	Slug: "posts",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Text("slug").Required().Unique(),
		field.Select("status", "draft", "published").Default("draft"),
	},
}
```

The punctuation differs, but the shape is still a typed configuration object. `field.Text` and
`field.Select` create fields. Methods such as `.Required()` return an updated copy of the field.
Go catches invalid combinations, such as a number option on a text field. Ridu checks relationships
between fields and collections when it loads the configuration.

## Enough Go to be productive {#enough-go}

### Packages and exports {#packages-and-exports}

Every `.go` file begins with a package name. Files in the same directory normally share a package,
so `content/posts.go` and `content/config.go` can use each other's variables and functions without
importing one another. Imports name the external packages used by that file.

Go uses capitalization instead of an `export` keyword. `Posts`, `Config`, and `Collection` are
visible to other packages; `posts` is private to its package.

### Structs, slices, and zero values {#structs-slices-zero-values}

A Go struct is closest to a TypeScript object with a fixed interface. A struct literal names the
fields you want to set:

```go
ridu.Config{
	Name:        "Acme Editorial",
	Collections: []ridu.Collection{Users, Posts},
}
```

`[]ridu.Collection` is a slice, Go's growable-list type. Go gives every field a zero value:
empty strings, `false`, `0`, and `nil` for pointers and slices. Ridu's public structs document when a
zero value means “use the default.” Name the fields as above so the configuration is easy to read.

### Functions and errors {#functions-and-errors}

Go commonly returns a result and an error instead of throwing:

```go
manifest, err := ridu.Resolve(Config())
if err != nil {
	return err
}
```

`:=` declares local variables and infers their types. `if err != nil` handles the error. Ridu uses
the same pattern for local operations, access callbacks, hooks, and startup.

Many callbacks also receive `context.Context`. Treat it like request-scoped cancellation and
deadline state, not a bag of global application values. Pass it to database or network work started
for that operation so cancellation can propagate.

### Pointers and `nil` {#pointers-and-nil}

You will sometimes see `*T`, meaning “a pointer to `T`.” Ridu uses pointers when absence has meaning.
For example, a nil actor in an access request means an anonymous request; it does **not** mean an
administrator.

### Formatting and tests {#formatting-and-tests}

Use `gofmt` to format Go code. Generated projects include formatting, vet, tests, generated-drift,
and frontend checks behind `ridu check`. Go test files end in `_test.go`, and table-driven tests are
the common equivalent of a parameterized test suite.

## What stays TypeScript {#what-stays-typescript}

Ridu generates a TypeScript module containing document, create, update, select, query, and
population types. Its client uses the Fetch-based `@riducms/sdk`; SDK methods return
`Promise<T>` values and reject structured `RiduError` failures.

Write custom admin inputs, pages, and dashboard panels as Svelte components and import them in
`admin.config.ts`. Hooks and access rules are ordinary Go application code. Build a Go plugin
when adding a new field type or packaging a reusable server extension. Bun builds the admin,
and the Go binary serves it in production.

Read [TypeScript SDK](/docs/typescript-sdk/) for frontend calls, [Admin](/docs/admin/) for the admin,
and [Custom components](/docs/custom-components/) to customize its UI.

## Choose where your code belongs {#where-behaviour-lives}

- Put collection and field definitions in small Go factory functions.
- Put authorization in [access rules](/docs/access-control/), not in admin visibility settings.
- Put lifecycle behaviour in [hooks](/docs/hooks/) and keep side effects safe for retries.
- Use the [local API](/docs/local-api/) for trusted Go callers; it still passes through the same
  authorization, validation, hooks, transaction, and redaction engine as REST.
- Use the generated TypeScript module or [REST API](/docs/rest-api/) from application frontends.
- Treat generated schema, Go, OpenAPI, and TypeScript files as committed output. Change config and
  let `ridu dev` regenerate them rather than editing generated files.

## A useful learning path {#learning-path}

You can learn the Go syntax as the product concepts appear:

1. Follow [Getting started](/docs/getting-started/) and read the generated project.
2. Add one field with [Collections](/docs/collections/) and [Fields](/docs/fields/).
3. Run generation and use the new type from [TypeScript SDK](/docs/typescript-sdk/).
4. Add one access rule and one hook only when the application needs them.
5. Keep [Troubleshooting](/docs/troubleshooting/) nearby for compiler, generation, and runtime
   symptoms.

Go reports incorrect field methods, callback arguments, and return types before the application
starts. Ridu reports configuration errors such as duplicate slugs or a relationship to a missing
collection.
