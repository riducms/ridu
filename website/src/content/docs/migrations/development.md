---
title: 'Development databases'
description: 'How ridu dev keeps a development database in sync with your config, when ridu migrate takes over, and how to record or squash history before the first deployment.'
product: data
eyebrow: 'Migrations'
order: 226
navigation:
  section: 'Develop & operate'
  parent: migrations
  order: 10
  title: 'Development databases'
---

`ridu dev` syncs your development database straight from your Go config on every save. That keeps
you fast while you design, but it records no history. A deployed database only changes through
committed migrations. This page covers where the two meet.

## How `ridu dev` syncs each database {#sync}

|                               | SQLite                     | PostgreSQL                           | MongoDB                                                   |
| ----------------------------- | -------------------------- | ------------------------------------ | --------------------------------------------------------- |
| Development database          | `.ridu/development.sqlite` | the project's Docker service         | the project's development replica set                     |
| What a save can change        | safe changes               | additions only; never drops a column | safe changes; dropping an indexed field needs a migration |
| After `ridu migrate baseline` | `ridu migrate` takes over  | `ridu dev` keeps syncing             | `ridu dev` keeps syncing                                  |

On every database, `ridu dev` stops and asks before a save could lose data: a possible
[rename](/docs/migrations/renames/#in-dev), a [field kind change](/docs/migrations/field-kinds/)
with stored values, or a field that [becomes required](/docs/migrations/required-fields/) while
documents lack it.

Once a SQLite development database has migration history, `ridu dev` still serves it but stops
changing its schema. From then on a config change needs `ridu migrate create` and `ridu migrate up`:

```text title="A SQLite development database" diagram
┌──────────────────┐   ridu migrate baseline   ┌──────────────────┐
│    synced by     │ ────────────────────────▶ │    managed by    │
│     ridu dev     │                           │   ridu migrate   │
└──────────────────┘                           └──────────────────┘
 each save changes                              ridu dev serves it;
 the schema                                     schema changes need
                                                migrate create + up
```

## Record history with `baseline` {#baseline}

A database that `ridu dev` synced has the right tables but no record of which migrations it matches,
so `ridu migrate status` and `up` call it unmanaged. `baseline` records that history without
running any migration:

```sh title="terminal"
ridu migrate create --name add-post-summary
ridu migrate baseline
ridu migrate status
```

```text title="What baseline records" diagram
migrations/                          in the database
├─ …_initial.ridu.json               ✓ recorded as applied
├─ …_add-posts.ridu.json             ✓ recorded as applied
├─ …_add-post-summary.ridu.json      ✓ recorded as applied ◀ newest match
└─ …_rename-title.ridu.json            pending, for ridu migrate up
```

`baseline` finds the newest migration whose schema the database already has and records it and
everything before it. It stops early at a migration with work that sync never does, such as moving
renamed content, running a data transform, or rebuilding a reference index, and leaves that one for
`up`. This is how a rename that `ridu dev` paused gets applied: `baseline`, then `up`.

`baseline` changes nothing when the history is already current. It refuses a database whose schema
matches no committed migration, and one with a partly applied migration. PostgreSQL and MongoDB
keep syncing under `ridu dev` afterwards, so run `baseline` again after each `ridu migrate create`.

Production databases never need `baseline`. They are only ever created and changed by `up`.

## Squash history before the first deployment {#squash}

Until anything is deployed, you can replace your migration history with one new `initial`
migration:

```sh title="terminal"
mv migrations .ridu/previous-migrations
ridu migrate create --name initial
ridu migrate baseline --replace \
	--previous-history .ridu/previous-migrations
ridu migrate status
```

```text title="Squashing history" diagram
before                                after
migrations/                           migrations/
├─ …_initial.ridu.json                └─ …_initial.ridu.json   new
├─ …_add-posts.ridu.json      ─────▶
└─ …_add-post-summary.ridu.json       .ridu/previous-migrations/
                                      └─ the old files, kept so Ridu can
                                         check what the database recorded
```

`baseline --replace` swaps the database's recorded history for the new `initial`. It changes only
that record, never documents or version snapshots. Before you run it:

- **Bring the database up to date first.** It must already have the new `initial`'s schema. On
  PostgreSQL and MongoDB, let `ridu dev` sync your current config. On SQLite, run `ridu migrate up`
  on the old history before you move it.
- **Keep the old files.** They must match the filenames and checksums the database recorded. If
  they are gone, recover them from Git or a backup.
- **Keep migrations that ran semantic work.** If a migration ran a data transform or another step
  that changed content, keep it and every migration before it, and squash only the ones after it.
- **Stop the application.** An application built from the old history fails readiness after the
  replacement. When that could happen, Ridu asks `Replace the recorded history anyway? [y/N]`. Stop
  every process using the database before answering `y`. Without a terminal, pass
  `--allow-production` instead.

Replacement refuses schema drift and partly applied migrations. It also refuses to drop a
collection or global, because only a migration removes stored documents and versions; recreate
the development database with `ridu migrate up`, or remove the resource in a migration.

After the first deployment, keep history as it is and only add to it.
