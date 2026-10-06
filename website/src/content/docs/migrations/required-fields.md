---
title: 'Required fields'
description: 'Make a field required when documents already exist: how the migration audits stored values, what counts as missing, and how to backfill them.'
product: data
eyebrow: 'Migrations'
order: 228
navigation:
  section: 'Develop & operate'
  parent: migrations
  order: 30
  title: 'Required fields'
---

Documents saved before a field became required may have no value for it. Ridu checks them when the
schema changes, so a missing value stops the migration instead of breaking reads later.

A field becomes required when you add `.Required()` to it, add a required field to a collection or
global that already has documents, require a child of an existing group, array row or block, or
change the kind of a required field.

## What the migration does {#audit}

`create` adds an audit step to the migration and warns you about it:

```text title="terminal"
$ ridu migrate create --name require-summary
warning  RIDU_REQUIRED_FIELD_AUDIT  require posts.summary; the
         migration audits stored documents after its data
         transforms and fails with RIDU_REQUIRED_VALUES_MISSING if
         any document that must be complete has no value
```

When `up` reaches the audit, it reads the stored documents and stops if any of them lacks a value:

```text title="ridu migrate up" diagram
1. change the schema
2. run the migration's data transforms, if it has any
3. audit the documents that must be complete
     posts/post_1   summary  ✓
     posts/post_4   summary  ✗ ──▶ stop: RIDU_REQUIRED_VALUES_MISSING
4. make the field required
```

The error names each field, its locale when it has one, how many documents lack a value, and up to
five of their IDs:

```text title="terminal"
RIDU_REQUIRED_VALUES_MISSING: stored documents have no value for
fields that become required: posts.summary in 1 document, for
example hello. …
```

`ridu dev` runs the same audit before it syncs. It keeps the current schema and lists the documents
to complete; fill them in, or keep the field optional, and save again.

## Fix missing values {#backfill}

Write the values before the field becomes required. Bind a compiled
[data transform](/docs/migrations/data-transforms/) that backfills them to the same migration. It
runs before the audit:

```sh title="terminal"
ridu migrate create --name require-summary \
	--transform backfill-summaries
```

Two cases work differently:

- **Versioned collections and globals.** A transform can't change their fields, so backfill them
  in an earlier data-only migration, then require the field in the next one.
- **PostgreSQL defaults.** PostgreSQL writes a top-level, untranslated field's `.Default(...)` into
  existing rows when it adds the column, so a required field with a default needs no transform
  there. SQLite and MongoDB apply defaults only to new documents.

Otherwise, keep the field optional.

## What counts as missing {#missing}

A value is missing when it is absent or `null`, an empty string for text, select, relationship and
upload fields, or an empty list for arrays, blocks, lists and multiple selections. That is the
same rule a save applies.

- **Translations.** A locale without a translation is optional, but an empty translation is
  missing, and so is a translated field with no translation in any locale. See
  [Localization](/docs/localization/).
- **Nested fields** are checked in every group, array row, block and translation that exists. An
  absent optional group has nothing to check.
- **Blocks.** A field required inside a block is one requirement, named like
  `block hero.heading`, checked in every stored `hero` block of every collection and global.
- **Plugin payloads**, such as the fields of a rich-text block, are not audited. Making one of those
  required needs a data transform.

## Which documents are audited {#documents}

| Resource                   | Audited                                                            |
| -------------------------- | ------------------------------------------------------------------ |
| Without drafts             | every document, including trashed ones                             |
| With drafts                | every published document, and its working copy unless it's a draft |
| Retained version snapshots | never; they are history                                            |

Drafts may stay incomplete; publishing validates them. A stored document that still lacks a
required value stays readable, with the field returned as `null`.

## How each database applies it {#databases}

- **SQLite** applies the migration in one transaction, so a failed audit rolls back everything.
  `ridu migrate down` audits a rollback that makes a field required again.
- **PostgreSQL** adds the column as nullable, runs the transforms, audits the rows under a lock
  that holds writers, then adds `NOT NULL`. The transforms, audit and constraint share one
  transaction, so a failed audit rolls them back together.
- **MongoDB** can't lock collections against old writers, so a migration with this audit needs
  `--allow-maintenance` on `up`. See [Deploy migrations](/docs/migrations/deploy/#maintenance).
