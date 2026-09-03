---
title: 'Performance and footprint'
description: 'Review Ridu’s current runtime benchmark and measure a representative application.'
product: core
eyebrow: 'Evaluate'
order: 230
navigation:
  section: 'Develop & operate'
  order: 80
  title: 'Performance and footprint'
---

One Go process serves Ridu's API and embedded admin; Node and package managers are not production
services. The benchmark below measures that process for one editorial application. Use the method
at the end of this page to measure your own schema and workload.

> [!NOTE]
> These are local benchmark results, not a service-level objective.

## Headline baseline {#headline}

The 15 August 2026 reference run compared Ridu with Payload 3.87.0 on the same Apple M2 Max and
shared local PostgreSQL 16.12 server. The values are medians of three trials with alternating server
order.

| Runtime state                   |  Ridu RSS | Payload RSS | Ridu reduction |
| ------------------------------- | --------: | ----------: | -------------: |
| Idle, PostgreSQL connected      | 26.86 MiB |  182.27 MiB |          85.3% |
| After first admin HTML response | 26.86 MiB |  220.17 MiB |          87.8% |
| After 250 additional documents  | 32.56 MiB |  290.66 MiB |          88.8% |
| Highest median workload peak    | 36.95 MiB |  493.17 MiB |          92.5% |

Idle RSS ranged from 26.00–26.89 MiB for Ridu and 181.69–183.17 MiB for Payload across the three
trials. PostgreSQL memory is excluded because both applications used the same server. RSS includes
the complete application process tree and was sampled asynchronously while the load ran.

Do not use the 27 MiB idle result as a production sizing estimate without testing your application.

## What the run exercised {#method}

Each fresh trial seeded equivalent editorial applications and 250 additional published posts, then
ran:

- 3,000 selected-field list reads and 3,000 selected-field point reads at concurrency 16;
- 300 authenticated draft creates, updates, and deletes at concurrency 8; and
- 300 admin HTML requests at concurrency 8.

Schema migration, deterministic seeding, warmup, and settling were outside the timed windows. Every
timed request had to return its expected `200` or `201`; the reference run completed 43,200 timed
requests without an unexpected status.

The environment was macOS 26.2 with 32 GiB RAM, Go 1.25.7, Bun 1.3.14, Node 24.13.0, and fresh
databases on PostgreSQL 16.12. These historical tool versions describe the run; consult the
repository's current compatibility policy before reproducing it.

## REST results {#rest-results}

Latencies are p50 / p95 / p99 in milliseconds. Measure a restored application before using these
numbers for capacity planning.

| Operation               | Concurrency | Ridu req/s | Payload req/s | Ridu latency       | Payload latency       |
| ----------------------- | ----------: | ---------: | ------------: | ------------------ | --------------------- |
| List 10 published posts |          16 |      9,988 |         1,322 | 1.48 / 2.37 / 4.55 | 11.73 / 16.07 / 19.51 |
| Find one published post |          16 |     19,732 |         2,196 | 0.78 / 1.25 / 1.65 | 6.32 / 10.34 / 32.49  |
| Create draft            |           8 |      3,748 |           377 | 2.01 / 2.91 / 3.29 | 20.30 / 28.10 / 31.03 |
| Update draft            |           8 |      3,738 |           288 | 2.06 / 2.69 / 3.29 | 27.08 / 35.41 / 40.72 |
| Delete draft            |           8 |      4,885 |           327 | 1.61 / 2.09 / 2.53 | 24.01 / 29.99 / 37.88 |

Reads selected equivalent application fields and disabled Payload population with `depth=0`. The
wire formats were not identical: Ridu's selected-list response was larger in this run, 2,796 bytes
versus 1,348 bytes. Writes used administrator accounts and exercised draft versions and deletion
through trash-enabled Posts collections.

A focused Payload run at mutation concurrency 16 exceeded a ten-second update deadline after its
creates completed. Ridu completed the same focused stages. An observation of Payload sessions idle
in transaction was consistent with pool starvation, but did not prove the cause, so the completed
headline write comparison uses concurrency 8 for both systems.

## Admin response and artifact size {#admin-and-artifact}

| Measurement                    |         Ridu |      Payload |
| ------------------------------ | -----------: | -----------: |
| Cold admin HTML response       |      1.14 ms |    391.66 ms |
| Warm admin HTML throughput     | 54,472 req/s |    260 req/s |
| Warm admin HTML p95            |      0.23 ms |     43.28 ms |
| HTML response size             |    454 bytes | 50,267 bytes |
| Production deployment artifact |    17.32 MiB |    76.39 MiB |

This measures the server response, not the author's experience in a browser. Ridu returns a small
static SPA entry and serves assets embedded in its binary; Payload server-renders the initial React
document. The test did not time JavaScript and CSS downloads, parsing, route chunks, rich-text
startup, or interaction readiness.

The Ridu artifact was one stripped binary with admin assets embedded. Payload's value was its Next
standalone directory with `.next/static` copied in. That traced directory also retained modules for
the sample application's SQLite fallback, so the artifact-size comparison is less controlled than
the RSS result.

## What this baseline does not cover {#limitations}

The benchmark does not cover:

- the dataset is modest and all traffic is loopback-only;
- byte-for-byte identical applications;
- relationship-heavy, access-filter-heavy, localized, upload and image-processing, job, and
  10,000-plus-document workloads were not tested;
- admin browser download, parse, route, rich-text, and interaction timing were not measured;
- three local trials are not a hardware matrix or statistical confidence study; and
- CPU profiles and Go allocation profiles were not captured.

## Measure your application {#measure-your-application}

The harness and exact rerun instructions live in `tests/performance/` in the Ridu repository. Use
them to reproduce the reference before changing the sample application, then add a workload representative of
your own collections, access filters, relationships, and uploads.

For deployment sizing, record at least idle and peak RSS, request latency percentiles, database
connection use, response sizes, build artifact size, browser interaction timings, and behaviour
under your expected concurrency. Run more than one trial on the target operating system and test a
realistic restored dataset rather than extrapolating from this page.

See [Production](/docs/production/) for operational readiness, [PostgreSQL](/docs/postgres/) and
[SQLite](/docs/sqlite/) for the official stores, and [Coming from PocketBase](/guides/from-pocketbase/) or
[Move from Payload](/guides/from-payload/) for product-level trade-offs.
