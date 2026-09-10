---
title: 'Bulk actions and trash'
description: 'Change a reviewed set atomically, recover soft-deleted documents, and permanently delete content.'
product: admin
eyebrow: 'Admin tasks'
order: 124
aliases:
  [
    'bulk edit',
    'bulk publish',
    'bulk delete',
    'trash',
    'soft delete',
    'restore deleted',
    'empty trash'
  ]
navigation:
  section: 'Admin & workflows'
  parent: admin
  group: 'Find and organize'
  order: 30
  title: 'Bulk actions & trash'
---

Use a bulk action when the same reviewed change belongs on several documents. Enable trash when an
ordinary delete should be recoverable.

## Enable recoverable deletion {#enable-trash}

Trash is configured per collection:

```go title="content/posts.go"
ridu.Collection{
	Slug:  "posts",
	Trash: true,
	Fields: field.Fields{
		field.Text("title").Required(),
	},
}
```

Save the change with `ridu dev` running. It regenerates contracts, safely synchronizes the
development store, and refreshes the admin. Deleting a document now moves it out of ordinary reads
and into the collection's **Trash** workspace. A collection without `Trash: true` hard-deletes
instead. Before deployment, create, review, and verify the selected adapter's migration.

## Change a reviewed selection {#bulk-actions}

In a collection list, select rows or choose all results matching the current authorized filters.
The available actions depend on collection configuration and the current actor:

- **Edit** applies one partial value set;
- **Publish** and **Unpublish** run their dedicated version lifecycles;
- **Delete** moves documents to trash when enabled, otherwise it permanently deletes them;
- **Restore** returns selected trashed documents to the active collection; and
- **Delete permanently** removes selected trashed documents.

One request accepts 1–100 unique IDs. Ridu authorizes and validates every target, locks the set, runs
the per-document hooks, and commits all results together. If one document fails, the whole
batch rolls back. Narrow a filter when **Select all** resolves more than 100 documents.

The generated SDK exposes the same operations:

```ts title="review-posts.ts"
const selected = ['post_01', 'post_02'] as const;

await ridu.bulkUpdate('posts', selected, { category: 'news' });
await ridu.bulkPublish('posts', selected);
await ridu.bulkDelete('posts', selected);

const deleted = await ridu.list('posts', { trash: true });
await ridu.bulkRestoreDeleted(
	'posts',
	deleted.docs.map((post) => post.id)
);
```

The admin has no generic bulk create/import action. Use a purpose-built task or import program with
idempotency, progress, and error handling.

## Restore a document {#restore}

Open **Trash** from the collection header, select a document, and choose **Restore**. Restore runs
current access, validation, relationship, uniqueness, field, and hook rules; it can fail when the
old values no longer satisfy today's model.

```ts
await ridu.restoreDeleted('posts', post.id);
```

The document returns to ordinary list/API reads only after the restore commits. Its retained upload
objects remain available throughout trash and restore.

## Permanently delete content {#permanent-delete}

`deletePermanent`, `bulkDeletePermanent`, and **Empty trash** cannot be undone by Ridu. They apply
reference restrict/nullify behavior and remove related sessions, credentials, versions, tasks,
preferences, locks, and reference state. Upload object cleanup runs after the database commit so a
failed transaction cannot remove bytes still referenced by content.

Prefer selected permanent deletion when the target set needs review. **Empty trash** is a confirmed,
collection-wide action, but it is still bounded to 100 accessible documents per atomic request. If
there are more, delete in reviewed batches.

```ts
await ridu.deletePermanent('posts', post.id);
await ridu.bulkDeletePermanent('posts', selected);
await ridu.emptyTrash('posts');
```

Back up the database and object storage as one recovery point before a large cleanup. Historical
version snapshots are separate records and are not rewritten merely because a referenced target is
deleted.

## Troubleshooting {#troubleshooting}

| Symptom                                  | What to check                                                                                                         |
| ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Trash is missing from the collection     | Confirm `Trash: true` and keep `ridu dev` running so the manifest and safe additive development schema are refreshed. |
| A bulk action is absent                  | Check the resource capability, document state, and current actor's evaluated access.                                  |
| Select all reports too many documents    | Add a narrower search/filter. Atomic selection and mutation are limited to 100 unique IDs.                            |
| Restore returns a validation error       | The trashed values no longer pass current schema, uniqueness, relationship, localization, or field-access rules.      |
| Permanent delete is denied or restricted | Check `Delete` access and references whose delete policy prevents removal.                                            |

Continue with [Browse and organize content](/docs/browsing-content/),
[Drafts and versions](/docs/drafts-and-versions/), and [Uploads and media](/docs/uploads/). Exact
methods are documented under [`RiduClient.bulkUpdate`](/reference/sdk/ridu-client-bulk-update/),
[`RiduClient.bulkRestoreDeleted`](/reference/sdk/ridu-client-bulk-restore-deleted/), and
[`LocalAPI.BulkDelete`](/reference/core/local-api-bulk-delete/).
