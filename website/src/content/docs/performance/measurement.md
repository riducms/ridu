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
    'bundle budget'
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

| Repository workload       | What it isolates                                                                 | What it does not prove                                                 |
| ------------------------- | -------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| Embedded value scaling    | Hooks and replacements over large arrays, Blocks, and embedded plugin trees      | Universal request latency or a production data distribution.           |
| Immutable read API        | Repeated `View`/`Value` reads with and without explicit container copies         | Database or network performance.                                       |
| Ordinary engine copying   | Updates, localization, population, and traversal without application callbacks   | The cost of custom hooks or external services.                         |
| Locale-view reuse         | Repeated projections inside one operation and their retained memory              | A cross-request cache or every locale workload.                        |
| Ridu/Payload HTTP fixture | Equivalent selected CRUD, process RSS, admin HTML, and artifact size on one host | Identical wire formats, every feature, or a hosted capacity guarantee. |

## Understand the current reference result {#headline}

The 15 August 2026 reference run compared Ridu with Payload 3.87.0 on the same Apple M2 Max and
local PostgreSQL 16.12 server. Medians from three alternating trials were:

| Runtime state                   |  Ridu RSS | Payload RSS | Ridu reduction |
| ------------------------------- | --------: | ----------: | -------------: |
| Idle, PostgreSQL connected      | 26.86 MiB |  182.27 MiB |          85.3% |
| After first admin HTML response | 26.86 MiB |  220.17 MiB |          87.8% |
| After 250 additional documents  | 32.56 MiB |  290.66 MiB |          88.8% |
| Highest median workload peak    | 36.95 MiB |  493.17 MiB |          92.5% |

The timed program performed selected-field lists and finds, authenticated versioned draft creates,
updates and deletes, and admin HTML requests. It completed 43,200 timed requests without an
unexpected status. PostgreSQL memory was excluded because both applications shared it.

> [!NOTE]
> These are local fixture results, not a service-level objective or production sizing estimate.

### REST results {#rest-results}

Latencies are p50 / p95 / p99 in milliseconds. The read payload sizes were not identical.

| Operation               | Concurrency | Ridu req/s | Payload req/s | Ridu latency       | Payload latency       |
| ----------------------- | ----------: | ---------: | ------------: | ------------------ | --------------------- |
| List 10 published posts |          16 |      9,988 |         1,322 | 1.48 / 2.37 / 4.55 | 11.73 / 16.07 / 19.51 |
| Find one published post |          16 |     19,732 |         2,196 | 0.78 / 1.25 / 1.65 | 6.32 / 10.34 / 32.49  |
| Create draft            |           8 |      3,748 |           377 | 2.01 / 2.91 / 3.29 | 20.30 / 28.10 / 31.03 |
| Update draft            |           8 |      3,738 |           288 | 2.06 / 2.69 / 3.29 | 27.08 / 35.41 / 40.72 |
| Delete draft            |           8 |      4,885 |           327 | 1.61 / 2.09 / 2.53 | 24.01 / 29.99 / 37.88 |

The environment was macOS 26.2 with 32 GiB RAM, Go 1.25.7, Bun 1.3.14, Node 24.13.0, and
PostgreSQL 16.12. Reads selected equivalent application fields and disabled Payload population.
Writes used administrator accounts and exercised draft versions and trash deletion.

### Admin response and artifact {#admin-and-artifact}

| Measurement                    |         Ridu |      Payload |
| ------------------------------ | -----------: | -----------: |
| Cold admin HTML response       |      1.14 ms |    391.66 ms |
| Warm admin HTML throughput     | 54,472 req/s |    260 req/s |
| Warm admin HTML p95            |      0.23 ms |     43.28 ms |
| HTML response size             |    454 bytes | 50,267 bytes |
| Production deployment artifact |    17.32 MiB |    76.39 MiB |

This measured the server response, not browser download, parsing, route chunks, rich-text startup,
or interaction readiness. Ridu returned a static SPA entry from its embedded assets; Payload
server-rendered its initial React document. The Payload standalone artifact also retained modules
for the fixture's SQLite fallback, so that artifact-size comparison is less controlled than RSS.

## Report limits with the result {#limits}

State the data size, concurrency, trial count, hardware, software versions, topology, query shape,
success criteria, and known asymmetries. Keep cumulative B/op separate from peak RSS. Keep server
HTML timing separate from browser interaction time. Do not call a result faster if one side timed
out or returned less work without reporting it.

The reference fixture did not cover upload/image throughput, very large databases, every access or
localization shape, a hardware matrix, or complete browser interaction timing. Use it to reproduce
the method, then replace its schema and traffic with your own before choosing instance sizes or
setting an SLO.
