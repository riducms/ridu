---
title: 'Drafts and versions'
description: 'Keep bounded revision history, control draft visibility, restore snapshots, publish changes, and schedule collection changes.'
product: admin
eyebrow: 'Authoring'
order: 120
navigation:
  section: 'Admin & workflows'
  parent: 'admin'
  order: 30
  title: 'Drafts & versions'
---

Versions and drafts are related but separate. `Versions: true` records immutable snapshots and
adds optimistic revisions. `VersionConfig.Drafts: true` makes unpublished status part of the
ordinary authoring workflow. You can keep history without enabling draft creation.

## Enable revision history {#enable-versions}

```go title="content/posts.go"
package content

import (
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Posts = ridu.Collection{
	Slug:     "posts",
	Versions: true,
	VersionConfig: ridu.VersionConfig{
		Drafts:           true,
		MaxPerDocument:   100,
		AutosaveInterval: 30 * time.Second,
	},
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Textarea("summary"),
	},
}
```

Versioned output includes `_revision` and `_status` (`draft` or `published`). The Go store model
exposes the same values as `Document.Revision` and `Document.Status`.

| Setting            | Current behaviour                                                                                                                                         |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Drafts`           | Defaults false. When true, ordinary creates default to draft and draft-specific create/update/restore behaviour is enabled.                               |
| `MaxPerDocument`   | Defaults to 100. Every mutation prunes the oldest snapshots beyond this positive per-document limit.                                                      |
| `AutosaveInterval` | Defaults to 30 seconds when zero; a configured value must be at least one second. It controls the admin's draft-save and published-edit checkpoint timer. |

The server does not mutate documents on a timer. For a dirty draft, autosave is the admin calling
the same update endpoint, so access, validation, hooks, revisions, and conflicts still apply. A
dirty published document is checkpointed in browser storage instead: a background update would
make those edits public. Reload restores that checkpoint until the author chooses
**Publish changes**. Zero currently selects the 30-second default rather than disabling this timer.

Every accepted create, duplicate, update, publish, unpublish, and restore saves the canonical
stored document in the same transaction. Reads and delete/trash operations do not create
versions. Relationship population, computed output, and field redaction are response shapes and
are not copied into the stored snapshot.

## Draft write semantics {#draft-writes}

`MutationOptions.Draft` has three states:

| Configuration/request                           | Create                                   | Existing update                                          |
| ----------------------------------------------- | ---------------------------------------- | -------------------------------------------------------- |
| Versions disabled                               | Ordinary unversioned document            | Ordinary unversioned update                              |
| Versions enabled, drafts disabled, `Draft: nil` | Published                                | Rejected for the published row; use `PublishChanges`     |
| Drafts enabled, `Draft: nil`                    | Draft                                    | Updates a draft; published rows require `PublishChanges` |
| `Draft: &true`                                  | Draft; rejected when drafts are disabled | Rejected; use the unpublish lifecycle                    |
| `Draft: &false`                                 | Published                                | Rejected; use the publish lifecycle                      |

In REST/SDK calls, the equivalent write option is `draft: true` or `draft: false`. Publishing and
unpublishing are clearer lifecycle operations than changing status as part of an unrelated update,
because they run the publish/unpublish hook operation and are easier to audit.

The low-level Go and REST version surface is present on any versioned resource. Treat
publish/unpublish as a draft workflow and configure `Drafts: true`; restore-as-draft rejects a
versioned resource whose draft support is disabled, and the admin/GraphQL draft actions
are driven by that setting.

## Draft read semantics {#draft-reads}

The Local Go API’s `FindOptions.Draft` and `ListOptions.Draft` are also three-state:

| Read option                   | Result set                                                         |
| ----------------------------- | ------------------------------------------------------------------ |
| `Draft: &false`               | Published documents only                                           |
| `Draft: &true`                | Published and draft documents; this is not “drafts only”           |
| `Draft: nil`, anonymous actor | Published only                                                     |
| `Draft: nil`, non-nil actor   | No status filter; collection/field access still decides visibility |

Ordinary REST and generated SDK reads use the actor-sensitive default above: anonymous reads see
published documents, while authenticated reads may include drafts when access allows it. These
read routes do not accept a `draft` option; REST rejects `?draft=true`. The SDK's `draft` option on
supported writes does not enable draft reads.

For a published-only REST or SDK collection list, filter on the version metadata:

```ts
const page = await client.list('posts', {
	where: { _status: { equals: 'published' } }
});
```

For Local Go reads, set `Draft` explicitly at public-rendering, preview, and background-job
boundaries. The GraphQL plugin also exposes explicit draft selection. Requesting drafts through
these surfaces can include them even for an anonymous actor if the authored Read rule allows it.
To enforce published-only anonymous access across ordinary read surfaces, assign a predicate rule
to `Posts.Access.Read`, for example:

```go title="content/access.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
)

