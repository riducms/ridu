# Ridu versus Payload performance comparison

This benchmark compares the committed Ridu and Payload parity fixtures as production HTTP servers
connected to the same local PostgreSQL instance. It records process-tree RSS, startup readiness,
admin HTML response time, and selected-field collection CRUD latency and throughput.

The comparison intentionally excludes PostgreSQL RSS because both applications share the same
server. Migrations and deterministic fixture seeding happen before timed windows. Trials alternate
server order to reduce ordering and thermal bias, and every response must have the expected status.

## Prerequisites

- PostgreSQL available at `127.0.0.1:5432` for the current operating-system user.
- Bun, Node, and Go versions supported by this repository.
- The Payload fixture dependencies installed with `bun install --cwd tests/contracts/payload_server`.

## Build and run

From the repository root:

```sh
mkdir -p .ridu/performance
bun run build:admin-fixture
go build -trimpath -ldflags='-s -w' -o .ridu/performance/ridu-server ./tests/contracts/admin_server
PAYLOAD_DATABASE_URL="postgres://$USER@127.0.0.1:5432/payload_perf_build?sslmode=disable" \
  PAYLOAD_SECRET='ridu-performance-payload-secret' \
  RIDU_PAYLOAD_STANDALONE=true \
  NODE_ENV=production \
  bun run --cwd tests/contracts/payload_server build
bun tests/performance/compare.ts
```

The harness copies `.next/static` into the generated standalone deployment and launches its
`server.js`, so the measured Payload process matches the reported deployment artifact.

Raw JSON is written beneath `.ridu/performance/`. These environment variables control workload
size: `RIDU_PERF_TRIALS`, `RIDU_PERF_DATASET`, `RIDU_PERF_READ_REQUESTS`,
`RIDU_PERF_MUTATION_REQUESTS`, `RIDU_PERF_ADMIN_REQUESTS`, and `RIDU_PERF_CONCURRENCY`.
Mutation concurrency defaults to eight because the pinned Payload fixture stalls during versioned
updates at concurrency 16; override it with `RIDU_PERF_MUTATION_CONCURRENCY` when testing that
boundary. Every request has a configurable `RIDU_PERF_REQUEST_TIMEOUT_MS` deadline.
Set `RIDU_PERF_POSTGRES_URL_TEMPLATE` when PostgreSQL is elsewhere; it must contain a `{database}`
placeholder and defaults to the current user on `127.0.0.1:5432`.

The committed Payload fixture has one intentional asymmetry: it also includes the Payload-only
localized global and capability collection used by the parity audit. Results must disclose that
extra initialized schema surface instead of presenting the configuration as byte-for-byte equal.

## Production stress benchmark

`stress.ts` is the longer, failure-tolerant companion to `compare.ts`. It uses the exact existing
`.ridu/performance/ridu-server` binary, application-specific `.ridu/admin-fixture-build`, and Payload
`.next/standalone/server.js`; it never rebuilds those artifacts. It records its own source SHA-256,
both server artifact hashes and sizes, both admin deployment trees, allowlisted benchmark tuning
variables, host/tool versions, Git revision and dirty state, and the effective workload
configuration. Requiring the fixture admin prevents the multilingual/plugin-bearing application
from being paired accidentally with Ridu's deliberately generic embedded fallback bundle.
The Payload static directory is replaced with a fresh copy of `.next/static` before hashing and
launching the standalone server. The standalone fixture's generated `media/` upload directory is
also cleared first so deterministic seed uploads cannot contaminate the immutable deployment hash
across runs.

The default run creates 10,000 additional published posts for each framework. Every generated post
contains author, category, collaborator, and related-post relationships whose IDs are discovered
through that framework's authenticated HTTP API. Dataset creation validates the echoed scalar
fields and exact to-one IDs, requires the expected to-many IDs to be present, remains outside the
timed windows, and is followed by admin, public, and contributor-visible count validation.

```sh
bun tests/performance/stress.ts
```

The harness destructively recreates only these two allowlisted databases:
`ridu_stress_benchmark` and `payload_stress_benchmark`. It refuses any other destructive database
target. Ridu always uses ports 18181/18182 and the fixed
`ridu_admin_fixture_stress` PostgreSQL schema; Payload always uses port 3010. Override the connection
with `RIDU_STRESS_POSTGRES_URL_TEMPLATE` (or the comparison harness's
`RIDU_PERF_POSTGRES_URL_TEMPLATE` fallback). The template must contain `{database}`.

