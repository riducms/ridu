# Ridu/PocketBase SQLite benchmark

This diagnostic compares Ridu with PocketBase 0.40.4 as production HTTP servers on SQLite. Both
are pure-Go single binaries, so unlike the Payload comparison it measures framework work rather
than a Go-versus-Node runtime difference. It is not part of any check gate and has no pass/fail
threshold.

```sh
bun tests/performance/pocketbase/run.ts
```

The first run downloads PocketBase and its dependencies through the Go module proxy, verified
against [`server/go.sum`](./server/go.sum). Builds, logs, data directories and raw JSON live in
`.ridu/performance/pocketbase/`; `latest.json` is the newest report.

## What is compared

[`ridu/`](./ridu/main.go) is a Ridu application in this module that links only the SQLite adapter.
[`server/`](./server/main.go) is an ordinary PocketBase Go application in its own module, so
PocketBase never enters Ridu's dependency graph. Each side keeps its framework's default settings,
including its own pinned `modernc.org/sqlite` driver. Both are built with the same Go toolchain,
`CGO_ENABLED=0`, `-trimpath` and `-ldflags=-s -w`.

| Concern        | Ridu                                                     | PocketBase                                                  |
| -------------- | -------------------------------------------------------- | ----------------------------------------------------------- |
| Content        | `posts`: title, summary, status select, number, checkbox | Same fields plus `created`/`updated` autodate fields        |
| Read access    | Anonymous: `Where(status = published)`; signed in: all   | `status = "published" \|\| @request.auth.id != ""`          |
| Write access   | Any signed-in user                                       | `@request.auth.id != ""`                                    |
| Index          | `status` field index, keyed with `id`                    | `(status, id)` index                                        |
| Schema setup   | `migrate` applies a migration artifact before startup    | `migrate up` applies the Go migration before startup        |
| Startup check  | Verifies the applied migration history digest            | Applies pending migrations (none remain)                    |
| Benchmark user | Created through `create-user` before timing              | Created by the migration, with a superuser                  |
| Authentication | Session cookie looked up in SQLite                       | JWT `Authorization` header plus an auth-record read         |
| SQLite         | WAL, `synchronous=full`, 8 pooled connections            | WAL, `synchronous=normal`, 32 MB page cache, 120 read conns |

The superuser exists so PocketBase's first `serve` never opens its browser installer.

### Commit durability

`pocketbase-full` is PocketBase with one setting changed: a `modernc.org/sqlite` connection hook
sets `PRAGMA synchronous = FULL` on every `data.db` connection after PocketBase's own DSN pragmas.
Its request-log `auxiliary.db` keeps PocketBase's default. Ridu's adapter always uses `FULL`, so
comparing `pocketbase` with `pocketbase-full` isolates how much of a write difference comes from
waiting for a sync at every commit. Each PocketBase trial reads back its pragmas through the
fixture's benchmark-only `/__bench/sqlite` route and records them in the report.

On macOS, SQLite's sync does not force the drive's write cache to flush unless `fullfsync` is on,
and neither framework enables it. The cost of `FULL` is therefore smaller here than on a Linux
server with a real `fsync`.

## Workload

Every trial starts from an empty data directory, applies migrations in a separate step, then
times startup to the first successful anonymous list. After login it seeds 1,000 posts through
the API, every tenth a draft, and checks the anonymous and signed-in totals. Timed loads:

- `list`: anonymous, ten selected posts ordered by `id`, with the total count;
- `find`: anonymous selected point reads of published posts;
- `list-authenticated`: the same list with the user's credential, unfiltered;
- `admin-html`: Ridu's `/admin/login` and PocketBase's `/_/` static dashboard entry;
- `create`, `update`, `delete`: authenticated writes of new posts, each update echoing its
  summary.

Every timed response must have the expected status and shape: lists return ten documents and the
exact expected total, finds return the requested ID, and creates and updates echo their values.
The counts are checked again after the writes. Trials rotate the framework order.

Each load reports throughput; p50, p95, p99 and maximum latency; response bytes, measured on the
decoded body, so in gzip mode they are the uncompressed size rather than what crossed the wire;
content encodings; peak RSS; client CPU; and server CPU time divided by requests. The load generator is
one Bun process. A load where its CPU reaches 90% or more is marked as client-bound with `*` in
the printed summary. For those loads, throughput and latency reflect the client, so compare server
CPU per request instead.

Requests send `Accept-Encoding: identity` by default. Ridu gzips GET bodies over 1 KiB for clients
that accept gzip, and PocketBase does not compress API responses. Set
`RIDU_PB_ACCEPT_ENCODING=gzip` to measure that default browser behaviour instead.

## Controls

- `RIDU_PB_FRAMEWORKS`: any of `ridu,pocketbase,pocketbase-full` (the default);
- `RIDU_PB_TRIALS` (3), `RIDU_PB_DATASET` (1,000);
- `RIDU_PB_READ_REQUESTS` (5,000), `RIDU_PB_MUTATION_REQUESTS` (1,000),
  `RIDU_PB_ADMIN_REQUESTS` (300);
- `RIDU_PB_CONCURRENCY` (16), `RIDU_PB_MUTATION_CONCURRENCY` (8);
- `RIDU_PB_REQUEST_TIMEOUT_MS` (30,000), `RIDU_PB_ACCEPT_ENCODING` (`identity`).

Ridu uses ports 18281 and PocketBase 18290. The report records the host, toolchain, Git revision
and dirty state, harness and fixture hashes, both binaries' hashes and SQLite driver versions, and
inherited `GOGC`, `GOMEMLIMIT`, `GOMAXPROCS` and `GODEBUG`.

## Limits

- The schemas and rules match observable behavior, not storage. Ridu stores every collection's
  documents as JSON in shared tables with expression indexes; PocketBase gives each collection a
  table with typed columns.
- The response envelopes differ. Ridu's selected documents also carry `createdAt` and
  `updatedAt`, and its list pagination has more fields, so its read responses are larger. Ridu's
  delete returns 200 with the deleted document; PocketBase's returns 204 with no body.
- The admin entries are different pages, so `admin-html` compares static serving, not equal work.
- PocketBase records every request in `auxiliary.db` by default; Ridu keeps no request log.
- Each framework links its own pinned `modernc.org/sqlite` and `modernc.org/libc`, which may
  embed different SQLite releases.
- Neither framework analyzes the seeded tables, so both use SQLite's default query planning.
- This is one local machine with loopback networking and a single-process client, not a hardware
  matrix or a confidence study. Other desktop processes are not stopped.
