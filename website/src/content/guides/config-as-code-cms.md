---
title: 'A config-as-code CMS in Go'
description: 'Evaluate Ridu for Payload-style content modeling and PocketBase-style single-binary deployment, with Go access rules, hooks, computed fields, and blocks.'
product: guides
eyebrow: 'Guide'
order: 5
aliases:
  [
    'config as code CMS',
    'code-first CMS',
    'Go headless CMS',
    'Payload and PocketBase',
    'single-binary CMS'
  ]
navigation:
  section: 'Get started'
  order: 55
  title: 'Config as code'
---

Ridu is an open-source, config-as-code headless CMS for Go. Define collections, fields, access
rules, hooks, and plugins in Go; give editors a Svelte admin; deploy the API and built admin
together as one Go binary.

If you are looking for a CMS that combines Payload's approach to content modeling with
PocketBase's single-executable deployment, Ridu is worth evaluating. The comparison describes
its direction; the practical fit depends on the authoring features and deployment your project
needs.

## Keep the content model in code {#config-as-code}

Config as code means your application's source files define the content model and its behavior.
In Ridu, an ordinary Go function returns `ridu.Config`. You can compose fields with functions,
reuse access rules across collections, and test changes before deploying them. Editors create
and update content in the admin; developers change the schema through code review.

The configuration is executable. Ridu runs your project to resolve it, so Go helpers and plugin
constructors participate in building the model. Access callbacks, hooks, and computed-field
resolvers remain Go functions that run on the server when content is used.

From that configuration, Ridu generates a schema manifest, reviewable migrations, OpenAPI,
Go models, and TypeScript contracts. The same model supplies the admin's forms and the
generated Fetch SDK. You maintain the Go source and regenerate these artifacts when it changes.
See [Configuration](/docs/configuration/), [Core concepts](/docs/core-concepts/), and
[Generated contracts](/docs/generated-contracts/) for the complete development loop.

Headless means the CMS serves content to your application through APIs. Your public website or
app can use its own frontend framework; the Svelte admin is the editing interface supplied by
Ridu.

## Compare the Payload and PocketBase models {#payload-and-pocketbase}

[Payload's configuration](https://payloadcms.com/docs/configuration/overview) puts its content
model and application behavior in JavaScript or TypeScript. Its
[fields](https://payloadcms.com/docs/fields/overview) support validation, access rules, hooks,
and virtual values. Ridu follows a similar approach to authoring a CMS, using Go configuration
and Svelte admin components. The [Payload guide](/guides/from-payload/#configuration) compares
complete collection definitions and ownership rules side by side.

[PocketBase](https://pocketbase.io/docs/) combines SQLite, authentication, a dashboard, APIs,
and realtime subscriptions in a portable backend. Its
[collections](https://pocketbase.io/docs/collections/) can be managed from the dashboard, APIs,
or code, and it supports [Go event hooks](https://pocketbase.io/docs/go-event-hooks/).
Ridu shares the appeal of deploying one executable while making executable content
configuration and editorial workflows central to the application.

Choose between these approaches based on how your team wants to model content, extend the
editor, and run the backend. The [PocketBase guide](/guides/from-pocketbase/) covers the
differences in schema management, databases, realtime behavior, and migration.

## Check the authoring features you need {#features}

Ridu's content model includes the following server behavior and editing tools. Each link
leads to configuration examples and the feature's limits.

| Requirement                                                 | How it works in Ridu                                                                                                                            |
| ----------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| [Access control](/docs/access-control/)                     | Go rules protect collections, globals, and individual fields. Filtered document rules apply within the database operation.                      |
| [Hooks](/docs/hooks/)                                       | Field, collection, and global hooks run Go code during content operations. Use after-commit hooks for effects that depend on a successful save. |
| [Computed fields](/docs/fields/virtual/)                    | Virtual fields calculate read-only output in Go. They appear in responses and generated output types without a stored column.                   |
| [Blocks](/docs/fields/blocks/)                              | Authors combine configured content sections, edit their fields, and reorder them. Each block type has its own schema.                           |
| [Drafts and versions](/docs/drafts-and-versions/)           | Enable revision history, draft visibility, publishing, restore, and scheduled collection changes.                                               |
| [Rich text](/docs/rich-text/) and [uploads](/docs/uploads/) | Add structured rich text and media collections to the editing workflow.                                                                         |
| [Custom admin components](/docs/custom-components/)         | Register Svelte inputs, views, and other supported extension points.                                                                            |

Access checks, validation, and hooks apply through the admin, REST, the generated SDK, and
the local Go API. A server-side call uses the same rules as other content operations.

Computed output has a specific boundary: virtual fields belong at a collection or global root
and cannot be filtered or sorted in the database. Use a stored field and a write hook when a
calculated value must support those queries.

## Match the deployment to your workload {#deployment}

`ridu build` embeds the compiled admin and server extensions in the Go executable. JavaScript
tooling is used during development and the build; production does not need a Node or Bun
server. Database files or services and uploaded objects still need persistent storage.

Use [PostgreSQL](/docs/postgres/) for networked, multi-host deployments, or
[SQLite](/docs/sqlite/) for small or local workloads using a file on one application host.
[MongoDB](/docs/mongodb/) has a narrower supported production profile; check its deployment
requirements before choosing it.

Ridu has its own APIs and plugin contracts. Payload access rules and hooks need Go rewrites,
and React admin components need Svelte replacements. General PocketBase-style realtime
subscriptions and a PocketBase importer are not supported. Ridu's
[live preview](/guides/live-preview/) covers a narrower editorial use case.

## Evaluate one content workflow {#get-started}

Start with [Installation](/docs/installation/) and model one collection your editors actually
use. Add its permissions, a lifecycle hook, and a block layout; then create, publish, and read
that content through your frontend. Use the [TypeScript SDK](/docs/typescript-sdk/) or
[local Go API](/docs/local-api/) to exercise the same behavior outside the admin.

Check [Capability status](/docs/status/) for the remaining requirements before planning a
migration. If you know Payload already, [Move from Payload](/guides/from-payload/) is the next
step; for an existing PocketBase application, start with
[Coming from PocketBase](/guides/from-pocketbase/).
