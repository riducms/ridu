---
title: 'Measure performance'
description: 'Benchmark a representative Ridu application and interpret latency, throughput, memory, allocation, and bundle results.'
product: core
eyebrow: 'Performance'
order: 234
aliases:
  [
    'performance benchmark',
    'load test',
    'memory benchmark',
    'bundle budget',
    'blocks benchmark'
  ]
navigation:
  section: 'Develop & operate'
  parent: performance
  order: 40
  title: 'Measure performance'
---

Measure the application you plan to deploy: its schema, restored data, access rules, hooks,
relationships, locales, uploads, and admin extensions. A small framework fixture is useful for
regression work, but it is not a capacity plan for another application.

## Choose a representative workload {#workload}

Include the operations that dominate real traffic and the largest documents authors actually edit.
Warm up servers before timed samples, keep data and query shapes fixed between comparisons, and
repeat trials in alternating order when comparing two implementations.

| Measurement     | Record                                                                    | Why                                                                                      |
| --------------- | ------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| Request latency | p50, p95, p99, failures, and timeout count                                | A mean hides slow requests and failed work.                                              |
| Throughput      | Successful requests per second at stated concurrency                      | Throughput without concurrency and error rate is ambiguous.                              |
| Process memory  | Idle RSS, steady-state RSS, and workload peak                             | Go allocation totals are not the same as live process memory.                            |
| Go allocation   | B/op and allocs/op for focused engine or callback benchmarks              | Shows copied bytes and object churn even when the garbage collector later releases them. |
| Database        | Query latency, pool waits, active connections, lock waits, and slow plans | Separates application processing from storage and topology problems.                     |
| Response        | JSON bytes and returned/populated document counts                         | Explains work that grows with output shape.                                              |
| Admin browser   | JS/CSS transfer, largest async chunk, interaction time, and long tasks    | Server HTML time alone does not describe author experience.                              |
| Deployment      | Built binary and required asset size                                      | Describes distribution footprint, not runtime memory.                                    |

## Use the repository performance gate {#repository-gate}

Ridu keeps focused Go, generated-project, browser, and comparison workloads under
`tests/performance/`. From the repository root:

```sh title="terminal"
make performance-check
```

The gate reports behavior and measurements without turning desktop timing noise into a brittle unit
test. For a focused CPU profile, compile the relevant benchmark once and pass Go's `-cpuprofile` or
`-memprofile` flag as described in `tests/performance/README.md`. Keep the Go version, operating
system, CPU, garbage-collector settings, data size, and benchmark action identical for a before and
after comparison.

| Repository workload         | What it isolates                                                                 | What it does not prove                                                 |
| --------------------------- | -------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| Embedded value scaling      | Hooks and replacements over large arrays, Blocks, and embedded plugin trees      | Universal request latency or a production data distribution.           |
| Immutable read API          | Repeated `View`/`Value` reads with and without explicit container copies         | Database or network performance.                                       |
| Ordinary engine copying     | Updates, localization, population, and traversal without application callbacks   | The cost of custom hooks or external services.                         |
| Locale-view reuse           | Repeated projections inside one operation and their retained memory              | A cross-request cache or every locale workload.                        |
| Ridu/Payload HTTP fixture   | Equivalent selected CRUD, process RSS, admin HTML, and artifact size on one host | Identical wire formats, every feature, or a hosted capacity guarantee. |
| Ridu/Payload blocks fixture | Startup, memory, admin and CRUD for block graphs up to 290,867 field placements  | Rich-text blocks, large collections, or browser editing performance.   |

## Understand the current reference result {#headline}

The 5 October 2026 reference run compared Ridu with Payload 3.87.0 on the same Apple M2 Max and
local PostgreSQL 16.12 server. Runtime measurements are medians across three alternating trials:

| Runtime state                   |  Ridu RSS | Payload RSS | Ridu reduction |
| ------------------------------- | --------: | ----------: | -------------: |
| Idle, PostgreSQL connected      | 44.55 MiB |  202.14 MiB |          78.0% |
| After first admin HTML response | 45.03 MiB |  233.81 MiB |          80.7% |
| After 250 additional documents  | 50.44 MiB |  316.75 MiB |          84.1% |
| Highest median workload peak    | 69.66 MiB |  559.09 MiB |          87.5% |

