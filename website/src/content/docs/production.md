---
title: 'Production'
description: 'Build, configure, deploy, observe, drain, migrate, and recover a Ridu application.'
product: core
eyebrow: 'Ship'
order: 220
navigation:
  section: 'Develop & operate'
  order: 60
  title: 'Production'
---

Ridu deploys as one Go binary containing the API, operation engine, workers, generated schema, and
static admin. PostgreSQL and MongoDB remain external durable dependencies; SQLite runs in the
process against a local file. An optional object backend remains separate. Bun and Node are
build-time tools, not production servers.

> [!IMPORTANT]
> SQLite supports one application host, a local filesystem, and small or local
> workloads. Same-host processes can coordinate through SQLite's file locks, but horizontally
> replicated Ridu deployments are not promised. The replica, rolling-deployment, and PostgreSQL
> recovery guidance below does not widen that envelope. See [SQLite](/docs/sqlite/) for its WAL-safe
> deployment and recovery contract.

> [!IMPORTANT]
> MongoDB production support is limited to generated starter and blank projects on Linux x86-64,
> Community 8.2.9, SCRAM-SHA-256, CA and hostname verified TLS, and a writable
> three-member replica set. It does not cover Atlas, DocumentDB, Cosmos DB, standalone servers,
> other versions/topologies/platforms, arbitrary scale, network-partition matrices, or PITR. See
> [MongoDB](/docs/mongodb/) for the exact boundary.

## Deployment checklist {#checklist}

1. Pin one coordinated Ridu release across the CLI, Go module, npm packages, and official plugins.
2. Run `ridu check` and application tests against committed generated contracts.
3. Create and review any schema migration; rehearse `ridu migrate verify` with the exact release in
   staging. MongoDB's final cutover verification runs command-scoped after the production drain.
4. Restore a recent selected-database and object-storage backup into staging and rehearse the plan at
   representative scale.
5. Build the production binary and immutable deployment image.
6. Follow the [abuse-prevention checklist](/docs/preventing-abuse/) for access rules, authentication
   limits, hosts, origins, proxies, request bounds, uploads, and edge rate limiting.
7. For any release with a changed migration-history fingerprint—including a manifest change or a
   same-manifest data-only artifact—drain old PostgreSQL or MongoDB replicas and workers, or quiesce
   every same-host process using the SQLite file; follow the adapter's exact migration and
   recovery-point order below.
8. Start the new binary and wait for `/readyz` before sending traffic.
9. Exercise login, access-filtered reads, writes, drafts, uploads, tasks, and representative admin
   flows.
10. Keep monitoring and the complete prior recovery point through the rollback window.

## Build one runtime artifact {#build}

```sh title="terminal"
npm run ridu -- check
npm run ridu -- migrate verify
npm run ridu -- build
```

`ridu build` resolves config, updates generated contracts, compiles the Svelte admin, embeds its
assets and the exact ordered migration filename/digest fingerprint, and writes the Go binary to
`dist/`. Build with the supported Go and Node.js versions listed in
[Releases and compatibility](/docs/releases/), the package manager recorded in `ridu.toml`, and the
committed lockfiles. The runtime image needs the binary, certificates, and application environment;
it does not need source,
`node_modules`, Bun, or Node. A direct `go build` omits the fingerprint and therefore fails closed
in ordinary production startup with an official database adapter.

Run a PostgreSQL- or MongoDB-selected project with the exact migration history used during
verification:

```sh title="terminal"
DATABASE_URL="$DATABASE_URL" \
RIDU_ADDRESS=':8080' \
./dist/content
```

A SQLite-selected project instead supplies an absolute local `RIDU_SQLITE_PATH`, keeps every
process using that file on the same host, and does not horizontally replicate the deployment.

Ridu never mutates the production schema at startup. The process performs readiness preflight
before binding the listener and fails if the store, storage, complete applied migration history,
executable manifest, or required maintenance contract cannot be verified. PostgreSQL, SQLite, and
MongoDB all compare the live ordered ledger with the fingerprint embedded by `ridu build`.

## Configure the generated boundary {#environment}

Generated projects expose the deployment-owned topology as environment:

