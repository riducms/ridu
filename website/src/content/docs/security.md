---
title: 'Security model'
description: 'Secure HTTP, authentication, uploads, databases, plugins, and operational access.'
product: core
eyebrow: 'Ship'
order: 240
navigation:
  section: 'Develop & operate'
  order: 70
  title: 'Security model'
---

Ridu's central security rule is simple: authoring UI state is never authority. REST, GraphQL, the
generated SDK, the admin, local operations, hooks, and durable tasks converge on server-owned
schema, access, validation, transaction, and redaction contracts.

The table below separates Ridu's controls from the security work owned by your application and
infrastructure.

## Trust boundaries {#trust-boundaries}

Ridu protects content and version history, credentials and sessions, private uploads, migration
identity, task data, preview capabilities, and generated application contracts.

1. An untrusted browser or API client reaches the Go HTTP server through deployment-owned TLS and
   optional trusted proxies.
2. The server authenticates, authorizes, validates, runs hooks, and applies field redaction before
   one official store transaction observes or mutates content.
3. The selected database and object storage are separate durable systems. Staged-object rollback, locks,
   last-reference scans, reconciliation, and matched backups bridge the fact that they cannot share
   one atomic commit.
4. The Svelte admin and static admin plugins execute in the browser. They improve authoring but
   cannot grant server access.
5. Go config, hooks, validators, task handlers, and backend plugins execute inside the trusted
   server process. Installing one is equivalent to installing application code; Ridu does not
   sandbox it.

Operators, application authors, and compiled plugins can control the process and its credentials.
Treat authors, clients, compromised browser state, uploaded bytes, remote URLs, headers, filters,
GraphQL documents, generated-client input, and stored application content as untrusted.

## Framework and deployment responsibilities {#responsibilities}

| Surface                  | Ridu enforces                                                                                                                                                                                | You must provide                                                                                                                                            |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| REST and GraphQL         | Body/query/token/member/depth bounds, stable errors, panic redaction, atomic access predicates, field redaction, optimistic conflicts, and bounded population/complexity.                    | Independent edge and abuse limits for public high-volume APIs, plus monitoring of rejection and error rates.                                                |
| Sessions and credentials | Bounded bcrypt admission, generic auth failures, distributed login/recovery/verification throttles, rotation/revocation, HTTP-only secure/SameSite cookies, CSRF and origin checks.          | Signing-secret protection, HTTPS, allowed origins, and application-specific password and identity policy.                                                   |
| Hosts and proxies        | Explicit allowed hosts/origins, trusted-proxy CIDRs, CORS, CSP, security headers, and optional HSTS.                                                                                         | Only the public hosts you serve and immediate proxy networks; forwarding-header stripping; HSTS only behind permanent HTTPS.                                |
| Uploads                  | Size and MIME admission, name and path normalization, namespace ownership, image work bounds, SSRF-safe remote fetches, private delivery checks, object locks, rollback, and reconciliation. | Malware scanning/quarantine where required, a production storage backend, and matched database/object backups.                                              |
| PostgreSQL               | TLS required by default, bounded pools and waits, atomic predicates, row/advisory locks, immutable migration ledger, destructive/maintenance preflight, and manifest-aware readiness.        | Least-privilege roles, verified TLS where possible, managed backup/PITR, restored-data rehearsals, and physical-drift checks with `ridu migrate status`.    |
| SQLite                   | Pure-Go local-file store, WAL and foreign-key enforcement, bounded waits, atomic predicates, immutable migration ledger, reversible lifecycle atomicity, and manifest/physical readiness.    | One host, a local filesystem, small/local workloads, WAL-safe backup/recovery drills, and no shared network filesystem or horizontally replicated topology. |
| MongoDB                  | Verified TLS by default, writable replica-set admission, bounded pools/lease waits, exact immutable ledger/index readiness, atomic predicates, credential redaction, and crash-safe resume.  | The supported version, topology, and platform; SCRAM-SHA-256; CA and hostname verification; least-privilege operational credentials; and matched drills.    |
| Durable tasks            | Compiled handler registry, strict typed input, leases, heartbeat fencing, retry/backoff, concurrency keys, cancellation, retention bounds, and graceful drain.                               | Idempotent external effects and monitoring of retries and terminal failures.                                                                                |
| Admin and plugins        | Embedded CSP-compatible admin, exact manifest/plugin pairing, server-owned permission checks, and origin/source/channel-bound preview messaging.                                             | Dependency review; treat frontend and compiled plugins as shipped trusted code; never expose dev servers publicly.                                          |
| Supply chain             | Version compatibility checks and generated-contract drift detection.                                                                                                                         | Pin and review dependencies; protect the repository, registry, secrets, and deployment identity.                                                            |

## Configure the HTTP boundary {#http-boundary}

Generated applications expose the relevant policy through `HandlerOptions`:

```go title="cmd/server/main.go"
ridu.WithHandlerOptions(ridu.HandlerOptions{
	AdminAssets:             adminassets.FS(),
	SecureCookies:           true,
	AllowedOrigins:          []string{"https://admin.example.com"},
	AllowedHosts:            []string{"cms.example.com"},
	TrustedProxyCIDRs:       []string{"10.0.0.0/8"},
	// 8 × 2²⁰ = 8,388,608 bytes (8 MiB)
	MaxBodyBytes:            8 << 20,
	RequestTimeout:          20 * time.Second,
	StrictTransportSecurity: "max-age=31536000; includeSubDomains",
})
```

