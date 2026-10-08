---
title: 'Enabling versions'
description: 'Turn on versions or drafts for a collection or global that already stores documents, and choose whether those documents stay published or become drafts.'
product: data
eyebrow: 'Migrations'
order: 229
navigation:
  section: 'Develop & operate'
  parent: migrations
  order: 45
  title: 'Enabling versions'
---

You can set `Versions: true` on a collection or global that already exists. Its stored documents
have no publication state or version history yet, so Ridu asks what they become. A migration records
your answer, so every database gets the same result.

```text title="When a collection starts keeping versions" diagram
          set Versions: true
                  │
                  ▼
     does it store any documents? ── no ──▶ sync or migrate
                  │                         as usual
                 yes
                  ▼
     choose what the documents become
                  │
      ┌───────────┼──────────────┐
      ▼           ▼              ▼
  published     draft      require-empty
```

## The choices {#choices}

| Choice          | What happens to each stored document, trashed ones included                                                                                                         |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `published`     | It is published with a first version and a matching draft, so everyone who could read it still can. This is the recommended choice.                                 |
| `draft`         | It stays as an unpublished draft with a first version. Public reads stop returning it until someone publishes it. Only available with `VersionConfig.Drafts: true`. |
| `require-empty` | Nothing is converted: `ridu migrate up` stops with `RIDU_VERSIONS_ENABLE_NOT_EMPTY` while the collection stores any document.                                       |

A collection with its own `status` select, like the starter's `posts`, still needs a choice. Ridu
doesn't read that field. Choose `draft` and publish the posts you want public, or choose `published`
and unpublish the rest.

## In `ridu dev` {#in-dev}

When the development database stores no documents for the collection, `ridu dev` enables versions
without asking. Otherwise it stops before regenerating and asks what the stored documents become:

```text title="terminal"
Collection "posts" starts keeping versions and stores 3 documents here. What do they become?
  published  publish each one with a matching draft, so readers keep seeing it (recommended)
  draft      keep each one as an unpublished draft that readers no longer see until it is published
  cancel     keep the current schema running and change nothing
Choose published, draft or cancel: published
Migration name [enable-versions-posts]:
```

Like an accepted [rename](/docs/migrations/renames/#in-dev), your answer becomes a migration in the
migrations directory, and `ridu dev` applies it to the development database. Another collection that
starts keeping versions in the same save but stores nothing here gets `require-empty`.

Under a task runner or coding agent, `ridu dev` can't ask. It rejects the reload with
`RIDU_VERSIONS_EXISTING_DOCUMENTS` and keeps the last working server running. Cancel does the same.
Save the change again in a terminal, or create the migration yourself.

## In a migration {#migrate}

`ridu migrate create` doesn't know which documents your other databases store, so it always asks
when a collection starts keeping versions. Pass the answer with `--versions-existing` in scripts:

```sh title="terminal"
ridu migrate create enable-versions --versions-existing=published
```

The flag applies to every collection and global that starts keeping versions in the migration.
Without the flag and without a terminal, `create` stops with `RIDU_VERSIONS_EXISTING_REQUIRED`.

`ridu migrate up` publishes or keeps drafts on every stored document in the migration's
transaction. `ridu migrate verify` replays it like any other step. On PostgreSQL and MongoDB, `up`
needs `--allow-maintenance`: stop the application first, so nothing stores a document while the
collection changes. SQLite and PostgreSQL check `require-empty` before they change the schema, so a
refused migration changes nothing.

`ridu migrate baseline` can record a `require-empty` migration on a database that `ridu dev`
synced, because syncing enables versions in the same way. It can't record one that publishes or
keeps drafts. That migration has to run on a database that migrations manage.

Enable versions in a migration of its own. It can't share one with renames or data transforms.

<details class="docs-disclosure">
<summary>Rolling back on SQLite</summary>
<div class="docs-disclosure-body">

`ridu migrate down` turns the collection back into an unversioned one. Each document keeps its
latest working content, including unpublished draft edits, and loses its versions and published
copy. Make sure that is the content you want before rolling back a migration that has been live.

</div>
</details>

## When `require-empty` stops {#not-empty}

`RIDU_VERSIONS_ENABLE_NOT_EMPTY` means the database stores documents the migration has no answer
for. If no database has applied the migration yet, delete it and create it again with
`--versions-existing=published` or `draft`. Otherwise remove the documents from the database that
refused it, then run `ridu migrate up` again.
