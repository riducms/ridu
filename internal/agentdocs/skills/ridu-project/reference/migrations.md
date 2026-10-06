<!-- Generated from website/src/content/docs/migrations.md by scripts/sync-agent-docs.ts. -->

# Migrations

Your Go config is the source of truth for your content. When you add, rename, or remove a field or
collection, every database that stores that content has to change with it. Ridu does this in two
ways:

- **While you develop**, `ridu dev` syncs your development database to your config every time you
  save. Nothing is recorded.
- **When you ship**, `ridu migrate create` records the change as a migration file that you commit,
  and `ridu migrate up` applies those files, in order, to every other database.

```text title="How a schema change reaches each database" diagram
                       content/*.go
                       your schema
                            │
          ┌─────────────────┴──────────────────┐
          │ ridu dev                           │ ridu migrate create
          ▼                                    ▼
  ┌───────────────┐                    migrations/
  │  development  │                    ├─ …_initial.ridu.json
  │   database    │                    ├─ …_add-posts.ridu.json
  └───────────────┘                    └─ …_add-post-summary.ridu.json
  synced on every save,                         │
  no history                                    │ commit and deploy
                                                ▼
                                         ridu migrate up
                                                │
                                                ▼
                                       ┌─────────────────┐
                                       │   production    │
                                       │    database     │
                                       └─────────────────┘
                                       records each file
                                       it has applied
```

New projects start with an `initial` migration, so the first deployment already has a history to
apply.

## Commands {#commands}

| Command                                          | What it does                                                                  |
| ------------------------------------------------ | ----------------------------------------------------------------------------- |
| `ridu migrate create --name <name>`              | Writes one migration for everything that changed since the newest one.        |
| `ridu migrate verify`                            | Replays every migration into a throwaway database to prove the history works. |
| `ridu migrate plan`                              | Lists what a database still has to apply. Changes nothing.                    |
| `ridu migrate up`                                | Applies the pending migrations to a database.                                 |
| `ridu migrate status`                            | Shows what a database has applied and checks that it matches the files.       |
| `ridu migrate baseline`                          | Marks migrations as applied in a database that `ridu dev` already built.      |
| `ridu migrate down`, `reset`, `refresh`, `fresh` | SQLite only. Rolls back or rebuilds a database.                               |

`create` never connects to a database. The other commands work on the database named by
`RIDU_SQLITE_PATH` for SQLite, or `DATABASE_URL` for PostgreSQL and MongoDB. `verify` uses that
setting only to make its own throwaway copy: a temporary file on SQLite, or an isolated schema or
database on the PostgreSQL or MongoDB server.

## Choose your workflow {#workflows}

Pick the situation you are in.

<details class="docs-disclosure">
<summary>I'm building locally and want the database to follow my config</summary>
<div class="docs-disclosure-body">

Run `ridu dev`. It watches your Go files and syncs the development database on every save, so you
can try out a schema without writing migrations.

```text title="ridu dev" diagram
┌────────────┐
│ $ ridu dev │
└────────────┘
  on every save:
  1. run your Go config to resolve the schema
  2. compare it with the database's current schema
  3. stop and ask before a change could lose data
  4. sync the development database ──────▶ ┌──────────┐
  5. regenerate types and reload            │ DATABASE │
                                            └──────────┘
```

Step 3 covers renames, changes to a field's kind, and fields that become required. In a terminal,
`ridu dev` asks what to do; under a task runner or coding agent it keeps the last working server
running and prints the commands to run instead.

`ridu dev` writes no migration files, with one exception: when you confirm a rename, it writes the
rename migration so production makes the same change. How each database syncs, and when a
development database starts needing `ridu migrate`, is covered in
[Development databases](./migrations/development.md).

</div>
</details>

<details class="docs-disclosure">
<summary>I changed my schema and want to ship it</summary>
<div class="docs-disclosure-body">

Create a migration, read it, and check that the whole history still replays:

```sh title="terminal"
ridu migrate create --name add-post-summary
ridu migrate verify
```

```text title="What the two commands do" diagram
┌───────────────────────────────────────────────┐
│ $ ridu migrate create --name add-post-summary │
└───────────────────────────────────────────────┘
  1. run your Go config to resolve the schema
  2. compare it with the newest file in migrations/
  3. ask you to confirm any renames
  4. write the new migration, without a database
     migrations/
     ├─ 20261006123116.027664000_initial.ridu.json
     ├─ 20261006123142.346608000_add-posts.ridu.json
     └─ 20261006123230.079418000_add-post-summary.ridu.json

┌───────────────────────┐
│ $ ridu migrate verify │
└───────────────────────┘
  1. create a throwaway database
  2. apply every migration, starting from the first
  3. check that the result matches your config
  4. delete the throwaway database

[✓] Migration history replays cleanly in an isolated SQLite database.
```