func readPosts(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor != nil {
		return ridu.Allow(), nil
	}
	statusPath, err := query.NewPath("_status")
	if err != nil {
		return ridu.Deny(), err
	}
	return ridu.Where(
		query.Equal(statusPath, query.String("published")),
	), nil
}
```

Set `Access: ridu.CollectionAccess{Read: readPosts}` on the collection. This example treats every
authenticated actor as an editor; replace that branch with your application's editorial access
rule. See [Access control](/docs/access-control/) for role and ownership predicates.

A draft is not a security boundary. Collection and field access always run, and requesting draft
content does not grant permission. Preview tokens provide a separate, short-lived, target-scoped
read path for a configured live preview; they do not weaken normal collection routes. See
[Editorial workflows](/docs/editorial-workflows/).

## Publish and unpublish {#publish-unpublish}

Publish changes status to `published`; unpublish changes it to `draft`. Both are mutations: they
run their dedicated collection access rule (falling back to `Update` only when omitted), update
field access, validation, field and collection change/operation hooks, optimistic revision checks,
snapshotting, and after-commit work. `PublishChanges` and the SDK's `publishChanges` atomically
apply edited values through that same publish lifecycle instead of performing an ordinary update.
Body-bearing publish and unpublish operations require both `Update` and their dedicated lifecycle
permission. Status-only transitions require only the dedicated permission. An ordinary update of a
published versioned row returns `publish_required`; Ridu never silently turns a generic update into
a live publication.

```go
published, err := app.Local().Publish(ctx, "posts", post.ID, ridu.MutationOptions{ExpectedRevision: post.Revision, Actor: actor})
if err != nil {
	return err
}

draft, err := app.Local().Unpublish(ctx, "posts", post.ID, ridu.MutationOptions{ExpectedRevision: published.Revision, Actor: actor})
```

The generated TypeScript client exposes the same intent:

```ts
const published = await ridu.publish('posts', id, {
	revision: post._revision
});

const republished = await ridu.publishChanges(
	'posts',
	id,
	{ title: 'Edited and published atomically' },
	{ revision: published._revision }
);

await ridu.unpublish('posts', id, {
	revision: republished._revision
});
```

`ExpectedRevision`/`revision` is optional at the API level; zero or omission means no fence. For an
editor, always send the last observed positive revision. A stale revision returns `conflict` (409)
instead of overwriting a newer save.

## Read version history {#version-history}

`LocalAPI.Versions` returns retained versions newest first; `Version` reads one positive revision.
Each `store.Version` has its own ID, document ID, revision, status, creation time, and snapshot.

Version history is not a raw database escape hatch:

- `CollectionAccess.ReadVersions` runs independently; when nil, it falls back to `Read`.
- A filtered decision is applied to every stored snapshot, so a caller can see only the historical
  states that match its predicate.
- Current field access, computed output, after-read hooks, localization projection, and redaction
  apply before a snapshot is returned.
- Reading a single revision narrows the authorized history before running its output lifecycle.

This means changing a field-access rule can hide a value in old snapshots without rewriting stored
history. It also means an ownership predicate can expose different revisions to old and new owners.

## Restore without rewriting history {#restore}

A restore does not move a pointer backward or delete later revisions. Ridu loads the authorized
snapshot, then runs its values through the publish lifecycle when the selected snapshot is
published, or the unpublish lifecycle when it is draft. Because the transition carries values, it
requires Update plus the matching lifecycle permission and runs current validation, relationship
checks, hooks, localization, and optimistic concurrency. The restored document receives the next
revision and the restore itself becomes a new snapshot.

```go
versions, err := app.Local().Versions(ctx, "posts", post.ID, ridu.FindOptions{Actor: actor})
if err != nil {
	return err
}

