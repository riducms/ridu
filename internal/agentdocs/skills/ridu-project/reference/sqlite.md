<!-- Generated from website/src/content/docs/sqlite.md by scripts/sync-agent-docs.ts. -->

# SQLite

SQLite is Ridu's official embedded database adapter for local and small deployments. It implements
the same content, relationship, auth, versions, uploads, preferences, document locks, and durable
task contracts used by the operation engine. The pure-Go driver needs no CGO, database service, or
JavaScript production runtime.

## Supported operating envelope {#operating-envelope}

Use SQLite within this operating envelope:

| Supported                                                          | Not supported                                                    |
| ------------------------------------------------------------------ | ---------------------------------------------------------------- |
| An ordinary database file on storage local to the application host | NFS, SMB, or another shared network filesystem                   |
| One application host; same-host processes use SQLite file locking  | Multiple application hosts or replicas sharing the file          |
| Small or local workloads that tolerate one writer                  | A horizontally scaled or independently operated database service |
| SQLite-aware online backup or a stopped, quiesced, WAL-safe copy   | Copying only the live main database file while WAL is active     |

SQLite file locking and busy-timeout handling coordinate ordinary same-host processes; they are not
a distributed-database or high-availability contract. Choose [PostgreSQL](./postgres.md) when the
database must be remote, independently operated, shared across horizontally replicated Ridu
deployments, or scaled beyond this envelope. Private in-memory databases are useful for tests and
process-owned tools, not durable deployments.

## Create and run a SQLite project {#new-project}

Select the adapter when scaffolding:

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

Choose **Starter** and **SQLite** in the wizard, then run the exact install and `dev` commands it
prints. The summary confirms the embedded adapter before any files are written.

<details class="docs-disclosure">
<summary>Preselect every scaffold choice</summary>
<div class="docs-disclosure-body">

For a non-interactive npm setup, pass every choice as an argument:

```bash title="terminal"
npm create ridu@latest -- \
	--template starter \
	--database sqlite \
	--module github.com/acme/content \
	--scope @acme \
	--package-manager npm \
	--agent codex \
	content
```

</div>
</details>

The generated `ridu.toml` records `database = "sqlite"`, the server opens `RIDU_SQLITE_PATH`, and no
PostgreSQL Compose service is generated. During development, `--database-path`, `RIDU_SQLITE_PATH`,
or the project-local `.ridu/development.sqlite` default selects the file. An explicit
`--database-path` may be project-relative; `RIDU_SQLITE_PATH` and the generated production server
require an absolute path or `file:` URI:

```bash title="terminal"
export RIDU_SQLITE_PATH='/var/lib/ridu/content.sqlite'
export RIDU_ADDRESS=':8080'
```

Keep the database and its parent directory writable only by the application identity. Do not place
the file on a shared volume as a substitute for PostgreSQL.

## Add SQLite to an existing project {#existing-project}

Use this path for a clean project before it has committed migration history or production data.
Ridu has no generic live PostgreSQL/MongoDB-to-SQLite migration facility. Moving an existing
dataset is application-owned and must preserve IDs, references, localized values, versions, auth
state, uploads, and a rehearsed cutover.

1. Replace the adapter in project metadata and remove any database service that is no longer used.
   This example starts from PostgreSQL; replace whichever adapter the project currently names:

   ```toml title="ridu.toml" remove={2} add={3}
   version = 1
   database = "postgres"
   database = "sqlite"
   entry = "./cmd/server"
   admin = "./admin"
   ```

2. Wire the server to the official adapter inside `runtimeOptions`. Keep the address and handler
   settings beside the store factory; the helper can remain lower in the same file:

   ```go title="cmd/server/main.go" remove={11,19-23} add={4,6,12,24,34-57}
   import (
       "context"
       "log"
       "net/url"
       "os"
       "path/filepath"
       "strings"

       "github.com/acme/content/internal/adminassets"
       "github.com/riducms/ridu"
       "github.com/riducms/ridu/adapters/postgres"
       "github.com/riducms/ridu/adapters/sqlite"
       "github.com/riducms/ridu/store"
   )

   func runtimeOptions(applicationConfig ridu.Config) []ridu.ExecuteOption {
       return []ridu.ExecuteOption{
           ridu.WithStore(func(ctx context.Context) (store.Store, error) {
               return postgres.OpenWithConfig(ctx, postgres.PoolConfig{
                   DatabaseURL:              os.Getenv("DATABASE_URL"),
                   AllowInsecureTransport:   envBool("RIDU_ALLOW_INSECURE_DATABASE"),
                   MaxUploadLockConnections: envInt32("RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS"),
               })
               return sqlite.Open(ctx, sqliteDatabasePath())
           }),
           ridu.WithAddress(env("RIDU_ADDRESS", ":8080")),
           ridu.WithHandlerOptions(ridu.HandlerOptions{
               AdminAssets:    adminassets.FS(),
               AllowedOrigins: envList("RIDU_ALLOWED_ORIGINS"),
           }),
       }
   }

   func sqliteDatabasePath() string {
       path := strings.TrimSpace(os.Getenv("RIDU_SQLITE_PATH"))
       switch {
       case path == "":
           log.Fatal("RIDU_SQLITE_PATH is required")
       case path == ":memory:":
           log.Fatal("RIDU_SQLITE_PATH must be a file because migrations and the server run in separate processes")
       case strings.HasPrefix(path, "file:"):
           parsed, err := url.Parse(path)
           if err != nil {
               log.Fatalf("RIDU_SQLITE_PATH must be a valid SQLite file URI: %v", err)
           }
           target := parsed.Path
           if target == "" {
               target, err = url.PathUnescape(parsed.Opaque)
           }
           if err != nil || !filepath.IsAbs(target) {
               log.Fatal("RIDU_SQLITE_PATH file URI must contain an absolute path")
           }
       case !filepath.IsAbs(path):
           log.Fatal("RIDU_SQLITE_PATH must be an absolute file path")
       }
       return path
   }
   ```

   The duplicate adapter imports and return statements show the before/after change. Remove every
   red line; the finished file contains only the SQLite import and `sqlite.Open` call.

   A fresh generated SQLite project contains the canonical helper, including support for an
   absolute opaque `file:` URI. Prefer copying that generated helper verbatim if you use URI paths.