| Environment                             | Production use                                                                                                                                                                                                    |
| --------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `DATABASE_URL`                          | PostgreSQL URL using `sslmode=require`, `verify-ca`, or preferably `verify-full`; or a MongoDB URL selecting one database, SCRAM-SHA-256, replica set, and verified TLS. Plaintext and TLS fallback are rejected. |
| `RIDU_SQLITE_PATH`                      | Absolute local SQLite file path or `file:` URI. Do not use a shared network filesystem or mount the file on multiple hosts.                                                                                       |
| `RIDU_ADDRESS`                          | Listener address, default `:8080`. Put TLS at the platform edge unless the application deliberately supplies it elsewhere.                                                                                        |
| `RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS` | Additional bounded pool for cross-process object locks; default 4. Count it alongside document connections across all replicas.                                                                                   |
| `RIDU_ALLOWED_HOSTS`                    | Comma-separated public hosts, optionally with ports. Set it because an empty list accepts any syntactically valid host for development compatibility.                                                             |
| `RIDU_ALLOWED_ORIGINS`                  | Exact browser origins allowed to make [cross-origin requests](/docs/cors/). Same-origin embedded admin traffic needs no entry.                                                                                    |
| `RIDU_TRUSTED_PROXY_CIDRS`              | Immediate proxy networks allowed to supply forwarded address and scheme. Leave empty when directly exposed.                                                                                                       |
| `RIDU_STRICT_TRANSPORT_SECURITY`        | HSTS header value. Set only when HTTPS is permanent for the complete declared scope.                                                                                                                              |
| `RIDU_READINESS_TIMEOUT`                | Bound for combined database, object-storage, and custom readiness checks.                                                                                                                                         |
| `RIDU_READINESS_DRAIN_DELAY`            | Time between becoming unready and beginning request shutdown. Unset or zero uses two seconds; a negative duration disables this propagation delay without disabling graceful shutdown.                            |
| `RIDU_SHUTDOWN_TIMEOUT`                 | Bound for HTTP shutdown and runtime-resource cleanup.                                                                                                                                                             |
| `RIDU_WORKER_DRAIN_TIMEOUT`             | Bound for cooperative task and maintenance-worker drain.                                                                                                                                                          |

Disable the readiness-propagation delay on platforms that atomically remove the old deployment
from routing before sending its termination signal. For Kubernetes or a self-managed load
balancer, choose it from measured endpoint/routing propagation and include it in the platform
termination grace period; two seconds is a default, not a universal safe value. On Railway, where
traffic cutover precedes termination, set `RIDU_READINESS_DRAIN_DELAY=-1s` and use the platform's
separate drain window for in-flight work.

`RIDU_ALLOW_INSECURE_DATABASE` is for an explicitly local database. `RIDU_ALLOW_UNVERIFIABLE_READINESS`
is for a custom adapter only after equivalent external checks and auth maintenance exist. Neither is
a remedy for a failing production dependency.

Secrets belong in the platform secret store, not generated files or image layers. Rotate database,
storage, signing, and external-provider credentials through an application-owned procedure that
keeps readiness and rollback viable.

For MongoDB, keep the running application on a database-scoped credential and use that or a selected
database operator for `plan`, `status`, and `up`. Use separately scoped, short-lived credentials for
verification and backups. Never make the application a cluster administrator to simplify
operations.

## Harden HTTP and proxies {#http}

The generated environment covers common topology. `HandlerOptions` and `ServerOptions` expose finer
bounds:

```go title="cmd/server/main.go"
ridu.WithHandlerOptions(ridu.HandlerOptions{
	AdminAssets:           adminassets.FS(),
	SecureCookies:         true,
	AllowedHosts:          []string{"cms.example.com"},
	AllowedOrigins:        []string{"https://app.example.com"},
	AllowedRequestHeaders: []string{"X-Acme-Tenant"},
	TrustedProxyCIDRs:     []string{"10.0.0.0/8"},
	MaxBodyBytes:          8 << 20, // 8 × 2²⁰ = 8,388,608 bytes (8 MiB)
	RequestTimeout:        20 * time.Second,
	ReadinessTimeout:      5 * time.Second,
	ContentSecurityPolicy: "default-src 'self'; object-src 'none'",
	Observe:               observeRequest,
	Audit:                 recordAuditEvent,
	RequestError:          reportTrustedError,
	JobError:              reportWorkerError,
})

ridu.WithServerOptions(ridu.ServerOptions{
	ReadHeaderTimeout:   5 * time.Second,
	ReadTimeout:         30 * time.Second,
	WriteTimeout:        30 * time.Second,
	IdleTimeout:         60 * time.Second,
	ShutdownTimeout:     15 * time.Second,
	WorkerDrainTimeout:  15 * time.Second,
	ReadinessDrainDelay: 5 * time.Second,
})
```

