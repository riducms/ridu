<!-- Generated from website/src/content/docs/migrations/renames.md by scripts/sync-agent-docs.ts. -->

# Renames

To a database, renaming `title` to `headline` looks like removing one field and adding another.
Ridu notices when a change could be a rename and asks you. Confirm it, and the migration moves the
stored values to the new name instead of losing them.

## Rename with `ridu migrate create` {#create}

Change the name in your config, then create a migration as usual:

```text title="terminal"
$ ridu migrate create --name rename-title
Detected field rename "posts".title -> "posts".headline.
Preserve its existing data as a rename? [Y/n] y
Accepted field rename "posts".title -> "posts".headline.
warning  RIDU_SQLITE_FIELD_RENAME  move stored posts.title
         content to headline in current documents and retained
         versions; stop application writers while the migration
         runs
Created migration
migrations/20261006123216.089250000_rename-title.ridu.json
```

```text title="What up does with a confirmed rename" diagram
 a stored posts document                 after ridu migrate up
┌──────────────────────────┐           ┌──────────────────────────┐
│ title:   "Hello world"   │ ────────▶ │ headline: "Hello world"  │
│ summary: "…"             │           │ summary:  "…"            │
└──────────────────────────┘           └──────────────────────────┘
 the same move happens in every retained version, and indexes and
 joins that name the field follow it
```

Without a terminal, `create` cannot ask and stops. Review the detected renames, then rerun it with
`--accept-renames`. Stop the application's writers while `up` applies a rename; an old process
could otherwise keep writing to the old name.

## What Ridu can rename {#scope}

- **Collection fields**, at any depth: root fields, localized fields, groups, array rows and
  blocks.
- **Collections**, on PostgreSQL and MongoDB. On SQLite, a collection rename needs a
  [data transform](./data-transforms.md).
- **Fields inside a block.** A block is defined once and placed in many fields, so a field renamed
  inside it is one rename, applied to every stored copy of that block in every collection and
  global:

  ```text title="terminal"
  Detected block field rename "hero".heading -> "hero".headline
  in every "hero" block.
  ```

Fields of a global are not offered as renames.

Depending on the database, a rename also carries tables, columns, indexes, constraints, version
history, auth state, task references, polymorphic relationships and declared plugin reference keys
with it.

## Renames that need more than one migration {#limits}

Ridu only proposes a rename when the mapping is unambiguous, and a rename may change nothing but the
name. Split these into separate migrations:

| Change                                                         | Do it as                                                 |
| -------------------------------------------------------------- | -------------------------------------------------------- |
| Rename a field and rename its children, or make it localized   | Rename in one migration, then make the other change      |
| Rename a blocks field and a field inside its blocks            | Two renames to confirm; they can share a migration       |
| Swap two collection slugs, or rename `a` → `b` while `b` → `c` | Go through a temporary slug, one migration per step      |
| On SQLite, rename fields and run a data transform              | One migration for the renames, another for the transform |

On SQLite, a rename migration stops before changing anything when a document already stores a
value under the new name. Apart from its renames, it must only add to the schema.

## Renames in `ridu dev` {#in-dev}

`ridu dev` compares each save with the schema it last synced. When the difference looks like a
rename, it syncs nothing until you settle it, because ordinary sync would leave the stored values
under the old name.

<details class="docs-disclosure">
<summary>In a terminal: answer the same question as create</summary>
<div class="docs-disclosure-body">

`ridu dev` asks the same question as `create`, and needs an explicit `y` or `n`.

**Answer `y`** and `ridu dev` makes the change for you and for production:

```text title="ridu dev, after you answer y" diagram
1. write the rename migration to migrations/
   (and a changes-before-<name> migration first, if earlier
   synced changes have no migration yet)
2. stop the running server, which still uses the old name
3. apply the rename to the development database
4. start the server on the new config
```

PostgreSQL and MongoDB first record the migrations the database already has, then apply the rename
through the migration runner. SQLite moves the values directly and keeps syncing; a SQLite
database that `ridu migrate` already manages goes through the runner instead.

**Answer `n`** and nothing is written. The old values stay where they are:

| Database   | What happens next                                                                                                                    |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| SQLite     | Sync carries on. Documents keep the old values under the old name, where the new config doesn't read them.                           |
| PostgreSQL | Sync stays paused, and the question returns on each save, until a reviewed migration removes the old column or you restore the name. |
| MongoDB    | Sync carries on, unless the old field was indexed; that needs a reviewed migration.                                                  |

To take back an `n` on SQLite or MongoDB, restore the old name, answer `n` to the reverse rename
`ridu dev` then detects, and rename again. You can't decline only some of several renames in one
save, because the declined fields' data would be dropped; make them separate saves, or write a
reviewed migration.

</div>
</details>

<details class="docs-disclosure">
<summary>Under a task runner or coding agent: run the commands yourself</summary>
<div class="docs-disclosure-body">

Without a terminal, `ridu dev` can't ask. It rejects the reload, and every later save, and prints
the commands to run. It carries on once either is true:

- the old name is restored; or
- the rename has been migrated by hand:

```sh title="terminal"
ridu migrate create --name rename-title --accept-renames
ridu migrate baseline
# On PostgreSQL and MongoDB, add --allow-maintenance.
ridu migrate up
```

If the removed and added fields really are unrelated, make the two changes in separate saves.
Running `ridu generate` does not release the pause.

</div>
</details>

Even with a terminal, `ridu dev` leaves two cases to `ridu migrate create` and `up`: a project
whose migrations run compiled data transforms, which only the project binary can replay, and a
pending migration that removes data or already reaches the new config without the rename.

If applying the rename fails, the migration files stay and the message says how to continue. If the
server fails to reload afterwards, for example because application code still uses the old
generated field name, `ridu dev` keeps watching and reloads on the save that fixes it. Deleting the
migration does not undo a rename that was applied.
