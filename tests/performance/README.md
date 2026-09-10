# Framework scale and browser performance checks

Run `make performance-check` without another repository gate or benchmark running. It builds the
required runtime packages, then executes the hook, ordinary-operation and population benchmarks,
the generated-definition scale/compilation matrix and the browser performance case sequentially.
Browser output lives in `.ridu/playwright/performance/` and the fixture keeps isolated data and
OS-assigned ports.

The normal gates retain small exact-behavior tests and repeated-definition compiler contracts.
Large 100/1,000/5,000-node hook workloads and the 1/10/50 definition compile matrix are explicit here.
Go benchmarks report time and allocations; they do not by themselves fail on a performance
regression. Generated growth tests assert structural bounds and real consumer compilation. The
browser performance case enforces its typing/insert/reorder thresholds with one worker; ordinary
E2E continues to prove nested editors mount lazily and retain their values.

For a focused CPU profile:

```sh
mkdir -p .ridu/performance
go test -run '^$' -bench '^BenchmarkEmbeddedHookBatch$' -benchtime=1x -benchmem \
  -cpuprofile .ridu/performance/embedded-hooks.cpu.pprof \
  -o .ridu/performance/core.test ./core
go tool pprof .ridu/performance/core.test .ridu/performance/embedded-hooks.cpu.pprof
```

Field-hook execution indexes each binding's admitted occurrences once. A nonstructural replacement
updates its own cached value and sibling view while immediately publishing its immutable root
replacement. Exact-locale sibling views are reprojected instead, preserving null omission and
nested localization. Row preparation still follows the first callback and every replacement that can
contain rows; preparation and structural changes rebuild the lookup while retaining the original
dispatch order. Read-only embedded discovery and validation share the transforming walker's
admission logic without constructing discarded replacement containers.

The original `BenchmarkEmbeddedHookBatch` workload remains unchanged. The companion
`BenchmarkEmbeddedValueScaling` uses the same create, schema and node payload, comparing Keep and
Replace at 1, 10, 100, 1,000 and 5,000 nodes. `BenchmarkNativeValueScaling` shares those cases
using native blocks with the same logical fields. Both `None` matrices and their 100-node
`ReadAll` cases run in `make performance-check`; larger callback variants run separately:

- `None`: callbacks keep or replace titles without reading the document.
- `Retain`: retain every immutable root view through the create operation.
- `ReadRoot`: look up the root's body in every callback, without traversing its list.
- `ReadAll`: read every title in every callback through immutable `Get` and `Elements`.
- `Materialize`: explicitly copy the list and each containing object before reading the titles,
  recreating the old nested-container copying as a control; root lookup uses the new cheap API.
- `Serialize`: include JSON serialization of the returned document in the timed operation.

Application construction and fixture input construction are outside the timer, as in the original
benchmark. Create, validation, hook dispatch, row preparation, authorization, the test store and the
returned document are inside. Retention and callback reads are timed; retained views remain live
through operation completion. Allocated bytes are cumulative, not peak live memory.

`store.Value` lists use a canonical immutable 32-way tree. Item replacement copies one leaf and
its ancestor pointer arrays, sharing other branches with earlier callback snapshots. Small lists
use one leaf. Tree shape depends on length, so construction, JSON decoding and editing preserve
structural equality for equal values. `Get`, `Lookup`, `Entries` and `Elements` share immutable
children without materializing containers. `CopyObject`, `CopyList` and `CopyDocument` explicitly
request detached mutable output; they replace the former copying accessor names. `ListItem` and
`WithListItem` provide immutable indexed access and replacement without exposing backing storage.
`runtimePatch` uses them to publish each individual change immediately. Multi-item patches, such
as batched dynamic defaults, retain one bulk rebuild to preserve linear batch work. JSON and population traverse list
branches directly; there is no deferred patch chain, mutable cache or later full-list consolidation.

For comparable local samples, build before each measurement and run without competing jobs:

```sh
mkdir -p .ridu/performance
go test -c -o .ridu/performance/core.test ./core
.ridu/performance/core.test -test.run '^$' \
  -test.bench '^BenchmarkEmbeddedValueScaling$/^None$' \
  -test.benchtime=1x -test.count=5 -test.benchmem
.ridu/performance/core.test -test.run '^$' \
  -test.bench '^BenchmarkEmbeddedValueScaling$/^(Retain|ReadRoot|ReadAll|Materialize|Serialize)$' \
  -test.benchtime=1x -test.count=5 -test.benchmem
.ridu/performance/core.test -test.run '^$' \
  -test.bench '^BenchmarkEmbeddedValueScaling$/^None$/^(1|10)$' \
  -test.benchtime=30x -test.count=5 -test.benchmem
```

For a profile of the actual create operation, use the compiled binary with
`-test.bench '^BenchmarkEmbeddedValueScaling$/^None$/^5000$/^Replace$'`,
`-test.benchtime=3x`, `-test.cpuprofile` and `-test.memprofile`. Inspect both CPU and
`go tool pprof -alloc_space` / `-alloc_objects` output. Profile Keep and the callback variants
separately; profiling overhead belongs outside the timing comparison.

Keep the benchmark workload and host/Go/GC settings identical for before/after comparisons. Report
all samples or their range alongside medians. Timing thresholds do not belong in correctness tests.
Regression coverage instead asserts snapshot immutability, equality/serialization, callback order,
identity preparation, validation and persisted results.

For a fixed-depth document, replacing each of N list items now costs O(N log₃₂ N) list path work,
instead of O(N²) copied values. Immutable list traversal is O(N) without a copied slice;
`CopyList()` also allocates O(N) output. A callback that scans every item for each of N hooks
still requests N² reads, and explicit copies add corresponding allocation work. Structural replacement hooks and
changed exact-locale occurrences still require reindexing, and exact-locale root projection adds
work per callback. Those paths are not covered by the scalar replacement scaling claim. Repeated
whole-document engine passes also retain a substantial linear allocation cost.

See [hooks and immutable values](https://riducms.com/docs/performance/hooks-and-values/) for
the supported cost model and [measurement](https://riducms.com/docs/performance/measurement/)
for profiling guidance and the limits of benchmark comparisons.

`BenchmarkOrdinaryValueScaling` measures native and embedded documents with no field callbacks.
Its five actions are a root scalar update, one nested title update, one localized edit, an insertion
with reorder, and a populated read with one relationship target. The 100-row cases run in
`make performance-check`; the complete matrix also includes 10, 1,000 and 5,000 rows. Every timed
operation starts with a fresh, already persisted fixture, prepared outside the timer. Nested edits
submit a complete identity list with sparse payloads. `InsertReorder` starts with N−1 rows and ends
with N rows so the 5,000-row case stays inside the existing combined embedded traversal budget.

The separate `BenchmarkPopulationTraversal` matrix runs at 100, 1,000 and 5,000 rows in the
performance gate. It measures observation, mapping and populated-document redaction traversal on
the same response-shaped fixture, with and without populated references. It does not measure
database access. These benchmarks report time,
B/op and allocs/op; their behavioral fixture tests contain no timing thresholds.

## Ridu versus Payload performance comparison

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

The locale-view benchmarks measure bounded operation-scoped projection sharing,
including ordinary updates, fallback/all-locales controls, callback snapshots, serialization and
separate live-memory probes. The 100-row locale controls and serialization cases run serially in
`make performance-check`; memory probes remain opt-in diagnostics.
