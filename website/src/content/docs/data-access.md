---
title: 'Data access overview'
description: 'Choose between the local Go API, REST, TypeScript SDK, GraphQL, and admin for a task.'
product: data
eyebrow: 'Data and APIs'
order: 85
aliases:
  ['crud', 'read data', 'write data', 'API overview', 'operations']
navigation:
  section: 'Work with data'
  order: 10
  title: 'Data access overview'
---

Ridu offers several ways into one operation engine. Choosing a transport changes how you express a
request, not the access rules, validation, hooks, transactions, relationship population, or field
redaction applied to it.

## Choose a surface {#choose-surface}

| Surface                                 | Use it when                                                    | Contract                                                           |
| --------------------------------------- | -------------------------------------------------------------- | ------------------------------------------------------------------ |
| [Local Go API](/docs/local-api/)        | Backend code already runs inside the Ridu process              | Dynamic `store.Values` or generated Go structs; no HTTP round trip |
| [REST API](/docs/rest-api/)             | Any HTTP-capable client needs the canonical wire API           | Generated OpenAPI plus stable success/error envelopes              |
| [TypeScript SDK](/docs/typescript-sdk/) | A browser, SvelteKit app, Bun/Node service, or test uses Fetch | Exact generated collection/input/query types over `Promise<T>`     |
| [GraphQL plugin](/docs/graphql/)        | A project needs GraphQL selection sets or tooling              | Optional generated SDL over the same engine                        |
| [Admin](/docs/admin/)                   | Content authors need the built-in interface                    | Schema-driven forms and lists using the generated SDK              |

Prefer the local API for Go code inside the application and the generated SDK for TypeScript. REST
is the durable interoperability boundary. GraphQL is opt-in, and excluding the plugin keeps its
implementation out of the compiled binary.

## The shared operation pipeline {#operation-pipeline}

Every collection read or mutation follows the same ordering:

1. identify the collection, actor, locale, requested draft/trash mode, and operation;
2. evaluate collection access and keep any filtered decision as an atomic store predicate;
3. run before-operation and before-validation hooks;
4. validate field values, relationships, access, uniqueness, and optimistic revision input;
5. execute the store work in one transaction, including nested local calls;
6. run after-change/read hooks, population, computed or join output, and field redaction;
7. commit, then dispatch registered after-commit effects.

Local calls are not privileged. Passing a `nil` actor means anonymous, not superuser. Admin
visibility is presentation metadata and never grants an operation.

## Collections and globals {#resources}

Collections hold repeatable documents and expose create, list, count, find, update, duplicate,
delete, and capability operations. Enabled capabilities add uploads, trash, versions, drafts,
publishing, scheduling, locks, and bulk mutations.

Globals are singleton documents. They expose read and update rather than collection-style create or
delete, and can opt into versions, drafts, publishing, scheduling, localization, live preview, and
field-level access. See [Collections and globals](/docs/collections/) for modeling differences.

## One request in Go and TypeScript {#same-request}

Inside the Go process:

```go title="Go local API"
statusPath, err := query.NewPath("status")
if err != nil {
	return err
}

page, err := app.Local().List(ctx, "posts", ridu.ListOptions{
	Page:  1,
	Limit: 20,
	Where: query.Equal(statusPath, query.String("published")),
	Actor: currentUser,
})
if err != nil {
	return err
}
```

From a generated TypeScript client:

```ts title="TypeScript SDK"
const page = await client.list('posts', {
	page: 1,
	limit: 20,
	where: { status: { equals: 'published' } }
});
```

The representations differ, but access predicates and caller filters remain separate until the
store combines them into one atomic query. Neither client can fetch a denied row and filter it
afterward.

## Documents, pages, and errors {#responses}

Documents carry an ID and timestamps in addition to authored fields. Enabled
features add values such as `_revision`, `_status`, upload metadata, or localization source
metadata. Generated contracts describe these fields.

List results use a page envelope with `docs` and nested `pagination` information including current
page, limit, total documents, total pages, and next/previous state. REST failures use one stable
error envelope; the SDK converts it to `RiduError` with `code`, `status`, `requestId`, `issues`, and
structured `details`. Branch on the code and issue path, never on human wording.

## Concurrency and side effects {#concurrency}

Version-enabled documents expose an integer revision. Send it with mutations—through
`ExpectedRevision`, the SDK `revision` option, or REST `If-Match`—to reject stale writes with a
conflict rather than overwrite another author. Document locks coordinate the admin editing
experience but do not replace authorization or optimistic revisions.

Hooks that send email, enqueue external work, or delete remote objects should register an
after-commit effect. Transaction hooks may run again when the surrounding operation is retried; an
after-commit dispatcher sees only committed work.

Continue with [Querying data](/docs/querying/), then choose the [Local Go API](/docs/local-api/),
[REST API](/docs/rest-api/), or [TypeScript SDK](/docs/typescript-sdk/) task guide.