restored, err := app.Local().Restore(ctx, "posts", post.ID, versions[len(versions)-1].Revision, ridu.MutationOptions{ExpectedRevision: post.Revision, Actor: actor})
```

`Restore` preserves the selected snapshot's status. `RestoreAsDraft` copies its values but forces
draft status and therefore requires drafts to be enabled. Both actions take mutation options for
exact actor identity, returned population/output selection, revision fence, and
locale controls. Restore first requires version-read permission, then Update plus Publish or
Unpublish according to the resulting status.

For localized resources, the snapshot is restored as a canonical all-locale value set so one
locale cannot accidentally splice old data over another. The response can still be projected to
the requested locale.

## Schedule collection publishing {#scheduling}

Scheduled publishing is durable for versioned collection documents:

```go
job, err := app.SchedulePublish(
	ctx,
	"posts",
	post.ID,
	time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
	post.Revision,
	identity,
)
```

Scheduling requires publish capability immediately. If the expected revision is zero, Ridu
captures the current revision so a later edit makes the job stale rather than publishing
unexpected content. The requesting auth collection and user ID are persisted, not a stale copy of
the user document.

At execution, the worker reloads that exact user, re-evaluates current publish access, and applies
the stored revision fence through the normal publish operation. A deleted user, changed role,
removed collection, stale revision, or failed hook leaves an actionable failed scheduled task
rather than silently publishing. `ScheduledPublishes` lists queued/running/failed items;
`CancelScheduledPublish` cancels or dismisses one after rechecking publish permission.

`ridu.Execute` runs the durable task worker. `HandlerOptions.TaskInterval` and `TaskBatch` tune its
polling. Directly embedded applications can call `RunScheduledPublishes` from their own worker
boundary.

Scheduled publishing currently targets collection documents only. Globals support immediate
publish/unpublish and version restore, but not scheduled global publishing.

## Versioned globals {#globals}

Globals accept the same `Versions` and `VersionConfig` fields. A missing draft-enabled global reads
as a schema-shaped draft with defaults; its first `UpdateGlobal` persists revision 1. Use:

- `Global` and `UpdateGlobal`;
- `PublishGlobal` and `UnpublishGlobal`;
- `GlobalVersions` and `GlobalVersion`;
- `RestoreGlobal` and `RestoreGlobalAsDraft`.

`GlobalAccess.ReadVersions` falls back to `Read`; `GlobalAccess.Publish` and `Unpublish` each fall
back to `Update` when omitted. A filtered update cannot initialize a missing singleton because
there is no row to match, so its first write needs an unconditional allow decision. Retention,
autosave, redaction, restore-as-new-revision, and optimistic conflicts otherwise match collection
behaviour.

## Common surprises {#failures}

| Symptom                                           | Explanation                                                                                                            |
| ------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| Anonymous `Find` cannot see a new document        | Draft-enabled creates default to draft; publish it or request authorized draft visibility.                             |
| `Draft: &true` returns published documents too    | True means include drafts, not draft-only. Add an authored status field if the product needs another workflow filter.  |
| Old snapshot has a redacted/missing field         | Current field access and after-read lifecycle apply to history reads.                                                  |
| Restore returns `access_denied`                   | Restore needs both access to that snapshot and update access to the current document.                                  |
| Restore/publish/update returns `conflict`         | The expected revision is stale; reload instead of silently retrying with zero.                                         |
| Only 100 snapshots remain                         | Zero `MaxPerDocument` resolves to the 100-version default; raise it if storage policy allows.                          |
| Setting autosave to zero does not stop it         | Zero resolves to the current 30-second default; the admin server-saves drafts and locally checkpoints published edits. |
| Scheduled job failed after an editor role changed | Execution rehydrates and reauthorizes the original exact identity by design.                                           |

Version and publishing methods are listed in the [Local Go API](/docs/local-api/) and exact Go
signatures are in the [Go API reference](/reference/ridu/). For browser and wire forms, see the
[TypeScript SDK](/docs/typescript-sdk/) and [REST API](/docs/rest-api/).
