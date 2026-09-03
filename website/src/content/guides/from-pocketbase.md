---
title: 'Coming from PocketBase'
description: 'Compare Ridu with PocketBase and understand what an evaluation or migration involves.'
product: guides
eyebrow: 'Guide'
order: 15
navigation:
  section: 'Get started'
  order: 70
  title: 'Coming from PocketBase'
---

PocketBase packages SQLite, realtime subscriptions, file and user management, an admin dashboard,
and APIs in one executable. Ridu packages its Go API and Svelte admin in one binary, with PostgreSQL,
SQLite, and MongoDB within its [bounded production profile](/docs/mongodb/).

Ridu is a code-configured CMS framework inspired by Payload's authoring model, not a PocketBase
compatibility layer or database GUI.

## The important differences {#differences}

| Concern                | PocketBase                                         | Ridu                                                                                                  |
| ---------------------- | -------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Primary data store     | Embedded SQLite                                    | PostgreSQL 17, embedded SQLite, or MongoDB within its bounded profile through official store adapters |
| Content model          | Collections managed through APIs, dashboard, or Go | Executable Go config                                                                                  |
| Server customization   | Extend a PocketBase Go application                 | Access, hooks, fields, and backend plugins are compiled Go                                            |
| Browser client         | General JavaScript SDK                             | Generated project types over a framework-neutral Fetch SDK                                            |
| Admin                  | Embedded dashboard                                 | Schema-driven Svelte 5 admin                                                                          |
| Production JS runtime  | None                                               | None; built admin assets are embedded in the Go binary                                                |
| Realtime               | Built-in realtime subscriptions                    | Not a general supported transport                                                                     |
| Schema migrations      | PocketBase collection migrations                   | Reviewable migration files generated from config                                                      |
| Draft/version workflow | Not the central model                              | Implemented collection versions, drafts, scheduling, and restore                                      |
| PocketBase data import | Not applicable                                     | No supported PocketBase importer                                                                      |

Ridu generates a schema snapshot, OpenAPI, Go models and handles, TypeScript types, and the admin
plugin registry from Go config. Frontends get project-specific types for selected and populated
fields.

Read [Core concepts](/docs/core-concepts/) for that pipeline and [Go for TypeScript developers](/docs/go-for-typescript/)
if code-configured content models are new to you.

## When PocketBase remains the clearer fit {#choose-pocketbase}

PocketBase is the more direct choice when your requirements depend on capabilities Ridu does not
provide:

- the schema should be managed dynamically through PocketBase's dashboard or collection APIs;
- general realtime subscriptions are a core application transport;
- you want PocketBase's established SQLite and realtime operating model; or
- you need to preserve PocketBase collections, migrations, hooks, or client behaviour without a
  rewrite.

Ridu's SQLite adapter is supported only for a local filesystem database on one application host
and small or local workloads. Same-host processes can coordinate through SQLite's file locks, but
Ridu does not promise a shared network filesystem or horizontally replicated deployment.
[Live preview](/guides/live-preview/) documents Ridu's narrower realtime surface rather than
promising PocketBase-style subscriptions.

## When Ridu is worth evaluating {#evaluate-ridu}

Ridu may fit the direction of your project when:

- PostgreSQL is already an operational requirement, Ridu's single-host SQLite envelope fits, or
  the exact [bounded MongoDB profile](/docs/mongodb/) matches your deployment;
- the content schema should be reviewed as code and drive migrations and generated contracts;
- filtered access rules and lifecycle hooks must apply equally to local Go calls, REST, jobs, and
  the admin;
- editors need draft versions, scheduling, restore, rich text, uploads, and schema-driven forms;
- application frontends benefit from exact generated TypeScript query and response types; and
- you want one small production process without a JavaScript server.

Check the actual surface in [PostgreSQL](/docs/postgres/), [SQLite](/docs/sqlite/),
[MongoDB](/docs/mongodb/),
[Drafts and versions](/docs/drafts-and-versions/),
[Uploads](/docs/uploads/), and [TypeScript SDK](/docs/typescript-sdk/).

## Translate the project, not just the schema {#translate-project}

A PocketBase application often concentrates important behaviour in collection rules, application
hooks, file conventions, realtime listeners, and client-side query strings. Inventory each of those
before reproducing the collections in Ridu.

| Inventory item                | Ridu destination                                                                    |
| ----------------------------- | ----------------------------------------------------------------------------------- |
| Collection fields and indexes | `ridu.Collection` plus typed `field.*` definitions                                  |
| API rules                     | collection and field [access rules](/docs/access-control/)                          |
| Go event hooks                | [hook phases](/docs/hooks/)                                                         |
| Files and file metadata       | upload collections plus a [storage backend](/docs/storage/)                         |
| Browser API calls             | generated [TypeScript SDK](/docs/typescript-sdk/) package                           |
| Realtime listeners            | redesign; no general Ridu equivalent is supported                                   |
| SQLite-specific queries       | redesign for Ridu's query vocabulary; do not depend on PocketBase's internal tables |

Do not translate dashboard visibility into authorization. Ridu's admin reads the same manifest as
other clients, but hiding a field or action in the UI is never permission. Access decisions remain
part of the atomic store operation.

## Migration approach {#migration-reality}

Ridu has no PocketBase importer or command that reads a PocketBase database and preserves its
internal migration history.
A migration therefore needs custom export and import code:

1. Freeze and back up the PocketBase database and uploaded objects.
2. Export stable document IDs, timestamps, relationship IDs, auth identities, file metadata, and
   application state into a format you can inspect.
3. Model and resolve the target Ridu config, then review the selected adapter's migration before
   writing content.
4. Import through Ridu's local API so validation, access policy, hooks, and transactions run. Decide
   which hooks should be disabled or made idempotent for the import.
5. Copy objects into the configured storage backend and reconcile them with imported metadata.
6. Compare counts, representative documents, access behaviour, hashes, and auth flows against a
   restored rehearsal before any cutover.

Authentication needs special treatment: do not assume PocketBase password material, sessions, or
tokens can be copied into Ridu. Plan an identity mapping and session reset unless a tested
application-specific bridge proves otherwise.

## Evaluate without committing to a migration {#evaluate-without-migrating}

Start with a small parallel model—users, one editorial collection, and media—then exercise its
author experience and API. Measure your workload and compare it with the
[performance baseline](/docs/performance/). Use
[Troubleshooting](/docs/troubleshooting/) if installation or local prerequisites get in the way,
and review [Production](/docs/production/) before interpreting a successful local demo as a
deployment recommendation.
