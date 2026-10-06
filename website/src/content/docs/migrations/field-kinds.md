---
title: 'Field kind changes'
description: 'Change a field from text to number, blocks to rich text, or one select to another: which changes are safe, what ridu dev does with stored values, and how to keep them.'
product: data
eyebrow: 'Migrations'
order: 229
navigation:
  section: 'Develop & operate'
  parent: migrations
  order: 40
  title: 'Field kind changes'
---

Changing a field's kind, such as text to number, blocks to rich text, relationship to upload, or a
single select to a multiple select, can leave stored values the new kind can't read. Ridu checks
before it changes anything.

```text title="When you change a field's kind" diagram
            change a field's kind
                      │
                      ▼
     would every stored value still fit? ── yes ──▶ sync or migrate
                      │                             as usual
                      no
                      ▼
          ridu dev pauses and shows how many
          documents and versions hold values
                      │
         ┌────────────┴────────────┐
         ▼                         ▼
      cancel                     clear
  keep the old kind        remove the values,
  and every value          when clearing is offered
```

## Changes that need no review {#safe}

Kinds that store the same plain string need no review while every stored value stays valid:

| From                                             | To                                           |
| ------------------------------------------------ | -------------------------------------------- |
| text, textarea, code, email, date, select, radio | text, textarea, code                         |
| select or radio                                  | another select or radio with all its options |

The reverse direction is reviewed: an arbitrary text value may not be a valid option, email address
or date.

## What `ridu dev` checks {#dev}

`ridu dev` counts stored values in current documents, including trash, and in every retained version
snapshot, before it regenerates contracts or reloads. When values exist, it pauses and shows the
counts. Under a task runner or coding agent, it rejects the reload and keeps the last working server
running.

- **Blocks.** A kind change inside a block is reviewed once, as `block hero.score`, and covers every
  stored `hero` block in every collection and global that places it.
- **Plugin payloads.** A kind change inside a plugin field, such as a rich-text block's field, is
  reviewed at the plugin field. Ridu offers no clearing there, because clearing would remove the
  whole rich-text value; restore the previous kind instead.

In a terminal you can **cancel**, which keeps the current schema and every stored value, or
**clear**, when it is offered.

<details class="docs-disclosure">
<summary>When clearing is offered, and what it removes</summary>
<div class="docs-disclosure-body">

Clearing is offered for ordinary stored fields in a development database without applied migration
history. It asks for a second confirmation, stops the old server, checks the counts again inside
the transaction, and removes the field's values from current documents and **every retained
snapshot**. Other fields and version metadata stay. If clearing fails, nothing is removed and the
old server restarts.

It is not offered for a database `ridu migrate` manages, an auth or upload resource, a localization
change, a plugin payload change, a save that combines the change with a rename, or a top-level
field that stays required. To clear a required field, make it optional for the clearing save, then
require it again after entering values.

If a later build or sync fails after a confirmed clear, the values are already gone. The server
stays stopped while `ridu dev` waits for a save that fixes the problem.

</div>
</details>

## Keep the values {#keep}

Ridu has no built-in conversion, and it won't guess how your block definitions map to a rich-text
document. To keep the values:

1. Restore the field's previous kind.
2. Add a new field with the new kind.
3. Copy the values into it with a [data transform](/docs/migrations/data-transforms/).

Transforms can't yet change fields of versioned resources or narrow stored reference shapes. Those
changes need an application-owned recovery that also handles retained snapshots.

## In a migration {#migrate}

`ridu migrate create` refuses a schema-only kind change, even with `--allow-destructive`. With
`--transform`, PostgreSQL, SQLite and MongoDB accept a kind change of an unversioned resource.
PostgreSQL changes the column before the transform runs, so it also refuses a column type it can't
convert in place, such as text to number; add a new field and copy the values instead.

## When Ridu doesn't know the stored schema {#unknown-schema}

Ridu records the schema a database was synced or migrated to only after it succeeds. Generated
files and the `.ridu/` cache can't stand in for it, so deleting the cache or running
`ridu generate` does not skip the review. A new, empty database initializes normally.

A database with application tables, collections or migration history but no schema record stops
`ridu dev` with `RIDU_DEVELOPMENT_SCHEMA_UNKNOWN`. On PostgreSQL and MongoDB, `ridu migrate up` also
refuses pending work when applied history has no schema record, because kinds stored as the same
JSON look identical in tables and indexes. Restore a backup that includes the schema record, or
keep the content you need and start a new development database. `baseline` can't recover an
unknown schema just because the storage looks the same.

`--no-sync` skips syncing, not this check. When the first inspection finds no values, Ridu drains
the old development server and checks again before generating. Stop any other process using the
database before an incompatible change too.