Commit the migration with the config change and the regenerated contracts. If you forget to create
it, `ridu check` and `ridu build` stop with
`create and commit the missing migration`. When only labels or admin settings changed, `create`
writes nothing; see [Changes that need no migration](#presentation-changes).

</div>
</details>

<details class="docs-disclosure">
<summary>I'm deploying and need to update the production database</summary>
<div class="docs-disclosure-body">

Run `ridu migrate up` as a deployment step, before the new version of your application starts. The
server never migrates a production database by itself.

```sh title="terminal" group="migrate-up" tab="SQLite"
export RIDU_SQLITE_PATH='/var/lib/ridu/content.sqlite'
ridu migrate plan    # list what will run
ridu migrate up      # run it
ridu migrate status  # confirm everything is applied
```

```sh title="terminal" group="migrate-up" tab="PostgreSQL or MongoDB"
export DATABASE_URL='replace-with-the-production-url'
ridu migrate plan    # list what will run
ridu migrate up      # run it
ridu migrate status  # confirm everything is applied
```

```text title="What up does" diagram
┌───────────────────┐
│ $ ridu migrate up │
└───────────────────┘
  1. read the files in migrations/
  2. read which ones the database applied ─────▶ ┌──────────┐
  3. take the database's migration lock          │          │
  4. apply each pending file, in order ────────▶ │ DATABASE │
  5. record each file as applied ──────────────▶ │          │
                                                 └──────────┘
[✓] Migrations are current.
```

The binary that `ridu build` produces records which migrations it was built with. Its readiness
check, `/readyz`, fails until the database has applied exactly that history, so a release never
serves a database it doesn't match.

Plan the release order, maintenance windows and recovery in
[Deploy migrations](./migrations/deploy.md).

</div>
</details>

<details class="docs-disclosure">
<summary>ridu dev built my database, and now I want it to track migrations</summary>
<div class="docs-disclosure-body">

A database that `ridu dev` synced has the right tables but no record of which migrations it
matches. `baseline` adds that record without changing the schema or your content:

```sh title="terminal"
ridu migrate baseline
ridu migrate status
```

```text title="What baseline does" diagram
┌─────────────────────────┐
│ $ ridu migrate baseline │
└─────────────────────────┘
  1. read the files in migrations/
  2. find the newest one whose schema the database already has
  3. record it, and every file before it, as applied ──▶ DATABASE
  4. leave later files pending for `ridu migrate up`
```

To replace your history with a single new `initial` migration before your first deployment, see
[Squash history](./migrations/development.md#squash).

</div>
</details>

<details class="docs-disclosure">
<summary>I want to undo a migration</summary>
<div class="docs-disclosure-body">

On **SQLite**, roll back the latest applied migration. The file stays in `migrations/` and shows as
pending again, so `up` can reapply it:

```text title="terminal"
$ ridu migrate down --allow-destructive
Rolled back the latest applied migration.

$ ridu migrate status
applied   20261006123116.027664000_initial.ridu.json
applied   20261006123142.346608000_add-posts.ridu.json
applied   20261006123216.089250000_rename-title.ridu.json
pending   20261006123230.079418000_add-post-summary.ridu.json
```

| SQLite command | What it does                                                |
| -------------- | ----------------------------------------------------------- |
| `down`         | Rolls back the latest applied migration.                    |
| `reset`        | Rolls back every applied migration.                         |
| `refresh`      | Rolls back every migration, then applies them all again.    |
| `fresh`        | Drops the whole schema, then applies every migration again. |

Each one can remove data, so each needs `--allow-destructive`.

**PostgreSQL** and **MongoDB** have no down migrations. Create a new migration that reverses the
change, or restore the backup you took before running `up`.

Never edit or delete a migration file that any database has applied. Ridu checks every applied
file's checksum and refuses a changed history.

</div>
</details>

## How the databases differ {#databases}

The commands are the same everywhere. What happens underneath depends on the database:

|                                | SQLite                              | PostgreSQL                             | MongoDB                              |
| ------------------------------ | ----------------------------------- | -------------------------------------- | ------------------------------------ |
| Target database                | `RIDU_SQLITE_PATH`                  | `DATABASE_URL`                         | `DATABASE_URL`                       |
| `verify` replays into          | a temporary file                    | an isolated schema                     | a random database                    |
| An interrupted `up`            | rolls the whole migration back      | resumes after the last committed phase | resumes from its recorded steps      |
| Undo                           | `down`, `reset`, `refresh`, `fresh` | a new migration, or restore a backup   | a new migration, or restore a backup |
| Removing a field or collection | not supported by migrations         | allowed after you approve it           | allowed after you approve it         |

The adapter guides cover the rest: [SQLite](./sqlite.md#migrations),
[PostgreSQL](./postgres.md#migrations), and [MongoDB](./mongodb.md#migrations).

## Changes that need care {#careful-changes}

Adding a collection or an optional field is a plain `create` and `up`. These changes need a
decision from you first:

| When you…                     | Ridu…                                                     | Read                                                 |
| ----------------------------- | --------------------------------------------------------- | ---------------------------------------------------- |
| rename a field or collection  | asks whether it is a rename, then moves the stored values | [Renames](./migrations/renames.md)                 |
| make a field required         | checks stored documents and stops if any lack a value     | [Required fields](./migrations/required-fields.md) |
| change a field's kind         | pauses if stored values would not fit the new kind        | [Field kind changes](./migrations/field-kinds.md)  |
| need to rewrite stored values | runs your compiled data transform inside the migration    | [Data transforms](./migrations/data-transforms.md) |
| remove a field or collection  | refuses until you review the loss and approve it          | [Safety checks](./migrations/safety-checks.md)     |

### Changes that need no migration {#presentation-changes}

Settings that only change how the admin presents content never shape stored data, so they never
need a migration:

- the application name, admin languages and timezones, admin loaders, and custom endpoint paths
  and summaries;
- `CollectionAdmin`, `GlobalAdmin`, and field `Admin` settings, such as hiding a collection or
  moving it to another navigation group;
- labels of collections, globals, blocks, select options and content locales, and a content
  locale's text direction;
- select option order, number input steps, code-editor languages, row labels, and join default
  columns.

When nothing else changed, `create` says so and writes no file:

```text title="terminal"
$ ridu migrate create --name posts-admin-group
No migration needed: only presentation settings, such as
labels, admin settings or the application name, changed since
the latest migration, and history ignores them.
```

The next migration that does change the schema records the new settings too. Upgrading Ridu or a
plugin needs no migration by itself. Content locales, select option values, validation rules and
anything else that shapes stored data still do.

## What's in a migration file {#artifact}

A migration is a JSON file named after the time it was created and the name you gave it. You don't
need to edit it, but it is worth reading in review:

```text title="migrations/20261006123216.089250000_rename-title.ridu.json" diagram
name        "rename-title"
planner     ridu-sqlite 1.3.0     the adapter that wrote it
previous    fdf4c4…               the migration before this one
from        f42c8d…               schema digest before
to          bf6c8a…               schema digest after
after       { … }                 the complete schema after it runs
phases
└─ phase-001                      runs in one transaction
   ├─ step-0001  rename_content   rename posts.title to headline
   └─ step-0002  assert_schema    verify resulting SQLite schema
risks
└─ warning  RIDU_SQLITE_FIELD_RENAME
            move stored posts.title content to headline in current
            documents and retained versions; stop application writers
            while the migration runs
```

Each file points at the one before it, so the files form a chain. `up` and `status` check that
chain, and each applied file's checksum, against the database's record. A renamed, reordered, or
edited file is refused. To fix a mistake after a migration has run anywhere, create a new one.

`risks` is where `create` tells you what to review. A `notice` or `warning` is written into the file
for you to read. A `destructive` risk stops `create` until you approve it; see
[Safety checks](./migrations/safety-checks.md).

## Next steps {#next}

- [Development databases](./migrations/development.md): how `ridu dev` syncs each database,
  `baseline`, and squashing history before the first deployment.
- [Renames](./migrations/renames.md): keep content when a field or collection changes name.
- [Required fields](./migrations/required-fields.md): require a field when documents already
  exist.
- [Field kind changes](./migrations/field-kinds.md): change a text field to a number, or blocks
  to rich text.
- [Data transforms](./migrations/data-transforms.md): rewrite stored values inside a migration.
- [Safety checks](./migrations/safety-checks.md): approval flags and the safety codes `create`
  and `up` report.
- [Deploy migrations](./migrations/deploy.md): release order, maintenance, interruption and
  recovery.
