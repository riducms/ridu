<!-- Generated from website/src/content/docs/postgres.md by scripts/sync-agent-docs.ts. -->

# PostgreSQL

PostgreSQL 17 is Ridu's official networked and multi-replica store. The adapter implements documents, globals,
relationships, localization, auth, versions, tasks, locks, preferences, reference indexes,
migrations, and readiness behind Ridu's public store contracts.

## Start a new PostgreSQL project {#new-project}

PostgreSQL is the scaffold default:

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

Choose **Starter** and **PostgreSQL** in the wizard, then run the exact install and `dev` commands it
prints. PostgreSQL is preselected, but the summary still makes the choice explicit before any files
are written.

<details class="docs-disclosure">
<summary>Preselect every scaffold choice</summary>
<div class="docs-disclosure-body">

For a non-interactive npm setup, pass every choice as an argument:

```bash title="terminal"
npm create ridu@latest -- \
	--template starter \
	--database postgres \
	--module github.com/acme/content \
	--scope @acme \
	--package-manager npm \
	--agent codex \
	content
```

</div>
</details>

The generated `compose.yaml` exposes PostgreSQL 17 on `127.0.0.1:54329`, and the `dev` script starts it,
waits for `pg_isready`, performs safe additive development synchronization, and runs the API/admin.
To use a service you already operate, supply its secret only to the command and skip Compose:

```bash title="terminal" package-manager="npm"
DATABASE_URL="$DEVELOPMENT_DATABASE_URL" npm run dev -- --no-docker
```

```bash title="terminal" package-manager="bun"
DATABASE_URL="$DEVELOPMENT_DATABASE_URL" bun run dev -- --no-docker
```

```bash title="terminal" package-manager="pnpm"
DATABASE_URL="$DEVELOPMENT_DATABASE_URL" pnpm run dev --no-docker
```

```bash title="terminal" package-manager="yarn"
DATABASE_URL="$DEVELOPMENT_DATABASE_URL" yarn run dev --no-docker
```

## Add PostgreSQL to an existing project {#existing-project}

Use this path for a clean Ridu project that does not yet have committed migrations or data. Ridu
does not provide a generic live SQLite/MongoDB-to-PostgreSQL migration facility; moving an existing
dataset between adapters is an application-owned export, transform, validation, and cutover.

1. Replace the current adapter in `ridu.toml`. This example starts from SQLite; if the project
   currently names MongoDB, replace that line instead:

   ```toml title="ridu.toml" remove={2} add={3}
   version = 1
   database = "sqlite"
   database = "postgres"
   entry = "./cmd/server"
   admin = "./admin"
   ```

2. Replace the old adapter import and store factory inside the existing `runtimeOptions` function.
   Keep the generated address, admin assets, handler options, and server options around it. Lazy
   opening keeps config resolution and generation offline:

   ```go title="cmd/server/main.go" remove={8,15} add={7,16-20}
   import (
     "context"
     "os"

     "github.com/acme/content/internal/adminassets"
     "github.com/riducms/ridu"
     "github.com/riducms/ridu/adapters/postgres"
     "github.com/riducms/ridu/adapters/sqlite"
     "github.com/riducms/ridu/store"
   )

   func runtimeOptions(applicationConfig ridu.Config) []ridu.ExecuteOption {
     return []ridu.ExecuteOption{
       ridu.WithStore(func(ctx context.Context) (store.Store, error) {
         return sqlite.Open(ctx, sqliteDatabasePath())
         return postgres.OpenWithConfig(ctx, postgres.PoolConfig{
           DatabaseURL:              os.Getenv("DATABASE_URL"),
           AllowInsecureTransport:   envBool("RIDU_ALLOW_INSECURE_DATABASE"),
           MaxUploadLockConnections: envInt32("RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS"),
         })
       }),
       ridu.WithAddress(env("RIDU_ADDRESS", ":8080")),
       ridu.WithHandlerOptions(ridu.HandlerOptions{
         AdminAssets:    adminassets.FS(),
         AllowedOrigins: envList("RIDU_ALLOWED_ORIGINS"),
       }),
     }
   }
   ```

   The duplicate return statements and adapter imports represent the before/after diff—the finished
   file contains only the PostgreSQL lines. The generated server template also supplies the
   `envBool` and `envInt32` parsers; copy them from a fresh `--database postgres` scaffold if the
   current entry does not already have them.

3. Either copy the generated PostgreSQL service from a disposable scaffold into `compose.yaml`, or
   point at an existing PostgreSQL 17 service. The generated local service uses database/user/password
   `ridu`, host port `54329`, and `sslmode=disable`; never carry that plaintext credential into
   production.