List trusted proxies narrowly. Forwarding headers are accepted only from configured networks; an
over-broad CIDR lets an untrusted hop influence scheme or client-address decisions. Enable HSTS
only when HTTPS is permanent for its complete declared scope.

The browser SDK includes credentials by default. Keep origins exact, cookies secure in production,
and custom request headers explicitly allowed. Follow [CORS](/docs/cors/) for same-origin rules,
generated-project configuration, preflights, and proxy diagnosis; see
[Authentication](/docs/authentication/) for session and account flows.

## Authorization is server work {#authorization}

Admin visibility, disabled buttons, generated TypeScript types, object keys, and signed URLs are not
authorization. Collection and field rules run inside the operation engine, and filtered decisions
remain part of the atomic store query for reads, updates, and deletes.

An authenticated principal is the pair `{auth collection, document ID}`. Do not assume document IDs
are globally unique across auth collections. REST, GraphQL, local calls, hooks, preview, locks,
uploads, scheduled work, and audit events preserve both values. A nil actor is anonymous, not an
administrator.

Private upload delivery checks collection access before returning bytes. Treat an object key or
signed URL as a capability and avoid logging it casually, but never use possession as the content
access policy.

## Bounded work {#bounded-work}

Ridu rejects expensive or ambiguous work before it becomes an unbounded database or serialization
problem. Important framework ceilings include:

| Bound                                                         | Current ceiling | Why it exists                                                                                |
| ------------------------------------------------------------- | --------------: | -------------------------------------------------------------------------------------------- |
| Relationship and upload occurrences in one document mutation  |             512 | Bounds recursively nested reference maps and store admission across effective locales.       |
| Validation issues returned for one operation                  |             128 | Keeps adversarial invalid input from creating an unbounded response.                         |
| Recursive population depth                                    |               5 | Prevents unrestricted graph traversal.                                                       |
| Materialized related-document nodes in one populated response |           4,096 | Charges complete subtrees so dense cycles cannot grow exponentially below the depth ceiling. |
| Durable task input or output                                  |      1 MiB each | Keeps database-backed work records bounded; large data belongs in object storage.            |

Application schemas and edge infrastructure may impose tighter limits. Public deployments still
need tenant-, user-, route-, and abuse-aware rate policy: Ridu's distributed auth-operation
throttling is not a universal API quota.

## Migrations, readiness, and recovery {#operations}

Production startup never mutates schema. Apply only committed [migration artifacts](/docs/migrations/)
with the release's exact binary. For any artifact requiring `--allow-maintenance`, stop all old
processes and workers and keep them stopped through completion and retries.

For MongoDB, keep the running application credential scoped to one application database and use
that or a selected database operator for `plan`, `status`, and `up`. Use separate least-privilege,
short-lived credentials for verification and backups. Keep every URL out of
generated files, image layers, logs, shell history, and process arguments; never grant cluster-wide
administration to the long-running application merely to simplify operations.

`/healthz` reports whether the process is alive. `/readyz` aggregates bounded checks for the migration
ledger/manifest, database, and upload dependency; MongoDB readiness also verifies the exact
Ridu-managed index plan. `ridu migrate status` remains the explicit operator report for immutable
history, durable step progress, and managed database state; readiness does not replace it.
Remove an instance from traffic before graceful drain.

Back up the selected database and object storage as one recovery point. Restoring only the database
can leave missing objects; restoring only objects can expose stale or orphaned bytes. Test every
backup with an isolated restore.

MongoDB's supported cutover order is drain, command-scoped verification with the operational URL,
a matched database/upload snapshot, `up` with the selected application/operator URL, post-`up`
`status`, then application start with the app URL. This workflow covers a database-scoped logical
dump/restore plus uploads, not point-in-time recovery or an unsupported managed provider.

## Residual risks and non-features {#residual-risks}

These boundaries require an application or infrastructure design:

- Auth operations are throttled, but general distributed API quotas are not built in.
- Audit callbacks are structured but are not a framework-owned tamper-aware durable audit log.
- After-commit callbacks can enqueue tasks, but no atomic database outbox closes the narrow crash
  window between the content commit and callback dispatch.
- Preview capabilities are process-local; multi-replica preview currently needs process affinity.
- Malware quarantine, resumable/direct uploads, and sandboxing of untrusted plugins are not
  provided.
- Prometheus and OpenTelemetry adapters are application-owned; Ridu exposes observations and
  readiness without requiring a vendor SDK.

Use reconciliation or an application-owned transactional outbox when an external effect cannot
tolerate the after-commit window. Treat every plugin as reviewed application code, even when its
configuration comes from a manifest.

Start with [Prevent abuse](/docs/preventing-abuse/) before exposing the application publicly. See
[Access control](/docs/access-control/), [Uploads](/docs/uploads/),
[Durable tasks](/docs/tasks/), [Production](/docs/production/), and
[Releases and compatibility](/docs/releases/) for the contracts around this model.
