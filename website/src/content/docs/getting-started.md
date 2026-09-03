---
title: 'Introduction'
description: 'Learn what Ridu provides, how an application is configured, and where to start.'
product: core
eyebrow: 'Start here'
order: 10
aliases:
  ['overview', 'what is ridu', 'why ridu', 'cms', 'Payload alternative', 'PocketBase alternative']
navigation:
  section: 'Get started'
  order: 20
  title: 'Introduction'
---

## What is Ridu? {#what-is-ridu}

Ridu is a Go content-management framework with a built-in Svelte 5 admin. You define
collections, fields, access rules, hooks, and plugins in executable Go. Ridu resolves that config
into a schema manifest and derives the database plan, REST contract, TypeScript
types, Fetch client, and authoring interface from it.

Local Go calls, REST, the SDK, admin, tasks, and plugin transports use the same authorization,
validation, hooks, transactions, population, and redaction. The built admin is embedded in the Go
server, so Node.js and package managers are build-time tools.

Choose PostgreSQL for networked, multi-host deployments or SQLite for a local file on one host.
MongoDB requires a transaction-capable replica set and has a narrower
[supported production profile](/docs/mongodb/). Uploads can use local storage or S3-compatible
object storage.

Check [Capability status](/docs/status/) for complete, limited, experimental, and planned behavior.

## The authoring model {#authoring-model}

A collection is a typed Go config value:

```go title="content/posts.go"
var Posts = ridu.Collection{
	Slug: "posts",
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Select("status",
			field.OneOf("draft", "published"),
			field.Default("draft"),
		),
	},
}
```

Go checks names and option types at compile time; Ridu then validates cross-field and
cross-collection rules while resolving the manifest. [Ridu for TypeScript developers](/docs/go-for-typescript/)
explains packages, struct literals, slices, functions, `nil`, errors, and contexts through familiar
TypeScript ideas.

Frontend developers consume the generated application module and can remain in TypeScript:

```ts title="src/posts.ts"
import { createClient } from '~/generated/ridu.generated';

const client = createClient({ baseURL: 'https://cms.example.com' });

const page = await client.list('posts', {
	where: { status: { equals: 'published' } },
	sort: ['-publishedAt'],
	limit: 12
});

for (const post of page.docs) {
	console.log(post.title);
}
```

The generated module provides collection and field types through the Fetch SDK. Its methods return
`Promise<T>` and throw structured `RiduError` failures.

## Choose your path {#choose-path}

### I build TypeScript frontends {#typescript-path}

Read [TypeScript SDK](/docs/typescript-sdk/) and [Querying data](/docs/querying/) first. You need Go
only when you author the server schema or a compiled backend extension; a frontend consuming an
existing Ridu application stays in TypeScript.

### I model content and backend behavior {#builder-path}

Follow the [installation guide](/docs/installation/), then read [Core concepts](/docs/core-concepts/),
[Configuration](/docs/configuration/), [Fields](/docs/fields/), [Access control](/docs/access-control/),
and [Hooks](/docs/hooks/). The [Project structure](/guides/project-structure/) guide separates files
you own from committed generated contracts and disposable build state.

### I am evaluating another CMS {#evaluator-path}

Use [Move from Payload](/guides/from-payload/) for a concept and migration-contract map, or
[Ridu for PocketBase users](/guides/from-pocketbase/) for the code-defined and embedded-store trade-offs.
Read [Performance and footprint](/docs/performance/) for benchmark results and test conditions.

## Current limits {#not-a-compatibility-layer}

See [Capability status](/docs/status/) for available, limited, experimental, and planned features
before choosing them for a project.

## Create an application {#create-application}

Open the project wizard with your preferred launcher:

```bash title="terminal" package-manager="npm"
npm create ridu@latest my-app
```

```bash title="terminal" package-manager="bun"
bun create ridu@latest my-app
```

```bash title="terminal" package-manager="pnpm"
pnpm create ridu@latest my-app
```

```bash title="terminal" package-manager="yarn"
yarn create ridu my-app
```

Choose **Starter** or **Blank**, a database, and optional agent guidance. Review the summary, then
run the install and `dev` commands the initializer prints. Omit `my-app` if you prefer to name the
directory inside the wizard.

Direct Go installation, organization-specific Go module/npm scope flags, non-interactive
automation, and recovery are covered in [Installation](/docs/installation/).

The CLI prints the local admin URL. On the first visit, create the initial admin account.
[Installation](/docs/installation/) explains requirements, generated files, and database choices.
