---
title: 'Migrations'
description: 'Create, review, verify, and safely apply database migrations.'
product: data
eyebrow: 'Ship'
order: 225
navigation:
  section: 'Develop & operate'
  order: 30
  title: 'Migrations'
---

Ridu derives PostgreSQL and MongoDB migrations from executable Go config, but it never treats a
schema diff as permission to change a database. The CLI writes an immutable artifact for review,
verifies complete history in an isolated shadow target, and applies it only through an explicit
command.

> [!NOTE]
> This page covers the shared forward workflow and the PostgreSQL and MongoDB production runners.
> SQLite uses its own immutable planner and explicit reversible lifecycle; see
> [SQLite migrations](/docs/sqlite/#migrations).

> [!IMPORTANT]
> Ridu does not run production migrations when the server starts and does not generate an automatic
> down contract for PostgreSQL or MongoDB. Rehearse on restored data and prefer a forward
> corrective migration after deployment. During the MongoDB cutover, capture the matched
> selected-database/upload recovery point after the drained production `verify` and before `up`.

## The ordinary workflow {#workflow}

Change application config, then create and inspect one artifact:

```sh title="terminal"
npm run ridu -- migrate create --name add-post-summary
npm run ridu -- migrate plan
npm run ridu -- migrate verify
```

Commit the `*.ridu.json` file with the config and generated-contract changes. Deploy the exact
binary and artifact history that passed verification. This preparation example is not the MongoDB
production cutover order; follow the exact sequence under
[Deployment and recovery](#deployment-recovery).

| Command  | Database connection | What it does                                                                                                                                                                        | What it never does                                                                                                       |
| -------- | ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `create` | No                  | Resolves current config, compares it with the latest artifact manifest, detects renames, binds a compiled transform when selected, plans phases, and writes one immutable artifact. | It does not inspect or change the database.                                                                              |
| `plan`   | Yes                 | Compares local history with the database and shows each artifact, phase, step, and committed checkpoint. Add `--json` for automation.                                               | It does not apply a step.                                                                                                |
| `status` | Yes                 | Reports applied and pending artifacts and checks the exact ledger and adapter-managed physical state.                                                                               | It does not repair drift or apply history.                                                                               |
| `verify` | Yes                 | Creates a random temporary PostgreSQL schema or MongoDB database, replays complete admitted history, checks final assertions/readiness, then drops that exact target.               | It does not test production data volume, lock timing, or content-specific collisions. It cannot stop at a phase or step. |
| `up`     | Yes                 | Validates history and the ledger, takes the adapter's bounded migration lock/lease, and applies pending work with durable resumption where supported.                               | It does not invent a rollback or bypass a safety finding.                                                                |

Every command except `create` requires a non-empty artifact history whose newest manifest digest
matches executable config. This prevents a database from being reported as current while config and
migrations describe different applications. Supply PostgreSQL or MongoDB through `DATABASE_URL` or
`--database-url`.

## What is in an artifact {#artifact}

An artifact records the information needed to identify and re-run the transition:

- the full before and after manifests and their SHA-256 digests;
- the artifact, planner, and runner contract versions;
- stable phase and step IDs, execution modes, physical-state lineage, and a final assertion;
- confirmed collection and field rename intent; and
- machine-readable safety findings with notice, warning, or destructive severity.

PostgreSQL artifacts can use atomic transaction phases, checkpointed batch phases, and narrowly
typed non-transactional concurrent-index phases. New MongoDB planner contract `2.0.0` artifacts emit only their
typed physical index, confirmed rename, compiled-transform, retirement, and assertion steps; it
does not embed arbitrary driver commands. Authenticated planner-`1.0.0` history remains a supported
immutable prefix to v2 and is validated and replayed rather than rewritten. Arbitrary
non-transactional SQL is not admitted.
Formatting-only JSON changes do not alter the canonical artifact digest, but renaming, reordering,
removing, editing, or inserting applied history is detected by the database ledger.

Never edit an applied artifact. If a deployment needs correction, restore the committed history and
create a new forward migration.

## Renames preserve identity {#renames}

When a slug or field path changes, `create` proposes only unambiguous one-to-one candidates:

```text
Detected field rename "posts".title -> "posts".headline.
Preserve its existing data as a rename? [Y/n]
```

Confirm interactively or use `--accept-renames` only after reviewing every proposed match. Ridu can
then preserve physical tables, columns, indexes, constraints, nested stored values, versions, auth
state, task references, polymorphic relationships, and declared plugin reference keys as the
specific transition requires.

Ambiguous mappings are rejected rather than guessed. Collection slug swaps and chains must be split
into separate artifacts with a temporary slug so a value cannot be rewritten twice. Application
task input and arbitrary JSON remain opaque; the generic runner never searches them heuristically.

## Destructive and maintenance admission {#admission}

Safety flags have the following scope:

| Admission                   | Accepted by              | Meaning                                                                                                                                                                 |
| --------------------------- | ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--allow-destructive`       | `create`                 | Record a planner-confirmed destructive finding in the artifact after review. It does **not** connect to or change a database.                                           |
| `--allow-maintenance`       | `verify`, `up`           | Admit a traffic-sensitive phase after every old application process, writer, and worker has been stopped. Keep them stopped through retries until `status` is complete. |
| `--allow-insecure-database` | database-backed commands | Permit plaintext or bypass certificate verification only for a local PostgreSQL or MongoDB environment. `create` is offline and rejects the flag.                       |
| `--allow-unbounded`         | `up`, `verify`           | Permit an applicable zero runner wait/timeout. Ordinary production defaults remain bounded; MongoDB lease expiry is always bounded.                                     |

Deleting ordinary resources can require both destructive approval at creation and maintenance
approval at execution because the runner also purges sessions, credentials, versions, tasks,
preferences, locks, and reference-index state. Removing an upload collection also requires moving
or deleting its external objects yourself; the database artifact cannot do that work, so the planner
fails closed even with `--allow-destructive`.

## Common safety codes {#safety-codes}

Stable codes are intended for CI policy and runbook search, not for bypass scripts.

| Code                                                                        | What it protects                                                                                                                      | Safe response                                                                                                                                 |
| --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `RIDU_MIGRATION_PLAN_MISMATCH`                                              | The artifact no longer exactly matches the plan regenerated from its embedded manifests and rename intent.                            | Restore or recreate the reviewed artifact; do not trust or apply the changed file.                                                            |
| `RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE`                                      | Stored current values or version snapshots could retain dormant references after a field/target/cardinality decrease.                 | Keep the shape, remove the entire owning root with typed retirement, or design an application-owned data-cleanup contract first.              |
| `RIDU_REFERENCE_RENAME_MAPPING_AMBIGUOUS`                                   | A source or destination field is mapped more than once.                                                                               | Split the change into unambiguous artifacts.                                                                                                  |
| `RIDU_COLLECTION_SLUG_REWRITE_OVERLAP_UNSAFE`                               | A collection rename chain or swap could rewrite a value twice.                                                                        | Use a unique temporary slug across separate reviewed artifacts.                                                                               |
| `RIDU_UPLOAD_COLLECTION_REMOVAL_UNSAFE`                                     | A database migration cannot manage external objects.                                                                                  | Transfer/delete through upload operations, reconcile, and verify matched backups; retain the collection until a typed retirement path exists. |
| `RIDU_AUTH_DISABLE_STATE_UNSAFE` and related `*_DISABLE_STATE_UNSAFE` codes | Disabling auth, API keys, recovery, verification, versions, drafts, locks, or trash could leave dormant state that reactivates later. | Keep the capability enabled or introduce the narrower cleanup contract the finding requires.                                                  |
| `RIDU_RETIRE_DEPENDENT_VERSION_HISTORY`                                     | Resource retirement must also delete dependent owner history to prevent references from returning on restore.                         | Treat the version-history loss as destructive and include it in backup, review, and acceptance.                                               |

The error is useful information. `--allow-destructive` does not silence semantic safeguards.

## Applying and resuming {#apply-resume}

Shared artifact-envelope format `1` remains the committed history contract. PostgreSQL writes phase
and step progress separately: an interrupted transaction leaves neither its data nor step rows, a
batch resumes after its last committed keyset checkpoint, and a concurrent index resumes from
catalog state or removes an invalid interrupted build before retrying the reviewed definition.

New MongoDB planner contract `2.0.0` artifacts use that same immutable format-`1` envelope. The
runner authenticates and replays a committed planner-`1.0.0` prefix before v2 artifacts, takes a
fenced, expiring lease, records completed steps durably, recognizes already-completed physical
work, and resumes the same artifact after an interrupted process. It never treats process exit or
lease expiry alone as completion.

Production defaults bound PostgreSQL advisory-lock, statement, batch, concurrent-index, and idle
transaction work, plus MongoDB lease waits and complete migration operations. PostgreSQL `up` can
stop after a committed boundary with
`--stop-after-phase` or `--stop-after-step`; qualify a repeated ID as
`<artifact-name>/<boundary-id>`. MongoDB rejects explicit stop boundaries and instead resumes from
durable progress after interruption. `verify` always replays to completion.

Use `plan --json` and `status --json` to feed deployment automation. Do not infer success merely
from process exit after a requested stop boundary: `status` must show the complete target history
before application readiness can pass.

## Deployment and recovery {#deployment-recovery}

Only binaries with the same manifest digest and migration-history fingerprint may overlap. Rehearse
on a restored recovery point before production. For a MongoDB release with any new
artifact, including an additive or same-manifest data-only artifact, use this exact production
sequence:

1. Drain every old application replica and worker.
2. Run `DATABASE_URL="$MONGODB_OPERATIONAL_URL" ridu migrate verify` so shadow-database authority
   is scoped to that command. Append `--allow-maintenance` whenever the complete committed history
   contains semantic work, because clean-shadow verification replays every artifact.
3. After verification succeeds, capture the matched selected-database and upload recovery point
   with a separate least-privilege credential such as `$MONGODB_BACKUP_URL`.
4. Run `DATABASE_URL="$MONGODB_MIGRATION_URL" ridu migrate up` with the selected database-scoped
   application or controlled operator identity. Append `--allow-maintenance` only when the pending
   or incomplete history suffix contains semantic work.
5. Run `DATABASE_URL="$MONGODB_MIGRATION_URL" ridu migrate status` and require complete history and
   exact Ridu-managed indexes.
6. Start only the target release binary carrying the expected manifest and migration-history
   fingerprints using `$MONGODB_APP_URL`, then wait for `/readyz`.

Semantic work includes persisted content renames, compiled transforms, reference-index rebuilds,
and typed resource retirement. MongoDB `verify` requires maintenance admission whenever any of that
work appears in the complete committed history; `up` requires it only when the pending or incomplete
suffix contains that work. The flag is an assertion that old writers are stopped, not a lock that
stops them for you.

See [PostgreSQL](/docs/postgres/) or [MongoDB](/docs/mongodb/) for connection and adapter-specific
recovery configuration, [Production](/docs/production/) for the wider cutover checklist,
[Security](/docs/security/) for trust boundaries, and
[Troubleshooting](/docs/troubleshooting/#migration-refused) for failure-first diagnosis.
