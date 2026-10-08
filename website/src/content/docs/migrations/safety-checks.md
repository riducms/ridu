---
title: 'Safety checks'
description: 'How Ridu classifies migration risks, which flags approve destructive or traffic-sensitive work, and what each safety code means.'
product: data
eyebrow: 'Migrations'
order: 231
navigation:
  section: 'Develop & operate'
  parent: migrations
  order: 60
  title: 'Safety checks'
---

`ridu migrate create` reviews every change before it writes a migration. Anything worth a second
look is recorded in the file's `risks`, with a level that decides what happens next:

| Level         | Meaning                            | What happens                                                    |
| ------------- | ---------------------------------- | --------------------------------------------------------------- |
| `notice`      | Context for whoever deploys it     | Recorded in the file.                                           |
| `warning`     | Deserves review before you deploy  | Recorded in the file and printed by `create`.                   |
| `destructive` | Can permanently remove stored data | `create` stops until you approve it with `--allow-destructive`. |

Some changes are refused outright, whatever the flags. Those report one of the
[safety codes](#codes) below.

## Approval flags {#flags}

Each flag approves one thing at one step. None of them silences a safety code.

| Flag                        | Used on                                    | What it approves                                                                                   |
| --------------------------- | ------------------------------------------ | -------------------------------------------------------------------------------------------------- |
| `--allow-destructive`       | `create`                                   | Writes a reviewed destructive change into the migration. It doesn't connect to a database.         |
| `--allow-destructive`       | SQLite `down`, `reset`, `refresh`, `fresh` | Rolls back or rebuilds the selected database.                                                      |
| `--allow-maintenance`       | `up`                                       | Runs traffic-sensitive steps. Only pass it after every old process, writer and worker has stopped. |
| `--allow-insecure-database` | database commands                          | Connects to a remote PostgreSQL or MongoDB development host without verified TLS.                  |
| `--allow-unbounded`         | `up`, `verify`                             | Allows a zero wait or timeout where one would otherwise apply.                                     |

`--allow-maintenance` is a promise that old writers are stopped, not a lock that stops them. Keep
them stopped through retries until `ridu migrate status` reports the history complete. `verify`
never needs it, because it replays into a private database no application can reach. Which steps
count as maintenance is covered in [Deploy migrations](/docs/migrations/deploy/#maintenance).

A loopback or Unix-socket database never needs `--allow-insecure-database`, and the offline
`create` command rejects it.

## Removing a collection or field {#removing}

On SQLite, migrations can only add to the schema, so removing a field or collection is refused:

```text title="terminal"
$ ridu migrate create --name remove-summary --allow-destructive
plan migration: SQLite artifact planner supports only additive
transitions; field "posts-summary" in collection "posts" was removed
```

On PostgreSQL and MongoDB, removing one is destructive. Removing a collection can need approval
twice: `--allow-destructive` when you create the migration, and `--allow-maintenance` when you apply
it, because the runner also purges the sessions, credentials, versions, tasks, preferences, locks
and reference-index entries the collection owns.

Removing an upload collection is refused even with `--allow-destructive`. A migration can't move or
delete the stored files, so do that first, as described under
`RIDU_UPLOAD_COLLECTION_REMOVAL_UNSAFE` below.

## Safety codes {#codes}

Codes are stable, so you can match them in CI policy and runbooks. Each one names a change that is
unsafe as written, not a check to work around.

<dl class="doc-option-list">
  <div>
    <dt><code>RIDU_MIGRATION_PLAN_MISMATCH</code></dt>
    <dd>The migration file no longer matches the plan Ridu rebuilds from its contents. Restore the reviewed file or create it again; don't apply the changed one.</dd>
  </div>
  <div>
    <dt><code>RIDU_REQUIRED_VALUES_MISSING</code></dt>
    <dd>Stored documents have no value for a field that becomes required. Backfill them with a data transform, or keep the field optional. See <a href="/docs/migrations/required-fields/">Required fields</a>.</dd>
  </div>
  <div>
    <dt><code>RIDU_VERSIONS_EXISTING_REQUIRED</code></dt>
    <dd>A collection or global starts keeping versions, and the migration doesn't say what its stored documents become. Pass <code>--versions-existing=published</code>, <code>draft</code> or <code>require-empty</code>, or answer the question in a terminal. See <a href="/docs/migrations/enable-versions/">Enabling versions</a>.</dd>
  </div>
  <div>
    <dt><code>RIDU_VERSIONS_ENABLE_NOT_EMPTY</code></dt>
    <dd>A migration that enables versions with <code>require-empty</code> found stored documents. See <a href="/docs/migrations/enable-versions/#not-empty">When require-empty stops</a>.</dd>
  </div>
  <div>
    <dt><code>RIDU_VERSIONS_EXISTING_DOCUMENTS</code></dt>
    <dd><code>ridu dev</code> would enable versions on a collection that stores documents without a decision about them. Save again in a terminal to choose, or create the migration with <code>--versions-existing</code>.</dd>
  </div>
  <div>
    <dt><code>RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE</code></dt>
    <dd>Narrowing a relationship's field, target or cardinality could leave dormant references in current values or version snapshots. Keep the shape, retire the whole owning resource, or write an application-owned cleanup first.</dd>
  </div>
  <div>
    <dt><code>RIDU_REFERENCE_RENAME_MAPPING_AMBIGUOUS</code></dt>
    <dd>A source or destination field is mapped more than once. Split the change into unambiguous migrations.</dd>
  </div>
  <div>
    <dt><code>RIDU_COLLECTION_SLUG_REWRITE_OVERLAP_UNSAFE</code></dt>
    <dd>A chain or swap of renames could rewrite one value twice. Go through a unique temporary slug, in separate reviewed migrations. See <a href="/docs/migrations/renames/#limits">Renames</a>.</dd>
  </div>
  <div>
    <dt><code>RIDU_UPLOAD_COLLECTION_REMOVAL_UNSAFE</code></dt>
    <dd>A database migration can't manage stored files. Move or delete them through upload operations, reconcile the result, and verify matching backups before removing the collection.</dd>
  </div>
  <div>
    <dt><code>RIDU_AUTH_DISABLE_STATE_UNSAFE</code> and the other <code>*_DISABLE_STATE_UNSAFE</code> codes</dt>
    <dd>Turning off auth, API keys, password reset, email verification, versions, drafts, document locks or trash would leave dormant state that could come back later. Keep the capability enabled, or add the cleanup the finding names.</dd>
  </div>
  <div>
    <dt><code>RIDU_RETIRE_DEPENDENT_VERSION_HISTORY</code></dt>
    <dd>Retiring a resource also deletes the version history of documents that depend on it. Include that loss in your backup, review and sign-off.</dd>
  </div>
</dl>

[Troubleshooting](/docs/troubleshooting/#migration-refused) lists more failures by symptom.
