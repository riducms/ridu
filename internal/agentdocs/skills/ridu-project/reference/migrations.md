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
the executable config. A database `ridu dev` synchronized has no history of its own; see [Record a
`ridu dev` database's history](#baseline). Database-backed commands use `DATABASE_URL` for
PostgreSQL and MongoDB, or `RIDU_SQLITE_PATH` for SQLite. `verify` is the one SQLite exception
because it uses a temporary database.

## Record a `ridu dev` database's history {#baseline}

`ridu dev` synchronizes its database straight from config and records no migrations, so `ridu migrate
status` and `up` report that database as unmanaged. When it already has the schema of your committed
migrations, record them instead of recreating it:

```sh title="terminal"
ridu migrate create add-post-summary
ridu migrate baseline
ridu migrate status
```

`baseline` runs no migration step. It records pending migrations as applied through the newest one
whose schema the database already has, and leaves later ones pending for `up`. It stops before a
migration with a step schema sync never runs, such as a rename's content rewrite, a data transform,
or a reference-index rebuild. That is how `ridu dev`'s rename pause resolves: `baseline` records
the history before the rename, then `up` runs it. `baseline` refuses a database whose schema
matches no committed migration and a migration that is partly applied, and it changes nothing
when the history is already current.

PostgreSQL and MongoDB keep synchronizing under `ridu dev` after a baseline; run `baseline` again
after each `ridu migrate create`. A SQLite database with history is managed by `ridu migrate` from
then on: `ridu dev` still serves it but skips schema sync, so a config change needs `ridu migrate
create` and `ridu migrate up`. Production databases never need `baseline`: they are created and
changed only by `up`.

### Squash history before the first deployment

Before the first deployment you can replace your schema history with one new initial migration.
Each squash produces new artifact names and digests, so a development database that recorded the
old history needs `baseline --replace` to record the new one. Keep the old artifacts as evidence:

```sh title="terminal"
mv migrations .ridu/previous-migrations
ridu migrate create initial
ridu migrate baseline --replace --previous-history .ridu/previous-migrations
ridu migrate status
```

The database must already have the new initial's schema. On PostgreSQL and MongoDB, `ridu dev`
keeps a baselined database synchronized, so let it apply your current config first; the changes it
accepted, such as an added field or a field kind changed while empty, fold into the new initial. On
SQLite, `ridu dev` does not synchronize a database with history: run `ridu migrate up` on the old
history before squashing, so the new initial describes the schema the database already has.

The old artifacts must match the recorded filenames and checksums. If they are gone, recover them
from Git or a backup: Ridu cannot infer from a checksum whether an old artifact ran a data
transform. Replacement refuses schema drift and partial migrations. It also refuses dropping a
collection or global, because only a migration removes its stored documents and versions: recreate
the development database with `ridu migrate up`, or remove it with a migration instead.

A data transform or other semantic step that already ran must stay. Keep the artifact that ran it,
and every earlier artifact, unchanged, and squash only the artifacts after it. A new data transform
after the squash stays pending: the replacement records the history before it, and `ridu migrate
up` runs it. The replacement changes only the migration ledger and the database's schema record,
never documents or version snapshots.

When the database is exactly at its recorded head, an application built from the old history may
still be serving it, and after the replacement that build fails readiness until you rebuild it from
the new history. `baseline --replace` explains this and asks
`Replace the recorded history anyway? [y/N]`; stop every application process and writer before
answering `y`. This always happens on SQLite, and on a PostgreSQL or MongoDB database whose
`ridu dev` changes left its tables and indexes unchanged. Without a terminal, pass
`--allow-production` instead of answering. Keep immutable history after deployment whenever
possible.

## What is in an artifact {#artifact}

An artifact records the information needed to identify and re-run the transition:

- the full before and after manifests and their SHA-256 digests;
- the artifact, planner, and runner contract versions;
- stable phase and step IDs, execution modes, physical-state lineage, and a final assertion;
- confirmed collection and field rename intent; and
- machine-readable safety findings with notice, warning, or destructive severity.

PostgreSQL artifacts can use atomic transaction phases, checkpointed batch phases, and narrowly
typed non-transactional concurrent-index phases. MongoDB planner contract `3.0.0` artifacts emit
only typed physical index, confirmed rename, compiled-transform, retirement, and assertion steps;
they do not embed arbitrary driver commands. Arbitrary non-transactional SQL is not admitted.
Formatting-only JSON changes do not alter the canonical artifact digest, but renaming, reordering,
removing, editing, or inserting applied history is detected by the database ledger.

Never edit an applied artifact. If a deployment needs correction, restore the committed history and
create a new forward migration.

## Presentation changes need no migration {#presentation-changes}

Presentation settings are ignored when deciding whether another migration is required, on every
database. They never shape stored data:

- the application name, admin interface languages and timezones, admin loaders, and the method,
  path and summary of custom endpoints;
- `CollectionAdmin`, `GlobalAdmin` and field `Admin` settings, such as hiding a collection or moving
  it to another navigation group;
- the labels of collections, globals, blocks, select options and content locales, and a content
  locale's text direction;
- select option order, number input steps, code-editor languages, row labels and join default
  columns.

Changing them needs no migration: `ridu build`, `ridu migrate status`, and readiness compare the
schema without them. When nothing else changed, `ridu migrate create` reports that no migration is
needed, writes no file, and exits successfully. The next migration that does change the schema
records the new settings too. Content locales, their default and fallbacks, select option values,
validation rules and everything else that shapes stored data still need a migration.

Plugin package versions and plugin build metadata, such as the package a plugin field's generated
TypeScript types come from, are not part of the stored schema either, so upgrading Ridu or a plugin
does not require a migration by itself.

## Changing a field's kind {#field-kind-changes}

Changing an existing field from blocks to rich text, text to number, relationship to upload, or
scalar to multiple select (and the reverse) can
leave stored values that the new schema cannot read. `ridu dev` checks current documents (including
trash) and every retained version snapshot before generating contracts or reloading. It displays
the affected counts and pauses when values exist. Under a task runner or coding agent, it rejects
the reload and keeps the accepted server running.

Kinds that store the same plain string need no review when every stored value stays valid. Text,
textarea, code, email, date, select and radio values all fit text, textarea and code, and a select
or radio fits another select or radio that keeps all of its options. The reverse is reviewed: an
arbitrary text value may not be a valid option, email address or date.

A kind change inside a plugin field's embedded payload, such as a rich-text block field changed
from text to number, is reviewed at the plugin field. Ridu counts every document with a value in
that field and offers no clearing, because clearing would remove the whole rich-text value. Restore
the previous embedded field kind instead.

The accepted manifest is recorded in the database only after successful synchronization or
migration. Generated files and disposable `.ridu/` caches cannot establish the schema that wrote
your content; deleting the cache or running `ridu generate` does not bypass review. A fresh database
initializes normally.

A database with application tables, collections, or migration history but no schema record stops
with `RIDU_DEVELOPMENT_SCHEMA_UNKNOWN` in `ridu dev`. On PostgreSQL and MongoDB, `ridu migrate up`
also refuses pending work when applied history has no schema record. Tables and indexes cannot tell
apart kinds stored as the same JSON. Restore a backup
including the schema record, or preserve your needed content and use a new, dedicated development
database. `baseline` and `baseline --replace` cannot recover an unknown schema merely because its
physical storage looks the same.

`--no-sync` skips mutation, not field-kind safety. When an initial inspection finds no values,
Ridu drains the old development server and checks again before generation. A late old-schema write,
or a reload rejected later before anything changed the schema or content, restarts the accepted
executable. Stop any other application processes using that database before an incompatible change
too.

In an interactive session you can clear, when available, or cancel. Cancel keeps the current
schema and all stored values. Ridu has no built-in conversion and does not infer a mapping from
your block definitions into a rich text plugin document. To keep the values, restore the previous
field kind, then add a field with the new kind and copy the values into it with a [compiled data
transform](#data-transforms). Transforms cannot yet change fields of versioned resources or weaken
stored reference shapes, so those changes require an application-owned recovery that handles
retained snapshots too. The prompt needs only a terminal, even in a project without a migrations
directory.

For a database without applied or incomplete immutable migration history, clear is available for
ordinary stored fields. It asks for a second explicit confirmation, stops the old server, checks
the displayed counts again inside the transaction, and removes that field's values from current
documents and **every retained snapshot**. Other fields and version metadata remain. If clearing
fails, nothing is removed and the old server restarts. A managed database, an auth/upload
resource, a localization change, an embedded payload change, a save combining the change with a
rename, or a top-level field that stays required does not offer clearing; `ridu dev` explains why
and keeps the current schema. Make a required field optional for the clearing save and require it
again after entering values. If a later build or synchronization fails after a confirmed clear,
the values have already been removed and the server stays stopped while `ridu dev` waits for a
corrective save.

`ridu migrate create` refuses a schema-only kind change even with `--allow-destructive`. With
`--transform`, PostgreSQL, SQLite and MongoDB admit a kind change of an unversioned resource; the
versioned-resource and reference-shape safety rules still apply. PostgreSQL changes the column
before the transform runs, so it also refuses a column type it cannot convert in place, such as
text to number. Add a field with the new kind and copy the values into it instead.

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

Ridu proposes renames for collections and their fields, not for the fields of a global.

A field rename changes the field's name and nothing else. `create` refuses one that also renames
the children of a group, array, or blocks field, or changes whether the field is localized,
because the stored values would move to the new name with their old inner shape. Rename the field
in one migration and make the other change in the next.

### Renames in `ridu dev` {#renames-in-dev}

`ridu dev` compares each saved config with the schema it last brought the development database
to. When the difference looks like a rename, it generates and synchronizes nothing until the
rename is settled, because ordinary schema sync would leave the stored values under the old name.
A database that no longer has that schema, because you migrated it by hand or replaced it, is not
held.

#### With a terminal

`ridu dev` asks the same question as `create`. It needs an explicit `y` or `n`; an empty line is
not an answer.

Answer `y` and it writes the rename migration, stops the running server, applies the rename to the
development database, and brings up the new server. You decide once, and production gets the same
rename from the committed migration. When `ridu dev` had already synchronized changes that no
migration covers, it first writes a `changes-before-<name>` migration for them.

The server stops first because it still runs the old config, and a write from it would store
content under the old name again. How the rename is then applied depends on the adapter:

- PostgreSQL and MongoDB record the migrations the database already has, then apply the rename
  through the migration runner.
- SQLite moves the stored values directly, so the database stays under development schema sync. A
  SQLite database that `ridu migrate` already manages takes the rename through the migration
  runner instead. SQLite renames fields, not collections.

Answer `n` and nothing is written. The old values stay where they are, and the adapter's ordinary
development sync decides what happens next:

- SQLite carries on. Documents keep the old values under the old name, where the new config does
  not read them.
- PostgreSQL never drops a column in development, so schema sync stays paused, and the question
  comes back on each save, until a reviewed migration removes the old column or you restore the
  old name.
- MongoDB carries on unless the removed field was indexed, which needs a reviewed migration.

To take back an `n` on SQLite or MongoDB, restore the old name, answer `n` to the reverse rename
`ridu dev` then detects, and rename again. Declining only some of several renames would drop the
declined fields' data in the same migration, so `ridu dev` refuses that combination; write it as a
reviewed migration, or make the changes in separate saves.

#### Without a terminal

Under a task runner or a coding agent `ridu dev` cannot ask. It rejects the reload, and every
later save, with the commands to run. Running `ridu generate` does not release it. It carries on
once either of these is true:

- The old name is restored.
- The rename has been migrated by hand: `ridu migrate create`, `ridu migrate baseline`, then
  `ridu migrate up`, with `--allow-maintenance` on PostgreSQL and MongoDB. The next save reloads.

If the removed and the added field really are unrelated, make the two changes in separate saves.

#### What `ridu dev` leaves to `ridu migrate`

Even with a terminal, these cases go through `ridu migrate create` and `ridu migrate up`:

- The project's migrations run compiled data transforms, which only the project binary can replay.
- A pending migration removes data, or the newest migration already reaches the new config
  without recording the rename.

If applying the rename fails, the migration files it wrote stay and the message says how to
continue. If the reload fails after the rename was applied, for example because application code
still uses the old generated field name, `ridu dev` keeps watching with no server running and
reloads on the save that fixes it. Deleting the migration afterwards does not undo a rename that
was applied.

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
- **`--allow-maintenance` on `up`** admits a traffic-sensitive phase after every old
  application process, writer, and worker has stopped. Keep them stopped through retries until
  `status` completes. `verify` never needs it: it replays into a private shadow schema or database
  that no application can reach, so it admits those phases itself.
- **`--allow-insecure-database` on database-backed commands** permits plaintext or bypassed
  certificate verification to a remote PostgreSQL or MongoDB development host. A loopback or
  Unix-socket database never needs it. The offline
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

MongoDB planner contract `3.0.0` artifacts use that same immutable format-`1` envelope. The
runner authenticates every committed artifact against the planner, takes a fenced, expiring lease,
records completed steps durably, recognizes already-completed physical work, and resumes the same
artifact after an interrupted process. It never treats process exit or
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
   is scoped to that command. The shadow database has no traffic, so verification needs no
   `--allow-maintenance`.
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
