# How this Ridu project works

You do not need to understand every file before changing this project. Most day-to-day work happens
in `content/`. Everything else is either occasional setup, frontend customization, or generated
output.

## The shortest useful mental model

```text
content/*.go
    ↓  ridu generate / ridu dev
schema + typed Go/TypeScript contracts
    ↓
Go API + framework-owned admin
```

The Go files in `content/` are the source of truth. Ridu runs that Go configuration and derives the
database shape, API schema, generated types, and admin forms from it. Do not maintain a second copy
of the schema in TypeScript or JSON.

## What should I edit?

### Edit these for ordinary CMS work

| Path | What it does | When you edit it |
| --- | --- | --- |
| `content/config.go` | Assembles the application and collections. | Add/remove a collection or application-level service. Use `ridu add` for packaged plugins. |
| `content/posts.go` | Defines the starter Posts collection. | Add fields, change labels, access rules, hooks, or collection behavior. |
| `content/users.go` | Defines the authentication collection. | Change user fields or authentication-related rules. |
| `admin/src/plugins.ts` | Registers application-owned admin field plugins. | Add a custom Svelte field or another application admin extension. |
| `admin/src/fields/` | A conventional home for your custom field components. | Build or change a custom field UI. The directory exists only when you need it. |

You can split `content/` however you like. `posts.go` and `users.go` are ordinary Go files, not
special filenames. Ridu only cares about the `ridu.Config` returned by `content.Config()`.

### Configure these occasionally

| Path | What it does | Touch it when… |
| --- | --- | --- |
| `cmd/server/main.go` | Opens PostgreSQL and starts Ridu. | You need runtime wiring such as observability, server options, or another store setup. |
| `compose.yaml` | Runs the development PostgreSQL container. | You need different local ports or database settings. |
| `ridu.toml` | Tells the CLI where the entry, admin, generated contracts, migrations, and assets live. | You deliberately move one of those directories. |
| `ridu.plugins.json` | Records packaged plugin dependencies and constructors. | Normally use `ridu add` or `ridu plugin remove`; review the resulting diff. |
| `go.mod`, `go.sum` | Pin Go dependencies. | Add a Go package or upgrade Ridu. Normally use `go get`/`go mod tidy`. |
| `package.json`, `bun.lock` | Define the Bun workspace and frontend dependencies. | Add a frontend package or upgrade dependencies. Normally use `bun add`. |
| `admin/src/main.ts` | Mounts the framework-owned admin with this project's generated client and plugins. | You need top-level admin mounting options. Most projects leave it alone. |
| `admin/vite.config.ts` | Builds and serves the admin, proxies `/api`, and reloads after schema changes. | You have a real Vite/build requirement. Do not add schema definitions here. |
| `admin/tsconfig.json` | TypeScript settings for the admin. | A TypeScript integration specifically requires it. |
| `admin/index.html` | Browser HTML entry containing the admin mount element. | You need document-level HTML or metadata. |
| `admin/package.json` | Admin-only dependencies and scripts. | Add an admin build dependency. |
| `.gitignore` | Keeps caches, secrets, builds, and embedded admin output out of Git. | You add another disposable local artifact. |

### Do not hand-edit these

| Path | Owner | Why |
| --- | --- | --- |
| `generated/ridu.schema.json` | `ridu generate` | Canonical resolved schema used for review and drift checks. |
| `generated/ridu.openapi.json` | `ridu generate` | OpenAPI description of the REST API. |
| `generated/ridu.graphql` | `ridu generate` when `generated.graphql.schema` is configured | Exact SDL emitted by the compiled GraphQL plugin, including executable naming and extension options. |
| `generated/ridu.generated.go` | `ridu generate` | Exact Go document/input types and local collection handles. |
| `generated/ridu.generated.ts` | `ridu generate` | Exact TypeScript document, input, query types, and typed Fetch client factory. |
| `admin/src/ridu.plugins.generated.ts` | `ridu generate` | Static imports for packaged plugins declared by Go config. Put application-local plugins in `plugins.ts`. |
| `content/ridu_plugins.generated.go` | `ridu add` / `ridu plugin remove` | Compiled registration for packaged Go plugins. |
| `internal/adminassets/dist/` | `ridu build` / Vite | Compiled static admin embedded into the Go binary. |
| `dist/` | `ridu build` | Final production binary. |
| `.ridu/` | Ridu development tools | Disposable CLI, Vite, generation, and dogfood caches. |
| `node_modules/` | Bun | Installed frontend dependencies. |

Generated contracts are committed so CI can catch drift, but they are still machine-owned. Change
`content/*.go`, then run `ridu generate`; never fix a generated file by hand.