The framework supplies conservative defaults; customize them from measured traffic. A negative
request timeout is an explicit streaming opt-out and must be paired with endpoint-owned bounds.

TLS termination must replace untrusted forwarding headers before traffic reaches a configured
trusted proxy network. Keep CIDRs to immediate controlled hops. Secure cookies are enabled by the
production executor unless `RIDU_SECURE_COOKIES=false`; never disable them behind a public HTTPS
origin. Configure CORS only for intentional browser clients and list custom request headers
explicitly.

The default admin CSP is conservative and preview-compatible. Override it narrowly; disabling CSP
because a plugin needs broad execution hides a supply-chain problem rather than fixing one. Use
[Prevent abuse](/docs/preventing-abuse/) for the public-traffic checklist and
[Security](/docs/security/) for the full trust boundary and request-work ceilings.

## Apply migrations as a deployment step {#migrations}

Use the exact target binary and committed artifacts for planning and rehearsal:

```sh title="terminal"
npm run ridu -- migrate plan --json
npm run ridu -- migrate verify
```

Shadow replay checks migration history, not production data volume or lock timing. Rehearse against a
recent restored backup, set migration timeouts below the deployment deadline, and stop old writers
for every maintenance-classified artifact.

PostgreSQL and MongoDB permit rolling overlap only for binaries embedding both the same manifest
digest and the same migration-history fingerprint. For a MongoDB release with any new artifact—even
an additive or same-manifest data-only one—the production cutover order is exact:
drain old replicas and workers; run `verify` with its command-scoped operational URL; capture the
matched database/upload snapshot; run `up` with the selected database-scoped application or
operator URL; run `status` after `up`; then start the new binary with the application URL. SQLite
does not promise horizontally replicated rollout overlap; quiesce every same-host process using the
file for the migration cutover.

Rollback mechanics are a separate concern from deployment overlap. PostgreSQL and MongoDB have no
generated down migration, while SQLite exposes only the explicit reversible steps stored in its
artifacts. A down migration can revert state, but it does not make incompatible application versions
safe to run together; Ridu uses forward repair or complete recovery-point restoration instead.

[Migrations](/docs/migrations/) documents create/plan/status/verify/up, resumable boundaries,
destructive and maintenance admission, stable safety codes, and immutable artifact validation.

## Liveness, readiness, and drain {#health}

| Signal                | Meaning                                                                                                                                                                                                                                                         | Use                                                                            |
| --------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `/healthz`            | The process is alive.                                                                                                                                                                                                                                           | Restart policy. It does not confirm a matching schema or reachable dependency. |
| `/readyz`             | The process is not draining and its migration ledger/manifest, document store, upload backend, and custom checks pass within the readiness timeout; PostgreSQL also verifies the expected physical schema, and MongoDB verifies the exact Ridu-managed indexes. | Load-balancer traffic admission.                                               |
| `ridu migrate status` | Immutable ledger, phase/step completion, and managed database schema agree with committed history.                                                                                                                                                              | Explicit operator deployment/drift report; readiness does not replace it.      |

On `SIGTERM` or interrupt, Ridu marks the instance draining so readiness fails, waits the configured
drain delay, performs bounded HTTP shutdown, cancels request contexts, drains cooperative workers,
and closes runtime resources within a separate bound. If a task ignores context past the worker bound, its
lease remains fenced and recovers after expiry.

Set the platform termination grace period longer than readiness delay + longest admitted request +
worker drain + resource-close budgets. Remove traffic on readiness before the platform sends a
hard kill.

## Observe the application {#observability}

Ridu keeps telemetry vendor-neutral:

- `RequestObservation` reports request ID, method/path, status, response bytes, duration, and stable
  error code.
- `AuditEvent` reports security-relevant actions with request ID, resolved client IP, action,
  collection/document target, and actor collection/document identity.
- `RequestError` receives trusted internal causes and recovered-panic detail while the HTTP response
  remains redacted.
- `JobError` receives worker and scheduled-operation infrastructure failures; handler retry state is
  also queryable through [durable tasks](/docs/tasks/).