4. Start the development loop against the selected service:

   ```bash title="terminal" package-manager="npm"
   go mod tidy
   DATABASE_URL="$POSTGRES_URL" npm run dev -- --no-docker
   ```

   ```bash title="terminal" package-manager="bun"
   go mod tidy
   DATABASE_URL="$POSTGRES_URL" bun run dev -- --no-docker
   ```

   ```bash title="terminal" package-manager="pnpm"
   go mod tidy
   DATABASE_URL="$POSTGRES_URL" pnpm run dev --no-docker
   ```

   ```bash title="terminal" package-manager="yarn"
   go mod tidy
   DATABASE_URL="$POSTGRES_URL" yarn run dev --no-docker
   ```

   Use the generated Compose URL for `$POSTGRES_URL`, or an already managed development URL. Remove
   insecure admission when the service uses verified TLS. `ridu dev` resolves the Go config,
   regenerates contracts, synchronizes safe additive changes, and starts the API and admin. Before
   deploying, create and verify immutable migration artifacts using the
   [migration workflow](#migrations) below.

## Connect the generated server {#connect}

Generated applications open the store lazily so schema generation never needs database access.
The factory belongs inside `runtimeOptions`, between the surrounding address and handler settings,
as shown in the [existing-project diff](#existing-project) above. Do not open a database from a
package initializer or from `content.Config()`.

Use encrypted transport outside an explicitly local environment:

```sh title="terminal"
export DATABASE_URL='postgres://ridu:secret@db.example.com:5432/content?sslmode=verify-full'
export RIDU_ADDRESS=':8080'
```

`postgres.Open` uses bounded defaults. Use `OpenWithConfig` when the application needs deliberate
pool or session tuning:

```go title="cmd/server/main.go"
return postgres.OpenWithConfig(ctx, postgres.PoolConfig{
	DatabaseURL:                     os.Getenv("DATABASE_URL"),
	ApplicationName:                 "acme-content",
	MaxConnections:                  30,
	MinConnections:                  2,
	MaxUploadLockConnections:        4,
	ConnectTimeout:                  10 * time.Second,
	StatementTimeout:                45 * time.Second,
	LockTimeout:                     5 * time.Second,
	IdleInTransactionSessionTimeout: 30 * time.Second,
})
```

Size connections against the database service and the total replica count, not one process in
isolation.

## Pool defaults and limits {#pool}

Zero values select the following defaults:

| Setting                  |                         Default | Purpose                                                                                                         |
| ------------------------ | ------------------------------: | --------------------------------------------------------------------------------------------------------------- |
| Document connections     |           20 maximum, 0 minimum | Bounds ordinary content and framework-state work per process.                                                   |
| Upload-lock connections  |                       4 maximum | Separate advisory-lock pool so staged uploads cannot consume the document connection needed to commit metadata. |
| Connection lifetime      | 1 hour ± up to 5 minutes jitter | Rotates connections without synchronizing every replica.                                                        |
| Idle connection lifetime |                      30 minutes | Releases unused capacity.                                                                                       |
| Pool health check        |                        1 minute | Checks idle connections.                                                                                        |
| Connect timeout          |                      10 seconds | Bounds startup and new connections.                                                                             |
| Statement timeout        |                      60 seconds | Bounds server-side statements.                                                                                  |
| Lock timeout             |                      10 seconds | Bounds PostgreSQL lock waits and upload-lock admission.                                                         |
| Idle transaction timeout |                      60 seconds | Prevents abandoned sessions from holding transaction state indefinitely.                                        |

A negative duration explicitly disables that individual timeout; connection counts cannot be
negative, minimum cannot exceed maximum, and server timeout values must use whole milliseconds.
An unbounded setting should be a measured exception, not a routine fix for blocked work.

The upload-lock pool is additional to `MaxConnections`. Startup and readiness ping both pools. Loss
or exhaustion of either makes the instance unready before the next upload discovers it.

## TLS is required by default {#tls}

`OpenWithConfig` rejects plaintext and TLS-fallback URLs unless `AllowInsecureTransport` is true.
Use `sslmode=require`, `verify-ca`, or preferably `verify-full` in production. Modes such as
`prefer`, `allow`, and `disable` permit fallback and are rejected.

`AllowInsecureTransport` exists for local Unix sockets and development databases protected outside
PostgreSQL. Do not expose it as a silent production fallback. Protect credentials independently and
use a least-privilege application role.

## Readiness checks {#readiness}

The store exposes connectivity, manifest, physical-schema, and exact-history checks:

| Check                                            | Required state                                                                                                                                                                                                          |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Ping(ctx)`                                      | Both document and upload-lock pools can reach PostgreSQL.                                                                                                                                                               |
| `Ready(ctx, manifest)`                           | Ping succeeds, the immutable migration ledger exists, its latest complete digest exactly matches the executable manifest, no phased migration work is incomplete, and the expected physical schema passes verification. |
| `ReadyWithMigrationHistory(ctx, manifest, hash)` | Ordinary readiness passes and the complete ordered ledger matches the filename/digest fingerprint embedded by `ridu build`.                                                                                             |

`ridu.Execute` runs aggregate readiness before binding its listener and includes the same store
check in `/readyz`. Production uses the exact-history form, so it also rejects a missing, altered,
renamed, reordered, or additional artifact even when the final manifest happens to be unchanged.
An older binary becomes unready after a migration, and a new binary is unready before it.

Readiness verifies the expected Ridu-managed physical schema, including required tables and
indexes, index build health, and trigger enablement. It shares this non-mutating verification with
migration status and completed-migration preflight. Run `ridu migrate status` for the detailed
ledger, phase/step, and physical-schema drift report. See [Production](./production.md) for probe
and drain behaviour.

## Transactions and access remain atomic {#transactions}

Ridu's operation engine owns the lifecycle; PostgreSQL supplies its transactional implementation.
Caller filters and filtered access predicates are compiled into the same SQL query for reads,
updates, and deletes. A local Go call, REST request, task, admin action, or plugin transport cannot
bypass that predicate by choosing another adapter entry point.

Ordinary writes use a transaction. Read snapshots use repeatable-read, read-only transactions.
Relationship admission and deletion coordinate row locks so a dangling target cannot commit;
expected revisions produce stable conflicts without revealing access-filtered documents; hook or
validation failure rolls persistence back; and joined mutations retain the outer transaction
boundary.

Retryable PostgreSQL serialization and deadlock outcomes become stable operation conflicts rather
than raw driver errors. Application callers should re-read and deliberately retry the operation
instead of replaying a stale mutation blindly.

## Schema ownership {#schema-ownership}

Ridu owns the configured PostgreSQL schema, including its content tables, framework state,
migration ledgers, indexes, and declared `ridu_plugin_<key>_...` tables. Unrelated tables in that
schema are reported as drift.

When another system shares the database, give Ridu a dedicated schema through the connection
`search_path`:

```text
postgres://ridu:secret@db.example.com/content?sslmode=verify-full&search_path=ridu_content
```

Do not grant a plugin ownership of a core or unrelated table merely to hide drift. Plugin table
prefixes are validated as part of descriptor resolution.

## Development and production migrations {#migrations}

`ridu dev` can apply additive, non-destructive development synchronization. It pauses when a
possible rename needs explicit intent. Production uses committed immutable artifacts:

```sh title="terminal" package-manager="npm"
npm run ridu -- migrate create --name rename-post-title
npm run ridu -- migrate plan
npm run ridu -- migrate verify
npm run ridu -- migrate status
npm run ridu -- migrate up
```

```sh title="terminal" package-manager="bun"
bun run ridu -- migrate create --name rename-post-title
bun run ridu -- migrate plan
bun run ridu -- migrate verify
bun run ridu -- migrate status
bun run ridu -- migrate up
```

```sh title="terminal" package-manager="pnpm"
pnpm run ridu migrate create --name rename-post-title
pnpm run ridu migrate plan
pnpm run ridu migrate verify
pnpm run ridu migrate status
pnpm run ridu migrate up
```

```sh title="terminal" package-manager="yarn"
yarn run ridu migrate create --name rename-post-title
yarn run ridu migrate plan
yarn run ridu migrate verify
yarn run ridu migrate status
yarn run ridu migrate up
```

Creation is offline. The database-backed commands use their own bounded pools, TLS policy,
advisory-lock admission, and schema assertions. Read [Migrations](./migrations.md) for exact
command semantics, destructive and maintenance admission, resumable phases, and recovery.

## Test against PostgreSQL {#testing}

Use a disposable PostgreSQL 17 database for application tests that cover access rules,
localization, transactions, relationship locks, optimistic conflicts, tasks, uploads, migrations,
or recovery. Give each test suite its own database or schema and clean it afterward. Never give a
test role access to production.

Run `ridu migrate verify` against a restored backup before deployment, then exercise the Local API,
REST or SDK paths your application depends on. Review [Testing](./testing.md),
[Releases and compatibility](https://riducms.com/docs/releases/), and [Security](https://riducms.com/docs/security/) for the wider checks.