The timed program performed selected-field lists and finds, authenticated versioned draft creates,
updates and deletes, and admin HTML requests. It completed 43,200 timed requests without an
unexpected status. PostgreSQL memory was excluded because both applications shared it.

Each application ran the way it is deployed. Ridu was a PostgreSQL-only build, like a generated
PostgreSQL application, and both applications applied their migrations in a separate step before
the timed server started. After seeding, both databases were analyzed, so the timings reflect
steady-state query plans.

> [!NOTE]
> These are local fixture results, not a service-level objective or production sizing estimate.

### REST results {#rest-results}

Latencies are p50 / p95 / p99 in milliseconds. The read payload sizes were not identical: median
average response sizes were 3,377 bytes for Ridu lists versus 1,348 for Payload, and 333.93 bytes
for Ridu finds versus 117.70 for Payload.

| Operation               | Concurrency | Ridu req/s | Payload req/s | Ridu latency       | Payload latency       |
| ----------------------- | ----------: | ---------: | ------------: | ------------------ | --------------------- |
| List 10 published posts |          16 |     15,968 |         1,272 | 0.90 / 1.75 / 2.43 | 12.31 / 15.77 / 19.76 |
| Find one published post |          16 |     28,020 |         2,276 | 0.53 / 0.91 / 1.44 | 6.70 / 10.03 / 11.31  |
| Create draft            |           8 |      5,174 |           350 | 1.42 / 2.21 / 3.87 | 21.52 / 31.89 / 38.77 |
| Update draft            |           8 |      5,054 |           273 | 1.48 / 2.45 / 2.96 | 28.44 / 36.16 / 39.02 |
| Delete draft            |           8 |      7,272 |           318 | 0.99 / 1.86 / 2.58 | 24.57 / 30.76 / 39.31 |

Draft creates omitted the optional unique slug, and updates changed the summary, so these timings
do not measure contention between writes claiming the same unique key. Lists counted the whole
published collection of 251 posts; at much larger sizes, the exact total count becomes the
largest part of a list request unless the list passes `pagination: false`.

The environment was macOS 26.2 with 32 GiB RAM, Go 1.27.1, Bun 1.4.0, Node 24.13.0, Next.js
16.2.6, and PostgreSQL 16.12. Reads selected equivalent application fields and disabled Payload
population. Writes used administrator accounts and exercised draft versions and trash deletion.

### Admin response and artifact {#admin-and-artifact}

| Measurement                |          Ridu |      Payload |
| -------------------------- | ------------: | -----------: |
| Cold admin HTML response   |      11.18 ms |    405.43 ms |
| Warm admin HTML throughput |   2,768 req/s |    219 req/s |
| Warm admin HTML p95        |       4.43 ms |     48.67 ms |
| HTML response size         | 223,653 bytes | 52,933 bytes |
| Deployment footprint       |     26.34 MiB |    77.26 MiB |

Warm admin requests used concurrency 8. The program measured the Ridu binary and Payload standalone
directory once, after all trials.

This measured the server response, not browser download, parsing, route chunks, rich-text startup,
or interaction readiness. Ridu returned its embedded SPA entry with server-prepared admin data,
including the schema manifest; Payload server-rendered its initial React document. The Payload
standalone artifact also retained modules for the fixture's SQLite fallback, so that artifact-size
comparison is less controlled than RSS.

## Block-heavy schemas {#blocks}

A block that several containers allow, inside containers that several layouts allow, appears at
every path that nesting can reach. Those paths are its placements, and a modest block library can
have hundreds of thousands of them. When a framework's cost follows placements, memory and startup
grow with the block graph even when every document is small. Ridu resolves, stores, types and binds
each block definition once, so its cost follows the definitions you write.

The blocks fixture in `tests/performance/blocks` builds Ridu and Payload configurations from one
framework-neutral spec per scenario, with the same fields, blocks, collections, drafts, access rules
and relationships:

