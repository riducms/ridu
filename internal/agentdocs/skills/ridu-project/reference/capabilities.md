<!-- Generated from website/src/content/docs/status.md by scripts/sync-agent-docs.ts. -->

# Capability status

Use this page to check whether a feature fits your project before following its setup guide.

## Find a capability {#available}

The matrix above links each capability to its setup guide and relevant API. Use the
[documentation home](https://riducms.com/docs/) for task-oriented guides and the [API reference](https://riducms.com/reference/) for
exported symbols.

## Limited capabilities {#limited}

These paths work with explicit constraints:

- **Generated projections.** `select` and `populate` are typed inputs, but TypeScript return types
  remain the collection's complete output type. Sort terms are strings rather than generated path
  unions.
- **Query vocabulary.** There is no `notIn`, full-text search, array all-elements operator, or
  general geospatial query API. Direct-field distinct values are available through the Local Go
  API when the selected field has no document-aware read rule; broader aggregation and a public
  REST distinct route are not. Bulk create and general idempotency keys are absent.
- **Localization boundary.** Localized storage, fallback, all-locale reads, interface translations,
  translated application labels, RTL authoring, timezone-aware number/date formatting, versions,
  and copy-locale work. Content locale and interface language remain independent, and a document's
  draft/published status applies to the document rather than independently to each locale.
- **Task primitives.** Typed handlers, retries, leases, heartbeats, cancellation, concurrency keys,
  and retention work. Workflow checkpoints, declarative cron, email/import/export adapters, and an
  access-controlled task admin are not included.
- **SQLite deployment.** SQLite is supported only as a local filesystem database on one application
  host for small or local workloads. Same-host processes use SQLite's file locks; Ridu makes no
  shared network filesystem or horizontally replicated deployment promise. Backups must be
  SQLite-aware or taken after quiescing WAL writers.
- **MongoDB deployment.** Production support covers only generated starter and blank projects on
  Linux x86-64 with MongoDB Community 8.2.9, SCRAM-SHA-256, verified TLS, and a writable
  three-member replica set. It
  does not cover Atlas, DocumentDB, Cosmos DB, standalone servers, other versions/topologies/OS or
  architectures, arbitrary scale, network-partition matrices, or PITR. The production cutover is
  drain → command-scoped `verify` with the operational URL → matched database/upload snapshot →
  `up` with the selected app/operator URL → post-`up` `status` → start with the app URL. Use separate
  scoped credentials for the application, verification, and backups. See [MongoDB](./mongodb.md).
- **Multi-replica preview.** Preview grants are process-local, so multiple application replicas
  need process affinity for a preview session. Every preview read is still access checked.
- **Large-file workflow.** Uploads are bounded and remote URLs are SSRF hardened, but resumable or
  direct-to-storage upload, quarantine, and malware scanning are not implemented.
- **Migration overlap.** Code-only binaries with the same manifest and migration-history
  fingerprints can overlap. Any release with a new artifact—including a same-manifest data-only
  artifact—needs the adapter's coordinated drain–verify–snapshot–migrate–status–start cutover; an
  explicit multi-history admission contract is not implemented. This is an operational constraint
  to compare with the project's downtime objective, not a general production-readiness defect.
- **Long-tail admin polish.** Broader tablet acceptance, related-list live refresh, plugin-isolated
  recovery, and known dirty-state, leave-guard, and accessibility defects remain open.

## Experimental and optional surfaces {#experimental}

The [GraphQL plugin](./graphql.md) is opt-in and supports generated schema, PostgreSQL CRUD,
localization, filters, population, and bounded requests. Compatibility may still change before the
plugin is marked available.

The [MCP plugin](./mcp.md) is opt-in and exposes only explicitly selected collection and global
reads to authenticated clients. It preserves ordinary access predicates and field redaction. Write
tools, prompts, custom resources, and source-code mutation are not currently supported.

The strict in-memory store is test infrastructure, not an application database. PostgreSQL, SQLite,
and MongoDB are supported in production only within their separately documented operating envelopes.
The public `store.Store` contract allows custom adapters, but conformance includes transactions,
auth, versions, tasks, locks, preferences, references, and cleanup—not only CRUD.

## Planned boundaries {#planned}

The following features do not have a supported API: general realtime subscriptions, MFA/passkeys,
general API quotas, full-text search, resumable upload, workflow checkpoints, declarative cron,
task administration, and shared multi-replica preview capabilities.

## How status labels work {#labels}

- **Available** means the documented workflow is supported.
- **Limited** means a useful path exists with named missing behavior or an operational constraint.
- **Experimental** means the feature works, but its API or compatibility may change.
- **Planned** means no supported API exists.

## Evaluate Ridu for your project {#evaluate}

1. [Install Ridu](./installation.md) and exercise the generated project.
2. Read [Core concepts](./core-concepts.md) and [Project structure](https://riducms.com/guides/project-structure/) to
   understand ownership and deployment shape.
3. Check the limited capability list against your product requirements.
4. Rehearse migrations, backups, the selected database, object storage, and admin workflows with realistic data.
5. Use [Security](https://riducms.com/docs/security/), [Production](./production.md), and
   [Troubleshooting](./troubleshooting.md) as an operational readiness checklist.

Evaluating another CMS? Start with [Move from Payload](../../payload-to-ridu/references/migration-guide.md) or
[Ridu for PocketBase users](https://riducms.com/guides/from-pocketbase/).
