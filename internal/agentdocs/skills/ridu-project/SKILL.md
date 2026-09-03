---
name: ridu-project
description: Build, change, troubleshoot, or ship a Ridu application. Use for executable Go config, collections, fields, access, hooks, Local API, generated contracts, migrations, plugins, admin extensions, and project lifecycle commands.
---

# Work on a Ridu project

Resolve Ridu's executable Go config with the project command or `npm run ridu -- generate`. Never
reconstruct the complete schema by parsing Go or maintaining a second TypeScript or JSON schema.

Read `package_manager` from `ridu.toml` before running frontend or project-local CLI commands.
Examples in this skill use npm; translate `npm install` and `npm run` to Bun, pnpm, or Yarn when the
project selects that manager. Version-1 projects without the key use Bun for compatibility.

## Route the task

Read `PROJECT.md`, `ridu.toml`, the relevant `content/*.go`, and nearby tests first. Then load only
the reference needed for the change:

- First project and end-to-end generated SDK read: [reference/quickstart.md](reference/quickstart.md)
- Requirements, scaffold choices, existing services, and recovery:
  [reference/installation.md](reference/installation.md)
- Configuration and project assembly: [reference/configuration.md](reference/configuration.md)
- Collections, globals, indexes, auth, uploads, and versions:
  [reference/collections.md](reference/collections.md)
- Field constructors, nesting, layout, relationships, validation, and localization:
  [reference/fields.md](reference/fields.md)
- Collection and field authorization: [reference/access-control.md](reference/access-control.md)
- Lifecycle behavior and transactional side effects: [reference/hooks.md](reference/hooks.md)
- Filters, projection, population, sorting, and pagination:
  [reference/querying.md](reference/querying.md)
- In-process operations and generated typed handles: [reference/local-api.md](reference/local-api.md)
- Generated Go, TypeScript, OpenAPI, and plugin contracts:
  [reference/generated-contracts.md](reference/generated-contracts.md)
- Schema evolution and production artifacts: [reference/migrations.md](reference/migrations.md)
- PostgreSQL setup, migrations, readiness, backup, and multi-replica operation:
  [reference/postgres.md](reference/postgres.md)
- SQLite setup, compiled transforms, migration lifecycle, and supported operating envelope:
  [reference/sqlite.md](reference/sqlite.md)
- MongoDB replica-set development, immutable migrations, exact serving verification, and bounded
  production support: [reference/mongodb.md](reference/mongodb.md)
- Backend and static admin extensions: [reference/plugins.md](reference/plugins.md)
- REST resources and the generated typed Fetch client:
  [reference/rest-api.md](reference/rest-api.md) and
  [reference/typescript-sdk.md](reference/typescript-sdk.md)
- Choosing between Local API, REST, SDK, and plugins:
  [reference/data-access.md](reference/data-access.md)
- Official rich text, SEO, Form Builder, GraphQL, and MCP adoption:
  [reference/rich-text.md](reference/rich-text.md), [reference/seo.md](reference/seo.md),
  [reference/form-builder.md](reference/form-builder.md),
  [reference/graphql.md](reference/graphql.md), and [reference/mcp.md](reference/mcp.md)
- Svelte admin customization and task workflows: [reference/admin.md](reference/admin.md),
  [reference/browsing-content.md](reference/browsing-content.md),
  [reference/saved-views-and-hierarchy.md](reference/saved-views-and-hierarchy.md),
  [reference/editing-documents.md](reference/editing-documents.md),
  [reference/bulk-and-trash.md](reference/bulk-and-trash.md), and
  [reference/document-locks.md](reference/document-locks.md)
- Drafts, versions, scheduling, localization, uploads, and live preview:
  [reference/drafts-and-versions.md](reference/drafts-and-versions.md),
  [reference/localization.md](reference/localization.md),
  [reference/uploads.md](reference/uploads.md), and
  [reference/live-preview.md](reference/live-preview.md)
- Application, database, transport, and browser testing: [reference/testing.md](reference/testing.md)
- Release, readiness, security, and cutover: [reference/production.md](reference/production.md)
- Diagnose common generated-project, database, API, SDK, admin, and plugin failures:
  [reference/troubleshooting.md](reference/troubleshooting.md)
- Current released support and explicit limits: [reference/capabilities.md](reference/capabilities.md)
- A TypeScript-oriented Go bridge: [reference/go-for-typescript.md](reference/go-for-typescript.md)

Use the separate `payload-to-ridu` skill for a Payload migration or parity assessment.