| Scenario | Shape                                                                                            | Blocks | Collections | Field placements |
| -------- | ------------------------------------------------------------------------------------------------ | -----: | ----------: | ---------------: |
| A        | Port of the Payload #17214 block-memory reproduction: nesting up to 9 deep, 3 layout collections |     44 |           5 |           61,304 |
| A-full   | The reproduction's own config: 6 layout collections and a page with two layout fields            |     44 |           8 |          143,035 |
| B        | A site of 39 collections sharing a block library nested up to 3 deep                             |     28 |          39 |           35,954 |
| B-dense  | B's blocks, with every container allowing every lower block                                      |     28 |          39 |          254,354 |
| C        | A component library: 18 leaf blocks under six container layers                                   |     32 |           2 |           96,957 |
| C-over   | C's library used by three layout collections                                                     |     32 |           4 |          290,867 |

Each seeded document held 236–400 block instances (77–102 KB of JSON). The tables below use the
references variant: blocks are declared once and selected by slug, with Payload's
`blockReferences`. An inline variant declares the same blocks inside each field. Both applications
ran production builds against PostgreSQL 17.10 and a single-member MongoDB 8.2.9 replica set, in
Docker on the same Apple M2 Max. Each combination ran three trials: reads at concurrency 8 and
draft writes at concurrency 4, against 20 published documents.

Two details differ from the Payload reproduction on both sides. Slugs are kebab-case because Ridu
requires them, and the reproduction's Lexical rich-text field is a textarea.

### Startup, memory and the admin {#blocks-memory}

Medians across trials. "Admin edit" is the server response time for the first authenticated
request to a seeded document's edit page; it does not include browser rendering.

| Scenario | Database   | Ridu startup | Payload startup | Ridu idle RSS | Payload idle RSS | Ridu admin edit | Payload admin edit |
| -------- | ---------- | -----------: | --------------: | ------------: | ---------------: | --------------: | -----------------: |
| A        | PostgreSQL |       207 ms |          492 ms |      32.3 MiB |        151.4 MiB |           27 ms |             368 ms |
| A        | MongoDB    |       211 ms |          3.60 s |      29.8 MiB |         1.22 GiB |          108 ms |             295 ms |
| A-full   | PostgreSQL |       210 ms |          486 ms |      32.7 MiB |        150.6 MiB |           26 ms |             496 ms |
| A-full   | MongoDB    |       214 ms |          7.51 s |      30.8 MiB |         2.51 GiB |           87 ms |             426 ms |
| B        | PostgreSQL |       208 ms |          492 ms |      34.5 MiB |        153.1 MiB |           28 ms |             234 ms |
| B        | MongoDB    |       210 ms |          1.99 s |      31.9 MiB |        697.3 MiB |           97 ms |             214 ms |
| B-dense  | PostgreSQL |       208 ms |          491 ms |      35.2 MiB |        154.0 MiB |           30 ms |             450 ms |
| B-dense  | MongoDB    |       211 ms |         10.79 s |      32.9 MiB |         2.91 GiB |           74 ms |             422 ms |
| C        | PostgreSQL |       207 ms |          469 ms |      32.3 MiB |        151.0 MiB |           30 ms |             598 ms |
| C        | MongoDB    |       217 ms |          6.02 s |      30.1 MiB |         1.32 GiB |          109 ms |             700 ms |
| C-over   | PostgreSQL |       207 ms |          482 ms |      32.3 MiB |        151.1 MiB |           18 ms |             794 ms |
| C-over   | MongoDB    |       219 ms |         13.27 s |      29.6 MiB |         3.24 GiB |           99 ms |             731 ms |

- **Payload on PostgreSQL** stays near 151 MiB idle in every scenario. Its admin edit response
  grows with the block graph.
- **Payload on MongoDB** grows with placements. Its adapter compiled 33,029 Mongoose schemas for B
  and 264,958 for C-over.
- **Ridu** stays within 29.6–35.2 MiB and 207–219 ms in every scenario. The admin edit response
  carried 181–213 KB for Ridu and 887–982 KB for Payload.
- **Inline blocks:** Ridu's inline variant resolves to the same schema as references and idled at
  29.6–40.7 MiB. Payload's inline variant idled at 158–161 MiB on PostgreSQL and 693 MiB–1.30 GiB
  on MongoDB for A, B and C.

### Requests {#blocks-requests}

Successful requests per second, Ridu / Payload. "Find" reads one published document, and "List
10" returns the title and full block layout of ten. Draft creates and updates write a complete
block-heavy layout. Neither side populated relationships.