When the optional GraphQL plugin is enabled, set
`generated.graphql.schema = "./generated/ridu.graphql"` in `ridu.toml`. Ridu then generates and checks the
exact runtime SDL with the other contracts. Keep the key absent in REST-only projects.

## Common jobs

### Initialize the first administrator

The generated Users collection omits an anonymous Create policy deliberately. While the collection
is empty, Ridu's REST and GraphQL transports permit exactly one anonymous first-user creation and
serialize the emptiness check, document insert, and private credential insert in one transaction.
Concurrent contenders cannot both become administrators.

Open `/admin` in your browser. Ridu detects the empty admin-user collection, redirects you to
`/admin/create-first-user`, renders the fields from `Users`, creates the account, and signs you
in. After the first account exists, the setup route closes and unauthenticated visitors see the
normal login screen.

For non-interactive deployment automation, the same one-time operation is available over REST:

```sh
curl --fail-with-body http://127.0.0.1:8080/api/auth/users/create-user \
  --header 'content-type: application/json' \
  --data '{"data":{"email":"admin@example.test"},"password":"replace-with-a-long-secret"}'
```

Do not put a real production password into shared shell history; send the JSON from a secret-aware
deployment tool or an operator-only bootstrap job. Once any active user exists, an anonymous
repeat receives `403` and cannot create another account. Authenticated administrators can create
later users normally.

If the product intentionally supports public registration, add an explicit `Access.Create` rule to
`Users`. An explicit rule owns the decision and disables the framework's omitted-policy
first-user default for that collection.

### Change a collection label

Edit the collection in `content/posts.go`, for example:

```go
Labels: ridu.Labels{Singular: "Article", Plural: "Articles"},
```

With `ridu dev` running, Ridu builds one disposable candidate, executes that exact binary to
regenerate the manifest, and reuses it as the replacement Go server. After it is healthy, a custom
Vite event refreshes the admin manifest in place. The CLI prints build, manifest, contract,
database, and total timings. This is not ordinary module replacement because the source is
executable Go rather than a frontend module.

### Add a built-in field

Add it to the collection's `Fields` slice:

```go
field.Text("summary", field.Label("Summary"), field.Required()),
```

Save the file. `ridu dev` handles development generation and non-destructive database additions.
Run `ridu check` before committing.

### Rename a field safely

Rename the field normally in executable config:

```go
field.Text("headline")
```

Then run `ridu migrate create --name rename-post-title`. Ridu compares the latest immutable
migration artifact with current config and asks you to confirm an unambiguous rename before it
records the physical and semantic data-preservation steps. Review the artifact before applying it.

### Add a custom field

A persisted custom field has two halves:

1. A Go helper/plugin that defines validation and public manifest config.
2. A Svelte field plugin that edits the value in the admin.

Put the Go package somewhere under `content/` (for example `content/colorfield/`), register it from
`content/config.go`, create the Svelte component under `admin/src/fields/`, and register its admin
plugin in `admin/src/plugins.ts`. The Go and Svelte halves share one plugin key.

### Change only the custom field UI

Edit the `.svelte` file. Vite HMR updates the browser without restarting Go. If you change the Go
field definition or plugin config, Ridu regenerates the schema and reloads the page instead.

### Add access rules or hooks

Keep them with the collection that owns them or in another clearly named Go file under `content/`.
They are executable server behavior and are never serialized into the public schema manifest.

### Change the database

`compose.yaml` is only the local container. PostgreSQL integration is opened in
`cmd/server/main.go`, and the connection URL comes from `DATABASE_URL`. Development schema sync is
non-destructive. Review production changes through `ridu migrate create`, then apply them with
`ridu migrate up`.

### Configure the production boundary

The generated server keeps production checks in `cmd/server/main.go` and uses secure framework
defaults when an environment value is absent:

| Environment | Production meaning |
| --- | --- |
| `DATABASE_URL` | Required PostgreSQL URL. Use `sslmode=require`, `verify-ca`, or `verify-full`; plaintext fallback is rejected. |
| `RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS` | Optional size of the separate bounded PostgreSQL pool used to coordinate object creation/adoption/deletion across processes. The default is four; these connections are additional to the document pool so uploads cannot starve their own transactions. |
| `RIDU_ALLOWED_HOSTS` | Comma-separated public hostnames, with optional ports. Set it in production because an empty list accepts every syntactically valid host for development compatibility. |
| `RIDU_ALLOWED_ORIGINS` | Comma-separated browser origins allowed to make cross-origin SDK requests. Same-origin admin traffic does not need an entry. |
| `RIDU_TRUSTED_PROXY_CIDRS` | Only the immediate reverse-proxy networks allowed to supply forwarded client/proto information. Leave empty when the Go process is directly exposed. |
| `RIDU_STRICT_TRANSPORT_SECURITY` | HSTS value emitted by Ridu. Set it only when the TLS terminator guarantees HTTPS for the complete declared scope. |
| `RIDU_READINESS_TIMEOUT` | Optional Go duration bounding the combined database, object-storage, and application readiness probe. |
| `RIDU_SKIP_READINESS_PREFLIGHT` | Development-only startup escape used by `ridu dev`; never set it for an ordinary production server. |
| `RIDU_READINESS_DRAIN_DELAY` | Optional delay after readiness turns false and before request shutdown begins. |
| `RIDU_SHUTDOWN_TIMEOUT` | Optional bound for HTTP shutdown and runtime-resource cleanup. |
| `RIDU_WORKER_DRAIN_TIMEOUT` | Optional bound for cooperative task-worker drain. |