- custom `ReadinessChecks` add required application or plugin dependencies without coupling Ridu to
  a metrics vendor.

The diagnostic callback is trusted and must enforce its own redaction and access policy. Audit
callbacks are structured observations, not a tamper-aware durable audit log.

At minimum, alert on readiness failures, login throttle/recovery anomalies, migration and physical
drift, exhausted database pools, repeated task retries or terminal failures, upload reconciliation
deltas, elevated conflicts, and sustained latency or error-rate changes. Track RSS and connection
use per replica; the [performance baseline](/docs/performance/measurement/#headline) is not capacity planning for your
schema.

## Back up and test restoration {#backup}

Back up the selected database and object storage as one recovery point. Retain the binary, configuration,
generated manifest, and immutable migration history needed to interpret it. Database metadata
without objects is incomplete; objects without matching references can retain stale data.

Regularly restore into an empty isolated environment and verify:

- migration ledger, `ridu migrate status`, and zero physical drift;
- collection, global, version, task, and auth-state counts;
- login, session rotation, access-filtered reads, update conflicts, and private upload delivery;
- representative object hashes and image variants;
- task claiming, scheduled publishing, and cleanup/reconciliation; and
- measured recovery-point and recovery-time objectives.

For PostgreSQL, logical dumps complement rather than replace managed point-in-time recovery. For
SQLite, use an online backup or stop all processes using the file and quiesce WAL writers before a
filesystem copy; never accept a copy of only the live main file. Restore the selected database and
object storage to the same named point before accepting the drill.

For MongoDB, drain writers, complete command-scoped `verify`, then create a
database-scoped compressed archive with `mongodump --archive --gzip --dumpDbUsersAndRoles` before
running `up`; pair it with the upload-storage backup from the same point.
Restore into an empty target with
`mongorestore --archive --gzip --drop --restoreDbUsersAndRoles`, restore uploads from the same named
point, and confirm that unrelated databases were untouched. This is a logical dump/restore
workflow, not point-in-time recovery.

## Stage, cut over, and roll back {#cutover}

For a PostgreSQL or MongoDB code release with the same manifest and migration-history
fingerprints, canary beside the previous binary and compare errors, p50/p95 latency, RSS, database
connections, response checksums, tasks, private uploads, drafts, and conflicts. Shift traffic only
after the replacement stays ready.

For a MongoDB release with any new migration artifact:

1. Rehearse the exact sequence in staging, then drain every old process and worker in production.
2. Run `DATABASE_URL="$MONGODB_OPERATIONAL_URL" ridu migrate verify`. Append
   `--allow-maintenance` whenever the complete committed history contains semantic work, because
   clean-shadow verification replays every artifact.
3. After verification, capture the matched database/upload snapshot with a separately scoped backup
   identity.
4. Run `DATABASE_URL="$MONGODB_MIGRATION_URL" ridu migrate up`, where the selected URL is the
   database-scoped application or controlled operator identity. Append `--allow-maintenance` only
   when the pending or incomplete history suffix contains semantic work.
5. Run `DATABASE_URL="$MONGODB_MIGRATION_URL" ridu migrate status` and require exact complete
   history and managed indexes.
6. Start only the target release binary carrying the expected manifest and migration-history
   fingerprints with `$MONGODB_APP_URL`, wait for readiness, and run acceptance checks.
7. Prefer a forward corrective migration. Restore only when the entire database/object pair and
   compatible binary can return to the same point.

For PostgreSQL, use its documented drain, recovery-point, `up`, and post-apply `status` contract;
SQLite requires every same-host process to be quiesced. Retry an immutable artifact after fixing an
operational blocker rather than editing it.

Use expand–migrate–verify–contract across separate reviewed releases for large transformations,
while keeping each migration-history boundary on the documented coordinated cutover. A
dual-manifest admission contract would be an optional deployment enhancement for teams with a
strict zero-downtime requirement, not a prerequisite for safe production use.

Review [Releases and compatibility](/docs/releases/) for supported platforms and upgrade policy,
[PostgreSQL](/docs/postgres/) for pools and transactions, [SQLite](/docs/sqlite/) for the embedded
operating envelope, [MongoDB](/docs/mongodb/) for its bounded replica-set profile, and
[Object storage](/docs/storage/) for backend and reconciliation requirements.
