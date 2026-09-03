---
title: 'MongoDB'
description: 'Create or configure a Ridu project for the supported MongoDB replica-set deployment.'
product: adapters
eyebrow: 'Database adapter'
order: 172
aliases: ['mongo', 'mongodb adapter', 'mongodb database']
capabilities: ['adapter.mongodb']
availability:
  status: limited
  label: 'Limited production support'
  description: 'Support is limited to the Linux, MongoDB, security, and topology profile below.'
  anchor: testing
navigation:
  section: 'Develop & operate'
  parent: 'adapters'
  order: 30
  title: 'MongoDB'
---

Choose MongoDB when you can run the supported replica-set topology below. Collections, access
rules, REST, the generated SDK, and the admin work the same way as they do with Ridu's other
database adapters; application code does not use MongoDB filters or driver types.

Production support is limited to:

- an ordinary Ridu-generated `starter` or `blank` project;
- Linux x86-64;
- MongoDB Community 8.2.9, with release evidence built from digest-pinned images;
- SCRAM-SHA-256 authentication and TLS with CA and hostname verification; and
- a writable three-member replica set.

This does not include Atlas, Amazon DocumentDB, Azure Cosmos DB, a standalone `mongod`, another
MongoDB version or topology, another operating system or architecture, arbitrary scale,
network-partition/failover matrices, or point-in-time recovery. See
[Releases and compatibility](/docs/releases/#supported-matrix) before choosing this profile.

## Create a project {#new-project}

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

Choose **Starter** and **MongoDB** in the wizard, then run the exact install and `dev` commands it
prints. Review the summary carefully: MongoDB is not the default database choice.

<details class="docs-disclosure">
<summary>Preselect every scaffold choice</summary>
<div class="docs-disclosure-body">

For a non-interactive npm setup, pass every choice as an argument:

```bash title="terminal"
npm create ridu@latest -- \
	--template starter \
	--database mongodb \
	--module github.com/acme/content \
	--scope @acme \
	--package-manager npm \
	--agent codex \
	content
```

</div>
</details>

Both generated templates include the same adapter wiring. The `dev` script starts the pinned local
single-node replica set from `compose.yaml` and uses:

```text
mongodb://127.0.0.1:27029/ridu?directConnection=true&replicaSet=ridu-rs0
```

This convenient plaintext, unauthenticated service is for local development only. It is still a
replica set because Ridu operations require MongoDB transactions; a standalone `mongod` is
rejected. To use another development replica set without putting its URL in process arguments,
supply a command-scoped environment value from your shell's secret binding:

```bash title="terminal" package-manager="npm"
DATABASE_URL="$MONGODB_DEVELOPMENT_URL" npm run dev -- --no-docker
```

```bash title="terminal" package-manager="bun"
DATABASE_URL="$MONGODB_DEVELOPMENT_URL" bun run dev -- --no-docker
```

```bash title="terminal" package-manager="pnpm"
DATABASE_URL="$MONGODB_DEVELOPMENT_URL" pnpm run dev --no-docker
```

```bash title="terminal" package-manager="yarn"
DATABASE_URL="$MONGODB_DEVELOPMENT_URL" yarn run dev --no-docker
```

Configuration discovery, `generate`, `generate --check`, and `migrate create` through the
project-local CLI remain offline. They do not open MongoDB or serialize its URL into config,
generated contracts, or migration artifacts.

## Add MongoDB to an existing project {#existing-project}

Use this path for a clean generated Ridu project before it has committed migration history or live
data. Ridu has no generic PostgreSQL/SQLite-to-MongoDB data migration facility. Moving a live
dataset is application-owned: export and transform every current document, localized value,
version, auth/session record, relationship, task, preference, lock, and upload reference; preserve
stable IDs; validate the destination; and rehearse backup, cutover, and rollback.

### 1. Select MongoDB in project metadata {#existing-project-config}

Replace the existing adapter value. This example starts from PostgreSQL:

```toml title="ridu.toml" remove={2} add={3}
version = 1
database = "postgres"
database = "mongodb"
entry = "./cmd/server"
admin = "./admin"
```

The value must match the CLI's public adapter name. It controls development orchestration and
migration routing; it does not contain a connection URL.

### 2. Use the generated adapter handshake {#existing-project-server}

MongoDB development needs more than replacing one `Open` call. The internal candidate started by
`ridu dev` must verify the resolved manifest's exact indexes before the stable proxy promotes it.
Copy the following generated pattern into `cmd/server/main.go` (retaining the project's admin asset
and handler options):

```go title="cmd/server/main.go" remove={10,26,34,38-42,77-78} add={3,11,15,19-22,27,35,43-62,79-80}
import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/acme/content/content"
	"github.com/acme/content/internal/adminassets"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/store"
)

const internalDevelopmentServerArgument = "--ridu-internal-development-server"

func main() {
	applicationConfig := content.Config()
	internalDevelopmentServer := len(os.Args) == 2 && os.Args[1] == internalDevelopmentServerArgument
	if internalDevelopmentServer {
		os.Args = os.Args[:1]
	}

	var options []ridu.ExecuteOption
	if len(os.Args) == 1 {
		options = runtimeOptions(applicationConfig)
		options = runtimeOptions(applicationConfig, internalDevelopmentServer)
	}
	if err := ridu.Execute(applicationConfig, options...); err != nil {
		log.Fatal(err)
	}
}

func runtimeOptions(applicationConfig ridu.Config) []ridu.ExecuteOption {
func runtimeOptions(applicationConfig ridu.Config, internalDevelopmentServer bool) []ridu.ExecuteOption {
	return []ridu.ExecuteOption{
		ridu.WithStore(func(ctx context.Context) (store.Store, error) {
			return postgres.OpenWithConfig(ctx, postgres.PoolConfig{
				DatabaseURL:              os.Getenv("DATABASE_URL"),
				AllowInsecureTransport:   envBool("RIDU_ALLOW_INSECURE_DATABASE"),
				MaxUploadLockConnections: envInt32("RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS"),
			})
			backend, err := mongodb.OpenWithConfig(ctx, mongodb.Config{
				DatabaseURL:            os.Getenv("DATABASE_URL"),
				AllowInsecureTransport: envBool("RIDU_ALLOW_INSECURE_DATABASE"),
				ApplicationName:        "ridu-server",
			})
			if err != nil {
				return nil, err
			}
			if internalDevelopmentServer {
				manifest, err := ridu.Resolve(applicationConfig)
				if err != nil {
					_ = backend.Close()
					return nil, fmt.Errorf("resolve MongoDB development manifest: %w", err)
				}
				if err := backend.VerifyIndexes(ctx, manifest); err != nil {
					_ = backend.Close()
					return nil, fmt.Errorf("verify MongoDB development indexes: %w", err)
				}
			}
			return backend, nil
		}),
		ridu.WithAddress(env("RIDU_ADDRESS", ":8080")),
		ridu.WithHandlerOptions(ridu.HandlerOptions{
			AdminAssets:             adminassets.FS(),
			AllowedOrigins:          envList("RIDU_ALLOWED_ORIGINS"),
			AllowedHosts:            envList("RIDU_ALLOWED_HOSTS"),
			TrustedProxyCIDRs:       envList("RIDU_TRUSTED_PROXY_CIDRS"),
			ReadinessTimeout:        envDuration("RIDU_READINESS_TIMEOUT"),
			StrictTransportSecurity: os.Getenv("RIDU_STRICT_TRANSPORT_SECURITY"),
		}),
		ridu.WithServerOptions(ridu.ServerOptions{
			ShutdownTimeout:            envDuration("RIDU_SHUTDOWN_TIMEOUT"),
			WorkerDrainTimeout:         envDuration("RIDU_WORKER_DRAIN_TIMEOUT"),
			ReadinessDrainDelay:        envDuration("RIDU_READINESS_DRAIN_DELAY"),
			AllowUnverifiableReadiness: envBool("RIDU_ALLOW_UNVERIFIABLE_READINESS"),
			SkipReadinessPreflight:     envBool("RIDU_SKIP_READINESS_PREFLIGHT"),
			AllowUnverifiableReadiness: internalDevelopmentServer,
			SkipReadinessPreflight:     internalDevelopmentServer,
		}),
	}
}
```

The red PostgreSQL lines are context from the existing generated server. Remove them after adding
the MongoDB lines; the finished file has one adapter import, one `runtimeOptions` call and
declaration, one store factory, and one value for each readiness option.

The readiness exceptions are **only** for the CLI's internal development candidate, after the
explicit index check. Do not expose them as production environment fallbacks. Keep the template's
`envBool` helper so invalid environment values fail clearly, and close the backend on every
resolution/verification error as shown.

### 3. Add the local replica-set service {#existing-project-compose}

Copy this service into `compose.yaml`:

```yaml title="compose.yaml"
services:
  mongodb:
    image: mongo:8.2.9-noble@sha256:007773db61cb1aa44e526fb7175fc582902e67d4e6cc5f13106445767d46c818
    command: ['mongod', '--replSet', 'ridu-rs0', '--bind_ip_all']
    ports:
      - '127.0.0.1:27029:27017'
    healthcheck:
      test:
        - CMD-SHELL
        - >-
          mongosh --quiet --eval 'try { const h=db.hello(); if (h.setName === "ridu-rs0" && h.isWritablePrimary) { quit(0) }; if (!h.setName) { try { rs.initiate({_id:"ridu-rs0",members:[{_id:0,host:"mongodb:27017"}]}) } catch (_) {} } } catch (_) {}; quit(1)'
      interval: 2s
      timeout: 5s
      retries: 30
      start_period: 5s
```

The health check initializes `ridu-rs0` and succeeds only when the node is its writable primary.
Ridu requires sessions and transactions, so a standalone MongoDB server is not sufficient. The
local URL is:

```text
mongodb://127.0.0.1:27029/ridu?directConnection=true&replicaSet=ridu-rs0
```

This single-node, unauthenticated, plaintext service is development-only and is not the
three-member authenticated TLS production profile.

### 4. Start and verify development {#existing-project-verify}

```bash title="terminal" package-manager="npm"
go mod tidy
npm run dev
```

```bash title="terminal" package-manager="bun"
go mod tidy
bun run dev
```

```bash title="terminal" package-manager="pnpm"
go mod tidy
pnpm run dev
```

```bash title="terminal" package-manager="yarn"
go mod tidy
yarn run dev
```

`ridu dev` starts the generated replica-set service, regenerates contracts, synchronizes safe
additive changes, and starts the API and admin. Before deployment, create and verify the immutable
migration history:

```bash title="terminal" package-manager="npm"
export DATABASE_URL='mongodb://127.0.0.1:27029/ridu?directConnection=true&replicaSet=ridu-rs0'
export RIDU_ALLOW_INSECURE_DATABASE=true
npm run ridu -- migrate create --name initial
npm run ridu -- migrate plan
npm run ridu -- migrate verify
npm run ridu -- migrate up
npm run ridu -- migrate status
```

```bash title="terminal" package-manager="bun"
export DATABASE_URL='mongodb://127.0.0.1:27029/ridu?directConnection=true&replicaSet=ridu-rs0'
export RIDU_ALLOW_INSECURE_DATABASE=true
bun run ridu -- migrate create --name initial
bun run ridu -- migrate plan
bun run ridu -- migrate verify
bun run ridu -- migrate up
bun run ridu -- migrate status
```

```bash title="terminal" package-manager="pnpm"
export DATABASE_URL='mongodb://127.0.0.1:27029/ridu?directConnection=true&replicaSet=ridu-rs0'
export RIDU_ALLOW_INSECURE_DATABASE=true
pnpm run ridu migrate create --name initial
pnpm run ridu migrate plan
pnpm run ridu migrate verify
pnpm run ridu migrate up
pnpm run ridu migrate status
```

```bash title="terminal" package-manager="yarn"
export DATABASE_URL='mongodb://127.0.0.1:27029/ridu?directConnection=true&replicaSet=ridu-rs0'
export RIDU_ALLOW_INSECURE_DATABASE=true
yarn run ridu migrate create --name initial
yarn run ridu migrate plan
yarn run ridu migrate verify
yarn run ridu migrate up
yarn run ridu migrate status
```

For a separately managed development replica set, omit the Compose service and rerun the selected
tab's `dev --no-docker` command with `DATABASE_URL="$MONGODB_DEVELOPMENT_URL"`. Keep TLS and
authentication enabled unless the target is a disposable local development database. Before
production, replace the local URL and insecure admission with the authenticated, CA- and
hostname-verified three-member profile below and follow the full release cutover.

## Development schema changes {#development}

During `ridu dev`, an additive manifest change creates only missing collections and indexes. A
separate serving Store then non-mutatingly verifies the exact physical index plan and passes
development readiness before the stable proxy promotes the candidate. A failed build, incompatible
index, unhealthy candidate, or rejected rename leaves the last working process in place.

Development synchronization never drops state or guesses how stored content should move. For a
rename, compiled transform, index replacement, or reviewed retirement, create and apply an
immutable production migration instead of repairing MongoDB by hand.

`--no-sync` skips additive mutation, but it does not weaken verification: the candidate starts only
when another owner has already prepared the selected database.

## Production connection and credentials {#production-connection}

The production `DATABASE_URL` must select the application database, authenticate with
SCRAM-SHA-256, name the replica set, and enable certificate and hostname verification. For example:

```text
mongodb://ridu-app:<secret>@mongo-1.example.internal,mongo-2.example.internal,mongo-3.example.internal/content?authSource=content&authMechanism=SCRAM-SHA-256&replicaSet=ridu-rs0&tls=true&tlsCAFile=/run/secrets/mongodb-ca.pem
```

Keep the running application credential scoped to its database. Use separate, short-lived
credentials for verification and backups:

| Credential   | Scope                                                                                                                     |
| ------------ | ------------------------------------------------------------------------------------------------------------------------- |
| Application  | Database-scoped access to the Ridu database. Use it for the running app and, when sufficient, `plan`, `up`, and `status`. |
| Verification | Permission to create and drop the temporary database used by `migrate verify`.                                            |
| Backup       | The database-scoped permissions required by `mongodump` and `mongorestore`.                                               |

Supply each credential only to its command. Ridu redacts credentials and topology details from normal
connection failures, but operators must still keep URLs out of shell history, process arguments,
logs, generated files, and image layers. A mode-`0600` Database Tools configuration file is one way
to keep backup credentials out of process arguments.

`RIDU_ALLOW_INSECURE_DATABASE` and `--allow-insecure-database` are for local development only.
They do not expand the production support profile.

## Immutable migration lifecycle {#migrations}

Artifacts created by `ridu migrate create` use MongoDB planner contract `2.0.0` inside shared
artifact-envelope format `1`.
Authenticated planner-`1.0.0` artifacts remain a supported immutable prefix: the runner validates
and replays that committed history before applying v2 artifacts rather than rewriting or rejecting
it. Create and inspect the plan before the cutover:

```bash title="terminal" package-manager="npm"
npm run ridu -- migrate create --name add-post-summary
npm run ridu -- migrate plan --json
```

```bash title="terminal" package-manager="bun"
bun run ridu -- migrate create --name add-post-summary
bun run ridu -- migrate plan --json
```

```bash title="terminal" package-manager="pnpm"
pnpm run ridu migrate create --name add-post-summary
pnpm run ridu migrate plan --json
```

```bash title="terminal" package-manager="yarn"
yarn run ridu migrate create --name add-post-summary
yarn run ridu migrate plan --json
```

Ridu binds each artifact to its manifest history and rejects altered or reordered migration files.
`up` takes a fenced lease with a bounded wait, records durable
step progress, and resumes the same immutable history after an interruption. It requires explicit
maintenance admission only when the pending or incomplete history suffix contains a rename,
transform, reference-index rebuild, or resource retirement. Run that work only after every old
application process and worker is drained.

`verify` creates a random isolated database, replays the complete history with the adapter's
migration runner, checks the final ledger and index state, and drops that database. Because the
temporary database starts empty, `verify` requires `--allow-maintenance` whenever any artifact in
the complete history contains semantic work, including work already applied to the live database.
`status` is non-mutating.
`ridu build` embeds a fingerprint of the exact ordered migration filenames and artifact digests.
Application readiness requires the live ledger to match that fingerprint, its head to match the
executable manifest, and every required Ridu index to pass non-mutating verification.
`ridu check` and `ridu build` remain offline and fail when committed history does not end at
executable config.
A direct `go build` has no fingerprint and fails closed in ordinary production startup. Deployment
cutover additionally fails when the applied ledger or physical indexes do not exactly match the
release history.

MongoDB does not expose `down`, `reset`, `refresh`, or `fresh`. Correct forward with another
reviewed artifact, or restore the complete matched recovery point.

## Deploy and recover {#production}

For a code-only replacement with the same manifest digest and migration-history fingerprint, start
the candidate, require `/readyz`, then drain the old process. For any new migration artifact—even an
additive or same-manifest data-only one—use the coordinated sequence:

1. Rehearse the exact binary and history against a restored recovery point.
2. Drain every old application process and worker.
3. Run `migrate verify` through the project-local CLI with
   `DATABASE_URL="$MONGODB_OPERATIONAL_URL"` so the shadow-database authority exists only for that
   command. Append `--allow-maintenance` whenever the complete committed history contains semantic
   work.
4. After verification succeeds, capture one recovery point containing a database-scoped
   `mongodump` and the upload store. Use a separately scoped `$MONGODB_BACKUP_URL`.
5. Run `migrate up` through the project-local CLI with `DATABASE_URL="$MONGODB_MIGRATION_URL"`, using
   the selected database-scoped application or controlled operator identity. Append
   `--allow-maintenance` only when the pending or incomplete history suffix contains semantic work.
6. Run `migrate status` with the same migration identity and require exact complete history and
   Ridu-managed index state.
7. Start only the target release binary carrying the expected manifest and migration-history
   fingerprints with `$MONGODB_APP_URL`, then wait for `/readyz` before admitting traffic.

Take the cutover recovery point only after draining writers and successfully completing the
command-scoped verification, but before `up`. The recovery drill uses database-scoped
`mongodump --archive --gzip --dumpDbUsersAndRoles`, restores that archive into an empty target with
`mongorestore --archive --gzip --drop --restoreDbUsersAndRoles`, and restores upload storage from
the same named point. The dump/restore must remain scoped to the selected application database;
never restore over unrelated databases. Confirm the ledger and `status`, the restored scoped user,
auth and sessions, content and versions, relationships, SDK/admin workflows, rich text, upload
objects, tasks, and representative data before directing traffic.

This is a logical dump/restore path, not point-in-time recovery. Define and rehearse your retention,
encryption, off-site storage, recovery-point objective, and recovery-time objective.

## Test the deployment {#testing}

Before production, rehearse migrations, startup, readiness, a primary election, graceful shutdown,
and database/upload restoration on the topology you will deploy. Exercise authentication, content,
versions, relationships, uploads, tasks, the SDK, and the admin after restoration. Use disposable
databases for these tests; never point them at production.