TLS normally terminates at the platform edge. It must sanitize untrusted forwarding headers before
sending them from one of `RIDU_TRUSTED_PROXY_CIDRS`. Ridu enables secure cookies by default; never
set `RIDU_SECURE_COOKIES=false` in production. `RIDU_ALLOW_INSECURE_DATABASE=true` and
`RIDU_ALLOW_UNVERIFIABLE_READINESS=true` are development/explicit-adapter escape hatches.
`RIDU_SKIP_READINESS_PREFLIGHT=true` is used only by `ridu dev`; it is not production
configuration.

Production startup performs readiness before binding the listener. The official PostgreSQL store
requires a reachable database, a complete immutable migration ledger whose digest matches the
executable manifest, and no interrupted migration phase. Apply and verify the exact release's
migrations before starting it. If uploads are configured with `ridu.WithUploadStorage`, the backend
must implement `storage.HealthBackend`; the official local and S3 backends do. A custom backend may
opt out only after equivalent external dependency checks exist.

Give the platform termination grace period more time than the readiness-drain, HTTP-shutdown,
worker-drain, and resource-close budgets combined. A code-only replacement with the same manifest
digest may overlap: keep the old instance available until the replacement passes `/readyz`.
For every manifest-changing release, drain old replicas and workers first, run
`verify`/`up`/`status` with the new exact history, then start only the new digest. `/healthz` only
proves that a process is alive and never admits mixed manifests.

## What the commands actually do

| Command | Use it for |
| --- | --- |
| `ridu dev` | Install frontend dependencies, start PostgreSQL, generate contracts, sync safe development schema changes, run API + admin, and watch Go. |
| `ridu generate` | Re-resolve Go config and atomically update generated contracts without starting the app. |
| `ridu check` | Check generated drift, a non-empty migration history at the exact executable manifest, formatting, Go vet/tests, TypeScript, and Svelte. |
| `ridu build` | Generate, build the static admin, and compile one production Go binary. |
| `ridu migrate create --name …` | Create reviewable SQL for production schema changes. |
| `ridu migrate plan --json` | Inspect immutable pending phase, step, and checkpoint state without applying it. |
| `ridu migrate verify` | Replay committed history in a shadow schema and reject drift before deployment. |
| `ridu migrate up` | Apply reviewed artifacts under the production migration lock. |

## Why some changes hot-update and others reload

- Svelte, TypeScript, and CSS changes belong to Vite, so they use normal hot module replacement.
- Go changes build one candidate reused for manifest resolution and the replacement server.
- If that Go change alters the canonical schema, Ridu waits for the replacement server, sends a
  custom Vite event, and the admin fetches and reconciles the new manifest in place.
- If the Go change only alters a hook or access rule, the server restarts but the page stays put
  because the admin schema did not change.

Open forms reconcile fields by stable ID. Compatible dirty values survive label, metadata,
addition, removal, and stable-ID rename changes; values from removed or retyped fields appear in a
recovery panel instead of being submitted under an invalid schema. A generated admin plugin import
change still needs a full reload, so the admin checkpoints dirty form values in `sessionStorage`
and reapplies compatible changes afterward. Browsers do not allow file-input selections to be
restored after a full reload.

## Dogfood-only files

If this project came from Ridu's `make dogfood-new`, ignored `.ridu/packages/` workspaces and
`.ridu/bin/ridu` stand in for packages that have not been published yet. Do not write application
code in `.ridu/`. Use `./.ridu/bin/ridu` so the project and CLI use the same dogfood release.

## When confused, use this rule

If you are changing **what content exists or how the server behaves**, start in `content/`.

If you are changing **how a custom field looks or behaves in the admin**, start in `admin/src/`.

If the filename contains `generated`, or lives in `.ridu/`, `dist/`, `node_modules/`, or
`internal/adminassets/dist/`, do not hand-edit it.
