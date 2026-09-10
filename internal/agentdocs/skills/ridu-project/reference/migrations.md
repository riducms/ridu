<!-- Generated from website/src/content/docs/migrations.md by scripts/sync-agent-docs.ts. -->

# Migrations

Migrations keep the database in step with the fields and collections in your Go config. New Ridu
projects include their initial migration. After changing the schema, create one more migration,
review it, and commit it with the code.

> [!NOTE]
> This page covers the shared forward workflow and the PostgreSQL and MongoDB production runners.
> SQLite uses its own immutable planner and explicit reversible lifecycle; see
> [SQLite migrations](./sqlite.md#migrations).

> [!IMPORTANT]
> Ridu does not run production migrations when the server starts and does not generate an automatic
> down contract for PostgreSQL or MongoDB. Rehearse on restored data and prefer a forward
> corrective migration after deployment. During the MongoDB cutover, capture the matched
> selected-database/upload recovery point after the drained production `verify` and before `up`.

## Create and check a migration {#workflow}

Change application config, then create and inspect one artifact:

```sh title="terminal"
ridu migrate create --name add-post-summary
ridu migrate verify
```

`create` compares the new config with the latest migration and writes a `*.ridu.json` file without
connecting to a database. Read the file, then run `verify` to replay the full history in an isolated
target. SQLite uses a temporary database automatically. PostgreSQL and MongoDB need `DATABASE_URL`
for their shadow target.

Commit the migration with the config and generated contracts. `ridu build` rejects a schema that
does not match the last migration.

## Inspect a target database {#inspect}

`plan` is optional during authoring. It answers a different question: which committed migrations
are pending in a particular database? Select that database first:

```sh title="terminal" group="migration-plan" tab="SQLite"
export RIDU_SQLITE_PATH="$PWD/.ridu/development.sqlite"
ridu migrate plan
```

```sh title="terminal" group="migration-plan" tab="PostgreSQL or MongoDB"
export DATABASE_URL='replace-with-the-target-database-url'
ridu migrate plan
```

Apply and confirm the same target during deployment:

```sh title="terminal"
ridu migrate up
ridu migrate status
```

`plan` reports what is pending and any stored progress; it never changes the database. `up`
validates history, takes the adapter's migration lock or lease, and applies the pending work.
`status` then compares the database ledger and physical schema with the committed artifacts.

`verify` is a rehearsal. It creates an isolated PostgreSQL schema, temporary SQLite database, or
random MongoDB database, replays every migration, checks the result, and removes the temporary
target. It cannot reproduce production data volume, lock timing, or content-specific collisions.

Every command except `create` requires a non-empty artifact history whose newest manifest matches
the executable config. Database-backed commands use `DATABASE_URL` for PostgreSQL and MongoDB, or
`RIDU_SQLITE_PATH` for SQLite. `verify` is the one SQLite exception because it uses a temporary
database.

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

## Data transforms and versioned resources {#data-transforms}

Compiled data transforms can perform admitted data changes on unversioned resources. PostgreSQL,
SQLite, and MongoDB reject transform mutations of versioned collections and globals: a general
transform cannot yet rewrite the current document and all retained snapshots atomically. This
applies to revision history even when drafts are disabled, and can block backfills, retypes, or
required-field transitions that need to rewrite versioned content.

Supported typed rename executors preserve retained history; the restriction does not prohibit all
schema evolution. Prefer an admitted additive change or confirmed typed rename when it fits the
application. Rehearse the complete history and representative retained versions before deployment.
Disabling versions or editing an immutable artifact is not a supported way to bypass this limit.

## Destructive and maintenance admission {#admission}

Safety flags have narrow scopes:

- **`--allow-destructive` on `create`** records a planner-confirmed destructive finding in the
  artifact after review. It does not connect to or change a database.
- **`--allow-maintenance` on `verify` or `up`** admits a traffic-sensitive phase after every old
  application process, writer, and worker has stopped. Keep them stopped through retries until
  `status` completes.
- **`--allow-insecure-database` on database-backed commands** permits plaintext or bypassed
  certificate verification only for a local PostgreSQL or MongoDB environment. The offline
  `create` command rejects it.
- **`--allow-unbounded` on `up` or `verify`** permits an applicable zero runner wait or timeout.
  Ordinary production defaults remain bounded, and MongoDB lease expiry is always bounded.

Deleting ordinary resources can require both destructive approval at creation and maintenance
approval at execution because the runner also purges sessions, credentials, versions, tasks,
preferences, locks, and reference-index state. Removing an upload collection also requires moving
or deleting its external objects yourself; the database artifact cannot do that work, so the planner
fails closed even with `--allow-destructive`.

## Common safety codes {#safety-codes}

Stable codes are intended for CI policy and runbook search, not for bypass scripts.

<dl class="doc-option-list">
  <div>
    <dt><code>RIDU_MIGRATION_PLAN_MISMATCH</code></dt>
    <dd>The artifact no longer matches the plan regenerated from its embedded manifests and rename intent. Restore or recreate the reviewed artifact; do not apply the changed file.</dd>
  </div>
  <div>
    <dt><code>RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE</code></dt>
    <dd>Current values or version snapshots could retain dormant references after a field, target, or cardinality decrease. Keep the shape, retire the complete owning root, or design an application-owned cleanup first.</dd>
  </div>
  <div>
    <dt><code>RIDU_REFERENCE_RENAME_MAPPING_AMBIGUOUS</code></dt>
    <dd>A source or destination field is mapped more than once. Split the change into unambiguous artifacts.</dd>
  </div>
  <div>
    <dt><code>RIDU_COLLECTION_SLUG_REWRITE_OVERLAP_UNSAFE</code></dt>
    <dd>A rename chain or swap could rewrite one value twice. Use a unique temporary slug across separate reviewed artifacts.</dd>
  </div>
  <div>
    <dt><code>RIDU_UPLOAD_COLLECTION_REMOVAL_UNSAFE</code></dt>
    <dd>A database migration cannot manage external objects. Transfer or delete them through upload operations, reconcile the result, and verify matched backups before retiring the collection.</dd>
  </div>
  <div>
    <dt><code>RIDU_AUTH_DISABLE_STATE_UNSAFE</code> and related <code>*_DISABLE_STATE_UNSAFE</code> codes</dt>
    <dd>Dormant auth, API-key, recovery, verification, version, draft, lock, or trash state could reactivate later. Keep the capability enabled or add the cleanup contract named by the finding.</dd>
  </div>
  <div>
    <dt><code>RIDU_RETIRE_DEPENDENT_VERSION_HISTORY</code></dt>
    <dd>Resource retirement must also delete dependent owner history. Include that version-history loss in backup, review, and acceptance.</dd>
  </div>
</dl>

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

See [PostgreSQL](./postgres.md) or [MongoDB](./mongodb.md) for connection and adapter-specific
recovery configuration, [Production](./production.md) for the wider cutover checklist,
[Security](https://riducms.com/docs/security/) for trust boundaries, and
[Troubleshooting](./troubleshooting.md#migration-refused) for failure-first diagnosis.