The default timed program is:

- closed-loop mixed list/find read saturation for 10 seconds at concurrency 1, 8, 32, and 128;
- 1,000 matched requests each for selected list, selected find, populated find, and contributor
  access-filtered list reads;
- 250 matched creates, updates, and deletes of versioned drafts;
- 10 seconds of repeated versioned draft updates at concurrency 1, 4, 8, 16, and 32, with one
  document owned by each worker;
- a 120-second mixed point/list/update soak at concurrency 8, again with worker-owned drafts.

Every timed request retains its status or network error, timeout classification, response byte
count, latency, and JSON-shape result. Documents returned by selected reads require every requested
field; selected lists also require the expected total but do not assert a non-empty page. Populated
reads require the exact expected to-one documents and the expected to-many documents to be present,
and updates must echo the submitted summary. Each workload reports throughput, status/error counts,
and p50/p90/p95/p99/p99.9/max latency. Process-tree RSS and CPU are sampled at roughly 10 Hz;
PostgreSQL session states and wait counts are sampled at roughly 1 Hz. The raw time series and
min/median/p95/max summaries are included for every workload, plus pre-load idle, post-load idle,
and recovery windows. Health checks and document-visibility count reconciliation run between phases
where relevant; this reconciliation is not a field/version checksum. Expected request failures do
not abort later stress phases.

The main controls are:

- `RIDU_STRESS_FRAMEWORKS=ridu`, `payload`, or `ridu,payload`;
- `RIDU_STRESS_DATASET`, `RIDU_STRESS_SEED_CONCURRENCY`;
- `RIDU_STRESS_READ_CONCURRENCIES`, `RIDU_STRESS_WRITE_CONCURRENCIES`;
- `RIDU_STRESS_DURATION_SECONDS`, `RIDU_STRESS_SOAK_SECONDS`,
  `RIDU_STRESS_SOAK_CONCURRENCY`;
- `RIDU_STRESS_MATCHED_READ_REQUESTS`, `RIDU_STRESS_MATCHED_MUTATION_REQUESTS`,
  `RIDU_STRESS_MATCHED_CONCURRENCY`;
- `RIDU_STRESS_REQUEST_TIMEOUT_MS`, `RIDU_STRESS_IDLE_SECONDS`,
  `RIDU_STRESS_RECOVERY_SECONDS`;
- `RIDU_STRESS_FAILURE_BUDGET`, which defaults to 64 and stops assigning new requests at that many
  failures while preserving every in-flight failure sample so a wedged server cannot make a run
  unbounded;
- `RIDU_STRESS_HOLD_SECONDS`, which keeps each server alive after its run for manual browser
  inspection.

The Payload fixture accepts `PAYLOAD_POOL_MAX` as an optional positive-integer PostgreSQL pool-size
override. Leave it unset for Payload's adapter default; use it only for explicitly labelled
capacity diagnostics, not for the product-default comparison.

Timestamped raw JSON and the latest copy are written to `.ridu/performance/stress-*.json` and
`.ridu/performance/latest-stress.json`. A safety ceiling of one million request samples per workload
prevents an already-dead server from filling memory with immediate connection failures; adjust it
with `RIDU_STRESS_MAX_SAMPLES`.

## Optional GraphQL memory

Two smaller binaries isolate the cost of importing and enabling GraphQL against the same synthetic
39-collection, 28-block schema:

```sh
mkdir -p .ridu/performance
go build -trimpath -ldflags='-s -w' -o .ridu/performance/graphql-without ./tests/performance/graphql_without
go build -trimpath -ldflags='-s -w' -o .ridu/performance/graphql-with ./tests/performance/graphql_with

.ridu/performance/graphql-without
RIDU_GRAPHQL_REQUESTS=0 .ridu/performance/graphql-with
RIDU_GRAPHQL_REQUESTS=50 .ridu/performance/graphql-with
RIDU_GRAPHQL_REQUESTS=5000 .ridu/performance/graphql-with
```

Each process forces GC and prints live heap, live object, and runtime system-memory values as JSON.
Use `/usr/bin/time -l` around a command to record peak RSS on macOS. Verify linker exclusion with:

```sh
go list -deps ./tests/performance/graphql_without | rg 'graphql-go|plugins/graphql'
```

No output is the expected baseline result.
