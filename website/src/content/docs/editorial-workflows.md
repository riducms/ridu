---
title: 'Editorial workflows'
description: 'Choose the task guide for duplication, bulk and trash actions, document locks, preferences, drafts, versions, and scheduling.'
product: admin
eyebrow: 'Authoring'
order: 115
aliases:
  [
    'trash',
    'soft delete',
    'duplicate',
    'bulk',
    'document lock',
    'takeover',
    'preferences',
    'saved view'
  ]
navigation:
  section: 'Admin & workflows'
  parent: 'admin'
  order: 20
  title: 'Editorial workflows'
---

Start with the authoring task you need:

| Task                                | Guide                                                                   |
| ----------------------------------- | ----------------------------------------------------------------------- |
| Find, filter, sort, and select work | [Browse and organize content](/docs/browsing-content/)                  |
| Reopen a useful list workspace      | [Saved views, folders, and hierarchy](/docs/saved-views-and-hierarchy/) |
| Use the schema-driven form          | [Create and edit documents](/docs/editing-documents/)                   |
| Change many records or recover one  | [Bulk actions and trash](/docs/bulk-and-trash/)                         |
| Coordinate concurrent editors       | [Document locks](/docs/document-locks/)                                 |
| Publish, restore, or schedule       | [Drafts and versions](/docs/drafts-and-versions/)                       |
| Translate content                   | [Localization](/docs/localization/)                                     |
| Preview unsaved changes             | [Live preview](/guides/live-preview/)                                   |

## Duplicate a document {#duplicate}

Duplicate runs a create lifecycle from an authorized source snapshot and accepts create-style
overrides:

```ts
const copy = await client.duplicate('posts', original.id, {
	title: `${original.title} (copy)`,
	slug: 'hello-ridu-copy'
});
```

Ridu checks source read access, redacts denied fields, validates the new input, applies create access
and hooks, assigns a new ID/timestamps, and writes in one transaction. Unique values usually need an
override. Localized documents retain their locale data according to the duplicate contract; use
the dedicated [copy-locale operation](/docs/localization/#copy-locale) to translate one existing
document in place.

## Run atomic bulk actions {#bulk}

Bulk edit, publish, unpublish, delete, trash restore, and permanent delete accept a bounded reviewed
selection and commit atomically. Follow [Bulk actions and trash](/docs/bulk-and-trash/#bulk-actions)
for admin steps, SDK examples, the 100-document limit, and rollback behavior.

## Enable trash {#enable-trash}

Set `Trash: true` on a collection when normal deletion should be recoverable. With `ridu dev`
running, saving that change regenerates contracts and safely prepares the local development store;
create the reviewed migration before deployment. [Bulk actions and trash](/docs/bulk-and-trash/#enable-trash) covers the active/trash
workspaces, restore behavior, and the difference between soft and permanent deletion.

## Permanently delete {#permanent-delete}

Permanent deletion and **Empty trash** are destructive, bounded operations. Read
[Permanently delete content](/docs/bulk-and-trash/#permanent-delete) before using them with uploads,
references, or retained version history.

## Coordinate editors with document locks {#locks}

Enable expiring authoring leases with `LockDocuments: true`. [Document locks](/docs/document-locks/)
explains refresh, read-only behavior, separately authorized takeover, SDK integration, and why
`_revision` remains the final write fence.

## Persist author preferences {#preferences}

Authenticated users have namespaced JSON preferences for saved list views, columns, navigation
state, locale choice, and application/plugin UI:

```ts
await client.setPreference('posts:list:default', {
	columns: ['title', 'status', 'updatedAt'],
	sort: ['-updatedAt']
});

const view = await client.preference('posts:list:default');
await client.deletePreference('posts:list:default');
```

Keys are 1–200 characters and use lowercase letters, numbers, `:`, `.`, `_`, or `-`. Preference
ownership includes the exact auth collection as well as user ID, so identical document IDs in two
auth collections cannot share state accidentally. `resetPreferences` removes every preference for
the current identity.

Preferences are convenience state, never authorization. A saved filter or hidden column cannot
expand the current user's capabilities.

## Ask what the actor can do {#capabilities}

Use collection/global capability endpoints to enable an action before the user clicks it, and
`resolveFilteredSelection` to turn the current authorized filter into a bounded bulk target set.
Capability responses contain booleans and non-secret presentation information; they never serialize
access callbacks or filtered predicates.

Always handle a denied operation anyway. State or access can change between capability evaluation
and mutation, and the server is the authority.

For revision history, drafts, publishing, restore, autosave, and scheduling, continue with
[Drafts and versions](/docs/drafts-and-versions/). See [The Ridu admin](/docs/admin/) for the full
authoring surface and [Access control](/docs/access-control/) for the rule matrix.