| Scenario | Database   |        Find |  List 10 | Create draft | Update draft |
| -------- | ---------- | ----------: | -------: | -----------: | -----------: |
| A        | PostgreSQL |  1,370 / 67 | 214 / 16 |     179 / 14 |     136 / 14 |
| A        | MongoDB    |   447 / 169 |  56 / 20 |      75 / 12 |      52 / 16 |
| B        | PostgreSQL | 1,626 / 122 | 262 / 22 |     160 / 20 |     137 / 22 |
| B        | MongoDB    |   554 / 219 |  62 / 26 |      78 / 17 |      57 / 24 |
| C        | PostgreSQL |  1,293 / 35 | 208 / 13 |     144 / 10 |      117 / 9 |
| C        | MongoDB    |   527 / 102 |  52 / 12 |      71 / 10 |      50 / 14 |

p95 latency in milliseconds, Ridu / Payload:

| Scenario | Database   |         Find |       List 10 | Create draft | Update draft |
| -------- | ---------- | -----------: | ------------: | -----------: | -----------: |
| A        | PostgreSQL |  7.4 / 163.7 |  50.7 / 632.9 | 26.2 / 307.5 | 35.4 / 319.2 |
| A        | MongoDB    |  21.6 / 72.6 | 206.2 / 551.9 | 58.1 / 435.4 | 85.0 / 335.2 |
| B        | PostgreSQL |   6.3 / 84.9 |  39.3 / 498.6 | 35.3 / 231.6 | 34.6 / 200.9 |
| B        | MongoDB    |  18.0 / 50.1 | 168.1 / 392.3 | 55.9 / 295.0 | 78.0 / 224.7 |
| C        | PostgreSQL |  7.7 / 329.2 |  51.8 / 734.8 | 32.6 / 448.8 | 42.2 / 522.3 |
| C        | MongoDB    | 19.9 / 117.1 | 208.3 / 917.8 | 61.8 / 579.5 | 87.7 / 406.8 |

Payload's request loads were recorded for A, B and C only; its larger scenarios measured startup,
memory and the admin. Ridu's requests on A-full, B-dense and C-over matched its smaller scenarios:
1,322–1,509 finds and 120–138 draft updates per second on PostgreSQL, and 49–57 draft updates per
second on MongoDB with p95 at most 89 ms.

### Executable fields and GraphQL {#blocks-hooks}

A Ridu-only variant gives the first text field of every leaf block a `beforeChange` hook, a
validator, a field access rule, a dynamic default and a visibility condition. Ridu binds those
callbacks once per block definition:

- **Memory and startup are unchanged:** idle RSS is 29.9–35.1 MiB and startup 206–213 ms.
- **Writes run the callbacks at every block instance:** draft creates and updates handled 6–33%
  fewer requests per second than without them. On PostgreSQL, that was 121–147 creates and 87–106
  updates per second; on MongoDB, 64–73 creates and 43–49 updates per second.

With GraphQL enabled on scenario A over PostgreSQL:

- **Ridu** built its GraphQL schema at startup without slowing it (206 ms, against 208 ms without
  GraphQL), idled at 33.4 MiB and answered the first query in 6.4 ms.
- **Payload** idled at 151.6 MiB, answered the first query in 76.3 ms and held 212.3 MiB afterwards.

Payload 3.87.0 (Next.js 16.2.6, Mongoose 8.22.1) was recorded on 5 October 2026. Ridu was recorded
on 6 October 2026. Both runs used the same host, Go 1.27.1, Bun 1.4.0, Node 24.13.0 and Docker
images. Payload was not rerun because its code had not changed between the runs.

## Report limits with the result {#limits}

State the data size, concurrency, trial count, hardware, software versions, topology, query shape,
success criteria, and known asymmetries. Keep cumulative B/op separate from peak RSS. Keep server
HTML timing separate from browser interaction time. Do not call a result faster if one side timed
out or returned less work without reporting it.

The reference fixture did not cover upload/image throughput, very large databases, every access or
localization shape, a hardware matrix, or complete browser interaction timing. The blocks fixture
seeded 20 documents per scenario and replaced rich text with a textarea, so it measures schema
cost rather than large collections or rich-text editing. Use it to reproduce
the method, then replace its schema and traffic with your own before choosing instance sizes or
setting an SLO.
