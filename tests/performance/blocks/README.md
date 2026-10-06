# Block-heavy Ridu/Payload benchmark

This diagnostic compares Ridu with Payload 3.87.0 on layout-builder schemas: many blocks that
reference each other by slug through a config-level registry, used by drafts-enabled collections.
It exists because Payload users report block-heavy applications idling at 1–4 GB
([payloadcms/payload#17214](https://github.com/payloadcms/payload/issues/17214)). It is not part of
any check gate and has no pass/fail threshold.

## What is compared

[`spec.ts`](./spec.ts) generates one framework-neutral JSON spec per scenario and variant. The Ridu
fixture ([`ridu/`](./ridu/)) builds `ridu.Config` from it and the Payload fixture
([`payload/`](./payload/)) builds its `buildConfig` input from it, so both serve the same fields,
blocks, collections, drafts, access and relationships. Both read the spec at startup, so a single
production build of each framework serves every scenario.

| Scenario  | Shape                                                                                                          |
| --------- | -------------------------------------------------------------------------------------------------------------- |
| `A`       | The #17214 reproduction's 44-block graph, 3 drafts-enabled layout collections                                  |
| `A-full`  | The reproduction's own config: 6 layout collections, `pages` with two layout fields (143,035 placements)       |
| `B`       | 39 collections, 28 blocks (20 leaves, 5 item containers, 3 wrappers), 34 drafts-enabled layout collections     |
| `B-dense` | 39 collections, 28 blocks in a denser layered DAG (containers reference every lower block): 254,354 placements |
| `C`       | 18 leaves and six container layers, one layout collection (96,955 placements)                                  |
| `C-over`  | `C`'s registry used by three layout collections (290,867 placements)                                           |

Scenario `A` is a port of
[evelynhathaway/payload@evelyn/block-schema-memory-repro](https://github.com/evelynhathaway/payload/tree/evelyn/block-schema-memory-repro/test/_community).
Its slugs are kebab-case because Ridu requires them, its Lexical `richText` field is a textarea on
both sides and `media` is an ordinary collection. `B` and `C` use leaf blocks with text, textarea,
select, checkbox, number, date, relationship and nested array fields. Each scenario has a
`references` variant (config registry plus slugs) and an `inline` variant that declares the same
blocks inline, reusing one definition value per block with an explicit `TypeName`/`interfaceName`.

Ridu resolves, stores and generates each block definition once, so an `inline` variant produces the
same manifest as its `references` variant and every scenario's cost follows its definitions rather
than its placements. `C` was sized as the largest graph of its family that Ridu's former
100,000-placement budget accepted; the `*-full`, `*-dense` and `*-over` scenarios exceeded it and are
now ordinary scenarios for both frameworks.

A third variant, `hooks`, runs for **Ridu only**; Payload is not rerun for it, and its Payload
probe, generation and trials are skipped and recorded as such. It is the `references` variant
whose leaf blocks (blocks without nested blocks) give their first text field a `BeforeChange`
hook, a validator, `Read` and `Update` access rules, a dynamic default and a visibility condition
on the automatic `blockName` sibling. Globals place leaf blocks too, and global fields initialize
through update access, so the variant declares no create rule. None of the callbacks changes the
workload's content: its text never has surrounding space or the rejected placeholder, every leaf
field is supplied, and the rules allow the benchmark's anonymous reads and authenticated writes.
Ridu binds such fields once per block definition, so the variant measures executable bindings
against the same placements as `references`.

Two fixture adaptations are deliberate and disclosed in results:

- Payload's PostgreSQL adapter rejects table and enum names longer than 63 characters, so every
  block gets a short `dbName`, as a Payload application with these layouts must.
- Ridu date fields use `field.DateTime`, matching Payload's stored instants.

## Prerequisites

- Docker. The runner creates and removes its own `postgres:17-alpine` container and the
  repository's MongoDB replica-set fixture (`adapters/mongodb/testdata/compose.yaml`) under the
  Compose project `ridu-blocks-bench`. It never connects to another database.
- `psql` on the host, Go, Bun and Node versions supported by the repository.
- The Payload fixture dependencies:

```sh
bun install --cwd tests/performance/blocks/payload --frozen-lockfile
```

## Run

From the repository root, without other gates or benchmarks running:

```sh
bun tests/performance/blocks/run.ts
```

The runner:

1. writes every spec to `.ridu/performance/blocks/specs/`;
2. builds four Ridu binaries (PostgreSQL-only, MongoDB-only, each with and without the GraphQL
   plugin) and a clean standalone Payload build, recording wall time;
3. probes each scenario without a database: Ridu resolution time and allocation, manifest,
   `/api/schema` and generated contract sizes, App construction time and allocation, and retained
   heap; Payload's MongoDB adapter `mongoose.Schema` instance count, init time and retained heap
   (Payload's PostgreSQL adapter builds its schema only on connect, so it has no offline probe);
4. generates schema-dependent artifacts: Ridu migration artifacts for each database, Payload
   `migrate:create` for PostgreSQL and `generate:types`;
5. for every accepted scenario × variant × database runs trials that alternate framework order.
   Each trial recreates its database and prepares it in a separate process: Ridu applies its
   migration artifacts, Payload runs `payload migrate` on PostgreSQL, and on MongoDB, which has no
   Payload migrate step, one preparatory Payload start lets Mongoose create its collections and
   finish building indexes (writes during that build fail with `LockTimeout`). The production
   server then starts and the runner measures startup to the first successful read (a list of the
   small `media` collection on both sides), idle RSS, the cold admin login page, the live heap after
   a forced GC, seeding, RSS with documents, the authenticated admin edit page for a block-heavy
   document, then timed loads. A load stops at its first failed request, records the error and is
   skipped in later trials of the same combination;
6. compares GraphQL disabled and enabled for scenario `A` on PostgreSQL.

RSS is the server process tree, sampled with `ps` exactly as `tests/performance/compare.ts` does;
database containers are excluded. Each document has 50 top-level blocks nested up to four levels
(about 230–400 block instances, 80–100 KB of JSON). The timed loads are:

| Load           | Request                                                                    | Concurrency |
| -------------- | -------------------------------------------------------------------------- | ----------: |
| `find`         | Anonymous read of a published document, all fields, no population          |           8 |
| `list-select`  | Anonymous list of 10 published documents selecting `title` and the layout  |           8 |
| `create-draft` | Authenticated draft create of a new block-heavy document                   |           4 |
| `update-draft` | Authenticated draft update sending the full layout with existing block IDs |           4 |
| `hot-target`   | Opt-in, Ridu only: edits of shared media while page saves reference them   |       4 + 1 |

Every response must have the expected status. The first ten responses of each load and every fifth
after that are also compared with the submitted content: both sides are projected onto the
declared fields, dropping block and row identities, so equal projections mean equivalent content.
Any difference aborts the trial with the first differing position.

### Hot-target contention

`hot-target` measures an edit of a document that many concurrent saves reference, such as an image
used by every page. Every relationship field of scenarios `B` and `C` targets `media`, and each
generated layout references nine or ten of the ten seeded media documents, so a Ridu page save
holds a reference (share) lock on them while it commits, and an edit needs the exclusive lock.
MongoDB has no row locks and Ridu builds them from fence writes (see
[the adapter](../../../adapters/mongodb/README.md)), so this load shows whether an edit waits its
turn or keeps losing to new references.

For `BLOCKS_HOT_TARGET_SECONDS`, `BLOCKS_MUTATION_CONCURRENCY` workers each create a draft page
with a fresh layout and then update it, repeatedly, while one separate client edits the media
documents' `alt` text in round-robin order, `BLOCKS_HOT_TARGET_PAUSE_MS` apart. Scenario `A` has no
relationship fields, so its edits are uncontended and serve as the baseline. Neither side stops at
a failed request: the load records `hot-target-save` and `hot-target-edit` separately, with every
request's latency (failed requests included), failures by status and timeouts beyond
`BLOCKS_REQUEST_TIMEOUT_MS`. Edit responses must return the submitted `alt`; saves are validated
like `create-draft` and `update-draft`. Payload is not run for this load. With
`BLOCKS_HOT_TARGET_PROCESSES=2`, a second Ridu process serves the same database during the load:
the save workers alternate between the two processes and the editor uses the first, as replicas
behind a load balancer would.

The summary's hot-target table marks a MongoDB scenario as starving when an edit failed or timed
out, any trial's edit p99 exceeded one second, or its median edit p95 exceeded five times
PostgreSQL's for the same scenario. The load is opt-in, so name it in `BLOCKS_LOADS`. To run it
alone, or after the other loads so their numbers can be compared:

```sh
BLOCKS_FRAMEWORKS=ridu BLOCKS_SCENARIOS=A,B,C BLOCKS_VARIANTS=references \
  BLOCKS_GRAPHQL_SCENARIO=none BLOCKS_LOADS=hot-target bun tests/performance/blocks/run.ts
BLOCKS_FRAMEWORKS=ridu BLOCKS_SCENARIOS=A,B,C BLOCKS_VARIANTS=references BLOCKS_GRAPHQL_SCENARIO=none \
  BLOCKS_LOADS=find,list-select,create-draft,update-draft,hot-target bun tests/performance/blocks/run.ts
```

On the 6 October 2026 reference host, before the MongoDB adapter queued its own lock requests and
kept fence records across updates, edits in `B` and `C` starved: a median p95 of 2.0–2.3 s, p99
of 4.0–4.8 s and about 20 times PostgreSQL's p95, while 1.8% of the concurrent saves failed with
`409`. With both, edit p95 is 68–76 ms (PostgreSQL: 98–112 ms), no trial's edit p99 exceeds
100 ms, saves reach 96–108 ms p95 and no edit or save fails. The queue orders one process only:
with the saves split across two processes (`BLOCKS_HOT_TARGET_PROCESSES=2`), edit p95 is
155–163 ms, but the median p99 is 0.65–0.83 s (1.1 s in one trial) and 0.3–0.5% of saves fail
with `409`.

## Configuration

| Variable                      | Default                   | Meaning                                                     |
| ----------------------------- | ------------------------- | ----------------------------------------------------------- |
| `BLOCKS_SCENARIOS`            | all six                   | Scenarios run for both frameworks                           |
| `BLOCKS_VARIANTS`             | `references,inline,hooks` | Block declaration variants; `hooks` runs for Ridu only      |
| `BLOCKS_DATABASES`            | `postgres,mongodb`        | Store adapters                                              |
| `BLOCKS_FRAMEWORKS`           | `ridu,payload`            | Frameworks; a Ridu-only run never builds or runs Payload    |
| `BLOCKS_TRIALS`               | `3`                       | Alternating trials per combination                          |
| `BLOCKS_SEED_DOCUMENTS`       | `20`                      | Published documents read by `find` and `list-select`        |
| `BLOCKS_FIND_REQUESTS`        | `400`                     | `find` requests                                             |
| `BLOCKS_LIST_REQUESTS`        | `100`                     | `list-select` requests                                      |
| `BLOCKS_CREATE_REQUESTS`      | `100`                     | `create-draft` and `update-draft` requests                  |
| `BLOCKS_READ_CONCURRENCY`     | `8`                       | Read concurrency                                            |
| `BLOCKS_MUTATION_CONCURRENCY` | `4`                       | Write concurrency                                           |
| `BLOCKS_VALIDATE_EVERY`       | `5`                       | Content validation interval after the first ten responses   |
| `BLOCKS_LOADS`                | all but `hot-target`      | Timed loads; `update-draft` needs `create-draft`            |
| `BLOCKS_HOT_TARGET_SECONDS`   | `20`                      | Duration of the `hot-target` load                           |
| `BLOCKS_HOT_TARGET_PAUSE_MS`  | `0`                       | Pause between `hot-target` edits                            |
| `BLOCKS_HOT_TARGET_PROCESSES` | `1`                       | `2` splits `hot-target` saves across two Ridu processes     |
| `BLOCKS_REQUEST_TIMEOUT_MS`   | `120000`                  | Per-request deadline                                        |
| `BLOCKS_STARTUP_TIMEOUT_MS`   | `300000`                  | Startup deadline                                            |
| `BLOCKS_GRAPHQL_SCENARIO`     | `A`                       | GraphQL comparison scenario, or `none`                      |
| `BLOCKS_SKIP_PAYLOAD_BUILD`   | unset                     | `true` reuses the existing `.next` standalone build         |
| `BLOCKS_KEEP_DATABASES`       | unset                     | `true` leaves the containers running                        |
| `BLOCKS_PROFILE`              | unset                     | `true` CPU-profiles every Ridu load in trial 1 (diagnostic) |

Results are written to `.ridu/performance/blocks/runs/<timestamp>/results.json` and copied to
`.ridu/performance/blocks/latest.json`, with server logs, migration artifacts, generated Payload
types, probe output and Ridu heap profiles (`pprof/`) beside them. Summarize a run with:

```sh
bun tests/performance/blocks/summarize.ts .ridu/performance/blocks/latest.json
```

The Ridu fixture adds benchmark-only routes beside the application handler: `/__bench/memory`
(forced GC, then live heap), `/__bench/heap` (heap profile), `POST /__bench/free-os-memory` and the
`/__bench/cpu/start|stop` pair used by `BLOCKS_PROFILE`. The Payload fixture exposes the same
`/api/__bench/memory` measurement as a root endpoint.

## Inspect a result

Ridu heap profiles are taken after a forced GC at idle and after the loads for trial 1 of each
`references` scenario. Inspect retained and allocated memory with:

```sh
go tool pprof -sample_index=inuse_space -top .ridu/performance/blocks/bin/ridu-postgres <profile>
go tool pprof -sample_index=alloc_space -top .ridu/performance/blocks/bin/ridu-postgres <profile>
```

The probe directory contains a heap profile of resolution plus App construction; its
`alloc_space` view attributes configuration-time allocation. Re-run one probe directly with:

```sh
RIDU_BLOCKS_SPEC=.ridu/performance/blocks/specs/A-references.json \
  .ridu/performance/blocks/bin/ridu-postgres probe .ridu/performance/blocks/probe-a
```

To count Payload's Mongoose schemas for a spec, as the #17214 reproduction's `measure-schemas.ts`
does:

```sh
BLOCKS_SPEC=$PWD/.ridu/performance/blocks/specs/A-references.json NODE_ENV=production \
  PAYLOAD_DATABASE_URL=mongodb://127.0.0.1:1/probe \
  node --expose-gc tests/performance/blocks/payload/src/probe.ts
```

## Migration planning

[`planning/`](./planning/) measures whether migration planning follows block definitions rather
than placements. For each scenario it applies four changes to the spec: a text field added to every
block, the first optional text field of the most-placed block that has one renamed, the same field
made required, and a new block with a text field and a relationship added to the most-placed
container. For each adapter it then measures:

- `create`: `ridu migrate create --accept-renames` without the project handshake: reading the
  history, rename detection and the adapter planner, from an initial artifact of the before schema;
- `dev`: one `ridu dev` reload against a disposable database synchronized to the before schema:
  the field-kind review, rename detection, the rename migration and its application when the change
  is a rename, and schema synchronization.

Each step runs in a fresh child process, so its peak RSS covers reading the manifest and planning
or synchronizing one change. Results are printed and written to `<out>/results.json`; set
`RIDU_PLANNING_PROFILE=<directory>` for CPU and allocation profiles of every step.

```sh
bun -e 'import { buildSpec, scenarioNames } from "./tests/performance/blocks/spec.ts"; for (const scenario of scenarioNames) await Bun.write(`.ridu/performance/blocks/specs/${scenario}-references.json`, JSON.stringify(buildSpec(scenario, true)));'
go build -o .ridu/performance/blocks/bin/planning ./tests/performance/blocks/planning
.ridu/performance/blocks/bin/planning -specs .ridu/performance/blocks/specs \
  -out .ridu/performance/blocks/planning -postgres "$POSTGRES_URL" -mongodb "$MONGODB_URL"
```

`-scenarios`, `-changes`, `-adapters` and `-modes` take comma-separated subsets; the defaults are
`A,B-dense,C-over`, all four changes, all three adapters and both modes. Without `-postgres` or
`-mongodb` the development step of that adapter is skipped. The URLs must name disposable
databases, such as a `postgres:17-alpine` container and the repository's MongoDB replica-set
fixture; the tool creates and drops its own schemas and databases in them.

## Limits

This is one host with both servers and both database containers sharing CPU. It measures
server-side work only: no browser rendering, rich-text editors or uploads. The block graphs are
synthetic but follow real layout builders. Payload's GraphQL route is enabled by default and Ridu's
GraphQL plugin is not; the GraphQL comparison isolates that difference.