3. Start the development loop against the file:

   ```bash title="terminal" package-manager="npm"
   go mod tidy
   RIDU_SQLITE_PATH=/absolute/path/to/content.sqlite npm run dev
   ```

   ```bash title="terminal" package-manager="bun"
   go mod tidy
   RIDU_SQLITE_PATH=/absolute/path/to/content.sqlite bun run dev
   ```

   ```bash title="terminal" package-manager="pnpm"
   go mod tidy
   RIDU_SQLITE_PATH=/absolute/path/to/content.sqlite pnpm run dev
   ```

   ```bash title="terminal" package-manager="yarn"
   go mod tidy
   RIDU_SQLITE_PATH=/absolute/path/to/content.sqlite yarn run dev
   ```

`ridu dev` regenerates contracts, synchronizes safe additive schema changes, and starts the API and
admin. Omit `RIDU_SQLITE_PATH` to use its project-local `.ridu/development.sqlite` default. The
generated production server requires an absolute
`RIDU_SQLITE_PATH`, because migrations and the binary are separate processes and must agree on one
durable file. Follow the immutable migration workflow below before deploying.

## Transactions and local concurrency {#transactions}

File databases enable WAL, foreign keys, a bounded busy timeout, and `synchronous=FULL` by default.
Read-only operations use snapshot transactions and can overlap the active writer in WAL mode.
SQLite still has one writer: write-capable operation transactions reserve it up front, and
contention fails when the configured timeout or caller deadline expires.

Nested reads reuse the operation snapshot. A mutation attempted inside a read-only lifecycle fails
before hooks or persistence rather than escaping into another transaction.

## Use immutable migrations {#migrations}

Production does not mutate schema at startup. Create and verify adapter-owned artifacts offline,
then apply them as a deployment step:

```bash title="terminal" package-manager="npm"
npm run ridu -- migrate create --name initial
npm run ridu -- migrate verify
npm run ridu -- migrate plan --database-path /var/lib/ridu/content.sqlite
npm run ridu -- migrate up --database-path /var/lib/ridu/content.sqlite
npm run ridu -- migrate status --database-path /var/lib/ridu/content.sqlite
```

```bash title="terminal" package-manager="bun"
bun run ridu -- migrate create --name initial
bun run ridu -- migrate verify
bun run ridu -- migrate plan --database-path /var/lib/ridu/content.sqlite
bun run ridu -- migrate up --database-path /var/lib/ridu/content.sqlite
bun run ridu -- migrate status --database-path /var/lib/ridu/content.sqlite
```

```bash title="terminal" package-manager="pnpm"
pnpm run ridu migrate create --name initial
pnpm run ridu migrate verify
pnpm run ridu migrate plan --database-path /var/lib/ridu/content.sqlite
pnpm run ridu migrate up --database-path /var/lib/ridu/content.sqlite
pnpm run ridu migrate status --database-path /var/lib/ridu/content.sqlite
```

```bash title="terminal" package-manager="yarn"
yarn run ridu migrate create --name initial
yarn run ridu migrate verify
yarn run ridu migrate plan --database-path /var/lib/ridu/content.sqlite
yarn run ridu migrate up --database-path /var/lib/ridu/content.sqlite
yarn run ridu migrate status --database-path /var/lib/ridu/content.sqlite
```

SQLite also supports reviewed `down`, `reset`, `refresh`, and `fresh` workflows with
`--allow-destructive`. Ridu rejects altered or reordered migration history; schema, data-transform,
plugin, and ledger work commits or rolls back together. Mutating migration commands reject
`:memory:` so a successful invocation always targets persistent state. `plan` and `status` inspect
existing files read-only and report a missing file as all pending without creating it.

Production readiness runs inside a stable SQLite read transaction. It requires the complete ordered
ledger to match the migration filename/digest fingerprint embedded by `ridu build`, then verifies
the head manifest and physical schema in that same snapshot. This rejects missing, altered,
renamed, reordered, or rolled-back data-only artifacts even when the final schema is unchanged.

SQLite stores canonical document values as JSON. A field rename or other stored-data rewrite must
therefore use a compiled transaction-bound transform registered by the project and selected with
`ridu migrate create --transform <name>`. The PostgreSQL-only `--accept-renames` shortcut is not a
substitute for that transform.

Localized values remain locale maps inside that canonical JSON. SQLite compiles localized filters
and sorting to JSON expressions and uses expression indexes or focused reference/uniqueness side
tables where needed; it does not use PostgreSQL's typed per-locale column layout.

## Back up and test recovery {#backup}

Use SQLite-aware online backup tooling. For a filesystem copy, stop all Ridu processes using the
file and quiesce writers first. Copying only the main file while WAL is active can omit committed
pages, and separate copies of the database and WAL do not form a complete recovery point.

Restore into an isolated local path with the exact binary and immutable migration history. Run
`ridu migrate status`, start the application through its normal readiness check, then exercise
login, representative reads and writes, relationships, versions, tasks, and upload reconciliation.
Do not rely on a backup until you have restored and tested it in isolation.

For projects with uploads, restore the SQLite database and object backend to one matched recovery
point. Database metadata without objects is incomplete, and objects without matching references can
retain stale data.
