---
title: 'Troubleshooting'
description: 'Diagnose common Ridu setup, generation, migration, SDK, auth, and runtime failures by symptom.'
product: cli
eyebrow: 'Help'
order: 160
navigation:
  section: 'Develop & operate'
  order: 110
  title: 'Troubleshooting'
---

Start with the exact error and the narrowest failing command.

| Symptom                         | First command or check                          | Continue with                                     |
| ------------------------------- | ----------------------------------------------- | ------------------------------------------------- |
| CLI or package cannot be found  | `command -v ridu` and `ridu version`            | [Installation](/docs/installation/)               |
| Generated files differ          | `ridu generate --check`                         | [Generated contracts](/docs/generated-contracts/) |
| Config or schema fails          | `ridu generate`                                 | [Configuration](/docs/configuration/)             |
| Migration is pending/refused    | `ridu migrate status` and `ridu migrate verify` | [Migrations](/docs/migrations/)                   |
| Runtime is live but not ready   | Request `/readyz` and inspect its cause         | [Production readiness](/docs/production/#health)  |
| Query or population is rejected | Reduce the request to one filter/path           | [Querying data](/docs/querying/)                  |
| Admin component fails to build  | `bun run check` in the generated project        | [Custom components](/docs/custom-components/)     |

## `ridu` or a package cannot be found {#command-not-found}

**Likely cause:** the CLI installation directory is not on `PATH`, the project has not installed its
committed frontend dependencies, or its Go and `@riducms/*` versions do not match the CLI release.

Confirm which binary the shell finds and print its version:

```sh
command -v ridu
ridu version
```

If the command is missing, install the released CLI and make sure Go's binary directory is on
`PATH`:

```sh
go install github.com/riducms/ridu/cmd/ridu@latest
```

Inside a generated project, run `ridu doctor` for tool availability and basic project prerequisites.
It does not compare release versions: compare `ridu version`, `go list -m github.com/riducms/ridu`,
and the `@riducms/*` versions in the project's package files and lockfile yourself. Restore
dependencies with the package manager and versions committed by the project, then run
`ridu generate --check` and `ridu check`. Do not add absolute local `replace` directives or Vite
aliases as a version workaround.

## `ridu new` refuses the project {#new-refuses-project}

Check all three inputs:

- the target path must not already exist;
- the project name must use lowercase kebab-case;
- `--module` must begin with a domain, such as `github.com/acme/content`; and
- `--scope` must be a lowercase npm-safe scope such as `@acme`.

If dependency installation or generation fails, the project is kept and the CLI prints
the `cd`, `go mod tidy`, and `ridu generate` recovery commands. Run those commands after restoring
registry or network access rather than deleting application work.

See [CLI](/docs/cli/) and the [generated project tour](/guides/project-structure/).

## Config resolution or generation fails {#config-generation-fails}

Ridu executes your project command; it does not parse Go source. Work through the failure in this
order:

1. Run Go formatting and tests to find a compiler error or invalid callback signature.
2. Run `ridu generate` directly so the project-command or schema diagnostic is not hidden among dev
   server logs.
3. Check for duplicate collection, global, or field identifiers; invalid reserved paths; a
   relationship to a missing collection; an invalid locale fallback graph; or an unsupported
   unique/index shape.
4. Confirm every backend plugin and paired admin package supports the same Ridu and plugin API
   versions.

Generation is atomic. A failed build, config validation, protocol handshake, encoding step, or
final rename leaves the prior artifact set intact. Fix the source config and regenerate; do not
patch `generated/` by hand.

Read [Configuration](/docs/configuration/), [Fields](/docs/fields/), and
[Plugins](/docs/plugins/) for the owning contract.

## `ridu generate --check` reports drift {#generated-drift}

The committed schema, OpenAPI, Go models, TypeScript client, and generated admin plugin imports no
longer match executable config. Run:

```sh
npm run ridu -- generate
git diff -- generated admin/src/ridu.plugins.generated.ts
```

Review the complete set together and commit intended contract changes with the config change. If
generation changes files on every run, stop: output is required to be deterministic. Check for a
plugin transform that depends on map iteration, time, random values, machine-specific paths, or
external I/O.

## `ridu dev` cannot start {#dev-cannot-start}

Run `ridu doctor` first. It checks structural project settings, Go and the selected package
manager, and basic database or Docker/OrbStack prerequisites. A successful result does not prove
that Go config compiles, generated contracts are current, or the database is reachable. Use
`ridu generate --check` and `ridu check` for config and contract validation; inspect the actual
`ridu dev` error for connection or startup failures.

Common branches are:

- set `--database-url` or `DATABASE_URL` when PostgreSQL or MongoDB is managed outside the project;
- set `--database-path` or `RIDU_SQLITE_PATH` when a SQLite project should not use its development
  default;
- use `--no-docker` only when you have provided that external database;
- use `--no-install` only after installing the committed frontend dependencies; and
- use `--no-sync` when another tool manages development schema synchronization.

Ridu reloads the admin only after a replacement server is healthy. If a Go edit restarts the server
but the browser keeps an old schema, confirm generation completed and no invalid config prevented
the healthy replacement. See the selected adapter's [PostgreSQL](/docs/postgres/) or
[SQLite](/docs/sqlite/) guide, or [MongoDB](/docs/mongodb/) guide.

## A migration is refused {#migration-refused}

Use the migration command that answers the question you have:

- `ridu migrate create` resolves config and writes a reviewable artifact without connecting to a
  database.
- `ridu migrate verify` replays history in an isolated PostgreSQL schema or temporary SQLite or
  MongoDB database.
- `ridu migrate status` compares committed artifacts with the target database.
- `ridu migrate up` applies verified pending artifacts under adapter-owned coordination: a
  PostgreSQL advisory lock, one atomic SQLite writer transaction, or MongoDB's fenced expiring
  lease and durable step ledger.

If creation finds a destructive change, review the machine-readable finding and pass
`--allow-destructive` only when data loss is intentional and rehearsed. PostgreSQL and MongoDB can
confirm an unambiguous detected rename to preserve semantic continuity. SQLite requires a
registered compiled transform; `--accept-renames` alone is intentionally rejected because it
cannot rewrite canonical JSON. MongoDB can bind a named compiled transform to its immutable
artifact when explicit schema-driven rewriting is required.

If status reports a digest mismatch, missing history, or changed applied artifact, restore the
committed artifact that was actually applied. Migration files are immutable; do not edit applied
PostgreSQL SQL, SQLite steps/transforms, or MongoDB plans/transforms. Authenticated MongoDB
planner-`1.0.0` history remains a supported immutable prefix to v2; do not rewrite it. PostgreSQL and MongoDB
recovery uses a forward corrective migration or a coordinated restore. SQLite's reviewed `down`,
`reset`, `refresh`, and `fresh`
commands require explicit destructive approval: `down`, `reset`, and `refresh` execute immutable
reverse steps, while `fresh` drops non-internal objects and replays committed up history.

PostgreSQL and MongoDB database-backed commands need a reachable URL, verified TLS and suitable
privileges, and time for their bounded lock/lease and operation timeouts. Keep MongoDB's application
credential scoped to its one database and use separate short-lived credentials for verification
and backups. SQLite `create` is offline and writes only its
artifact, while `verify` uses a temporary database and needs no deployment path. SQLite `plan` and
`status` do not apply migrations, but they open the selected path through the ordinary writable
SQLite store configuration. The database and parent directory must therefore be writable, and a
missing path may be created during inspection. Mutating commands additionally need an available
writer transaction. See [Production](/docs/production/) before applying a plan to valuable data.

Do not repair a MongoDB cutover by changing the order. Drain old processes, run command-scoped
`verify` with the operational URL, capture the matched database/upload snapshot, run `up` with the
selected app/operator URL, require post-`up` `status`, then start with the app URL.

## Login works in one client but not the browser {#browser-auth-fails}

Check the browser boundary rather than weakening authentication:

- the SDK defaults to Fetch credentials mode `include`; preserve it if you provide custom Fetch
  options;
- configure allowed origins and hosts for the actual browser URL;
- configure allowed request headers if your frontend sends custom headers;
- use secure cookies behind HTTPS, and configure trusted proxies only for infrastructure you
  control; and
- verify that the auth collection named by `Admin.User` exists and that the user is active.

Use the [CORS guide](/docs/cors/) to distinguish an unlisted origin, custom-header preflight, cookie
SameSite restriction, and untrusted TLS proxy. If the admin-user collection is empty and its Create
policy is omitted, the admin opens its
one-time first-user setup screen. An explicit Create policy owns registration instead and disables
that default. See [Authentication](/docs/authentication/) and [REST API](/docs/rest-api/).

## A request is denied or a field disappears {#access-denied}

Ridu applies collection access, field access, and redaction to every operation surface. A local Go
call is not privileged, and a hidden admin control is not authorization.

Check these cases:

- a nil actor means anonymous, never superuser;
- a filtered access decision must match the target row atomically;
- field read access may remove a value from the response even though the row itself is readable;
- an update may be allowed at collection level but reject a protected field; and
- create-field access defaults and update-field access are distinct contracts.

Log the authenticated actor and operation kind, then test the access callback directly with an
allowed, denied, and predicate-filtered case. Do not fetch broadly and filter rows in application
memory. [Access control](/docs/access-control/) explains the decision model.

## A query is rejected or a populated response is too large {#query-too-large}

Confirm every filter operator matches the field type, every selected path exists, and every
population path is a relationship. Then reduce `depth`, `limit`, selected fields, or nested
population.

Ridu validates query complexity and bounds relationship expansion before materializing a response.
Those limits protect the server from cycles and unbounded graph reads; splitting a large graph into
several explicit requests is preferable to raising limits blindly. The generated TypeScript client
helps with path and response types, but the server remains the final validator. See
[TypeScript SDK](/docs/typescript-sdk/) and [REST API](/docs/rest-api/).

## The SDK falls back to broad types {#sdk-broad-types}

Import the generated project client, not only the framework-neutral `@riducms/sdk` package. Exact
document, input, select, query, and population types come from the generated manifest.

The SDK can infer a default config when exactly one generated Ridu config is registered in the
TypeScript program. With zero or multiple generated configs, use the generated wrapper or provide
the config type explicitly. This avoids silently choosing the wrong application in monorepos.

With `ridu dev` running, save the schema change and wait for regeneration. Restart the TypeScript
language service if the editor has cached the previous declarations. If the development loop is not
running, use `ridu generate` once before restarting the language service.

## `/healthz` passes but `/readyz` fails {#readiness-fails}

Liveness says the process is running; readiness says it can safely serve traffic with required
dependencies.

Check the selected database path or connection, migration/manifest state, and configured storage
backends. MongoDB readiness additionally requires the executable manifest to be the exact complete
ledger head and every required Ridu index to match. In production, Ridu requires stores and upload
backends to provide readiness checks unless the application enables unverifiable readiness. Enable
that option only after moving the omitted checks to an external system.

Use `ridu migrate status` to inspect the ledger, durable progress, and managed database state; a
ready process does not replace that deployment check.

Keep load balancers on `/readyz` and process restarts on `/healthz`. [Production](/docs/production/)
covers the wider deployment checks.

## Upload cleanup or storage health fails {#upload-storage-fails}

For S3-compatible storage, verify endpoint, region, bucket, access key, and secret key. Non-HTTPS
endpoints are rejected unless `AllowInsecureEndpoint` is explicitly enabled for a trusted local
environment. Signed URL lifetimes must remain within the backend's supported bounds.

For cleanup and reconciliation, storage listings must provide a non-zero modification time. Keep
database and object-store backups as one recovery point: restoring only metadata or only objects
can create missing files or orphans. Do not trust client-supplied storage keys or checksums; upload
metadata is server-owned.

Read [Uploads](/docs/uploads/) and [Storage](/docs/storage/) before changing cleanup windows or
deleting unmatched objects.

## A hook ran twice or caused recursion {#hook-side-effects}

Hooks execute inside a defined operation lifecycle, and transactional work may be retried. Make
external side effects idempotent and place them in the documented after-commit phase rather than a
before hook. When a hook calls the local API, pass the hook context and deliberately choose whether
the nested operation should run hooks; an unguarded write back to the same collection can recurse.

Not every field or global hook phase exists. Use only the phases exposed by the public contract,
and fail generation rather than assuming a Payload hook name has a Ridu equivalent. See
[Hooks](/docs/hooks/) and [Move from Payload](/guides/from-payload/).

## Still stuck? {#still-stuck}

Capture the Ridu revision, Go and Bun versions, operating system, command, full structured error,
and the smallest config that reproduces it. For database issues, include the selected adapter,
PostgreSQL version, SQLite path shape, or MongoDB version/topology/TLS mode, and migration status
without secrets. Run `ridu doctor` and the narrow failing command before the full project check;
their output makes a report much easier to act on.