## Ownership rules

- Ordinary CMS behavior belongs in `content/`; runtime wiring belongs in `cmd/server/`; local admin
  extensions belong under `admin/src/`.
- Generated schema, OpenAPI, Go/TypeScript contracts, and generated plugin registries are
  machine-owned. Change executable config or plugin registration and regenerate.
- Keep access rules and hooks executable in Go. Admin visibility is presentation, not
  authorization. Filtered access must remain part of the atomic store operation.
- Use the Local API or generated typed handles for content operations. Do not call a store adapter
  directly or introduce an authorization bypass flag.
- Backend plugins are compiled Go packages and admin plugins are statically registered. Do not add
  runtime package installation or a JavaScript production server.

## Change workflow

1. Identify the observable behavior and the application-owned file that controls it.
2. Read the focused reference and follow nearby public Ridu patterns.
3. Change executable config or application code without editing generated output.
4. Run `npm run ridu -- generate` after config changes. For production schema changes, create and review an
   immutable artifact with `npm run ridu -- migrate create --name <lowercase-kebab-case>`. Explicitly confirm
   detected renames (or use reviewed `--accept-renames` automation); bind a registered compiled
   data rewrite with `--transform <name>` instead of hiding it in startup code.
5. Test success and denial/error paths at the smallest relevant layer.
6. Run `npm run ridu -- generate --check`, the project tests, and `npm run ridu -- check` before handoff.

## MongoDB production boundary

MongoDB production support covers ordinary Ridu-generated starter and blank projects on Linux
x86-64 using digest-pinned MongoDB Community 8.2.9 as a writable three-member replica set with
SCRAM-SHA-256 authentication and CA- and hostname-verified TLS. Give the running application only
a database-scoped credential. The release gate separates it from one controlled operational identity
used for both shadow verification and backup/restore. That is release-gate evidence;
deployments should split the operational duties into narrower credentials where practical. Never
place operational credentials in the server environment. Local ARM runs are preflight only; the
production support evidence comes from the GitHub Linux x86-64 release job.

For every release with a new migration artifact, review that exact immutable artifact—even when a
data-only transform leaves the manifest unchanged. If planning reports a transform or resource
retirement as destructive, inspect the affected data and rerun the reviewed
create command with `--allow-destructive`, preserving its transform and rename inputs. The flag
completes artifact creation without connecting to the database. Commit the resulting artifact.
Checksum-authenticated planner `1.0.0` artifacts remain the supported v1-to-v2 immutable history
prefix; planner `2.0.0` owns new semantic rename, transform, and retirement steps. Preserve the v1
prefix rather than rewriting it during an upgrade. `npm run ridu -- check` and `npm run ridu -- build`
remain offline: they compare committed history with executable config but do not inspect the applied
ledger or indexes. Live `status` and startup/readiness own those checks.

Cut over in this order:

1. **Drain writers.** Stop every old application process, worker, and other writer, and keep them
   drained through completion and retries.
2. **Verify history.** Run `npm run ridu -- migrate verify` with the operator's scoped verification
   credential. Append `--allow-maintenance` whenever the complete committed history contains
   semantic work, because the clean shadow replays every artifact. Grant it only after the drain.
3. **Capture the recovery point.** Use the operator's controlled backup credential for a
   database-scoped `mongodump` and copy the matching upload-object snapshot while writers remain
   drained. Restore that scope with `mongorestore` and the matching uploads if recovery is required.
4. **Apply migrations.** Run `npm run ridu -- migrate up` against the exact release history. Append
   `--allow-maintenance` only when the pending or incomplete history suffix contains semantic work.
5. **Confirm status and readiness.** Require `npm run ridu -- migrate status` to report the exact history
   current and preserve the non-mutating readiness check for the same ordered artifact
   fingerprint, manifest digest, and required indexes.
6. **Start the release.** Start the new binary only after those preconditions are true; startup
   must pass exact readiness before traffic is admitted.

MongoDB `down`, `reset`, `refresh`, and `fresh` remain unsupported; use a reviewed forward artifact
or restore the whole matched recovery point.
Do not extend this claim to Atlas, Amazon DocumentDB, Azure Cosmos DB, standalone servers, other
MongoDB versions or topologies, other operating systems or architectures, arbitrary scale,
network-partition matrices, or point-in-time recovery.

Stop for product direction when authorization intent is ambiguous, a destructive migration is
required, an extension needs a contract Ridu does not expose, or the requested behavior would
silently change stored data or an external API.
