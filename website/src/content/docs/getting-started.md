---
title: 'Introduction'
description: 'Ridu is a config-as-code backend framework for Go with a headless CMS built in. Define data and business logic in code; deploy the API and admin as one binary.'
product: core
eyebrow: 'Start here'
order: 10
aliases:
  [
    'overview',
    'what is ridu',
    'why ridu',
    'cms',
    'Payload alternative',
    'PocketBase alternative'
  ]
navigation:
  section: 'Get started'
  order: 20
  title: 'Introduction'
---

## What is Ridu? {#what-is-ridu}

Ridu is an open-source, config-as-code backend framework for Go with a headless CMS and Svelte 5
admin built in. It includes authentication, access control, APIs, and background jobs. You define
collections, fields, permissions, hooks, and plugins in Go. Ridu uses that configuration to build
the database structure, API, TypeScript client, and admin forms.

The same permissions and validation apply whether a change comes from Go code, the API, or the
admin. The admin is included in the Go server binary. You use JavaScript tools to build it;
you do not need a JavaScript server in production.

Choose PostgreSQL for networked, multi-host deployments or SQLite for a local file on one host.
MongoDB requires a transaction-capable replica set and has a narrower
[supported production profile](/docs/mongodb/). Uploads can use local storage or S3-compatible
object storage.

Check [Capability status](/docs/status/) for complete, limited, experimental, and planned behavior.

| You want to                        | Start with                                    | Main API                                                                 |
| ---------------------------------- | --------------------------------------------- | ------------------------------------------------------------------------ |
| Define content and server behavior | [Configuration](/docs/configuration/)         | `ridu.Config`, `ridu.Collection`, and `field.*` constructors             |
| Read or change content from Go     | [Local API](/docs/local-api/)                 | `app.Local()` or generated typed handles                                 |
| Build a TypeScript frontend        | [TypeScript SDK](/docs/typescript-sdk/)       | Generated `createClient`, `list`, `find`, `create`, and `update` methods |
| Change the authoring interface     | [Custom components](/docs/custom-components/) | `defineAdmin`, `defineFieldEditor`, and focused extension registrations  |
| Prepare a deployment               | [Production](/docs/production/)               | `ridu build`, migrations, readiness, and graceful shutdown settings      |

## Define your content in Go {#authoring-model}

A collection describes documents of one kind, such as posts:

```go title="content/posts.go"
var Posts = ridu.Collection{
	Slug: "posts",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Select("status", "draft", "published").Default("draft"),
	},
}
```

Go checks that you use the right field methods and callback types. Ridu checks the full
configuration for problems such as a relationship pointing to a collection that does not exist. [Ridu for TypeScript developers](/docs/go-for-typescript/)
explains packages, struct literals, slices, functions, `nil`, errors, and contexts through familiar
TypeScript ideas.

Frontend developers consume the generated application module and can remain in TypeScript:

```ts title="src/posts.ts"
import { createClient } from '~/generated/ridu.generated';

const client = createClient({ baseURL: 'https://cms.example.com' });

const page = await client.list('posts', {
	where: { status: { equals: 'published' } },
	sort: ['-createdAt'],
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
only when you change the content model or server behavior; a frontend consuming an
existing Ridu application stays in TypeScript.

### I build the CMS {#builder-path}

Follow the [installation guide](/docs/installation/), then read [Core concepts](/docs/core-concepts/),
[Configuration](/docs/configuration/), [Fields](/docs/fields/), [Access control](/docs/access-control/),
and [Hooks](/docs/hooks/). The [Project structure](/guides/project-structure/) guide separates files
you edit from files Ridu generates for you.

### I am evaluating another CMS {#evaluator-path}

Start with [Ridu, Payload, and PocketBase](/guides/config-as-code-cms/) to compare config-as-code
authoring and single-binary deployment. Use [Move from Payload](/guides/from-payload/) to plan a migration, or
[Ridu for PocketBase users](/guides/from-pocketbase/) for the code-defined and embedded-store trade-offs.
Read [Performance](/docs/performance/) for optimization guidance and
[Measure performance](/docs/performance/measurement/) for benchmark results and test conditions.

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
directory inside the wizard, or use `.` to create the project in your current directory. Ridu warns
before creating files there and stops if any project paths already exist.

Direct Go installation, organization-specific Go module/npm scope flags, non-interactive
automation, and recovery are covered in [Installation](/docs/installation/).

The CLI prints the local admin URL. On the first visit, create the initial admin account.
[Installation](/docs/installation/) explains requirements, generated files, and database choices.
