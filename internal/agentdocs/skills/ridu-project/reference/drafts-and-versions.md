<!-- Generated from website/src/content/docs/drafts-and-versions.md by scripts/sync-agent-docs.ts. -->

# Drafts and versions

Versions and drafts are related but separate. `Versions: true` records immutable snapshots and
adds optimistic revisions. `VersionConfig.Drafts: true` adds incomplete working content alongside
a separate published snapshot. You can keep history without enabling draft creation.

## Enable revision history {#enable-versions}

Turn versioning on when you create a collection or global, or later. When an existing one already
stores documents, you choose whether they stay published or become drafts, and a migration records
the choice. See [Enabling versions](./migrations/enable-versions.md).

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
exposes the same values as `Document.Revision` and `Document.Status`. Working reads also expose
`_publishedRevision` and `_hasDraftChanges` when live content exists. `_status: 'published'` means
there is a live snapshot; pending working edits are not automatically public. Public reads omit
the draft metadata and return the live snapshot's own revision and timestamp.

| Setting            | Current behaviour                                                                                                                   |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| `Drafts`           | Defaults false. When true, ordinary creates default to draft and draft-specific create/update/restore behaviour is enabled.         |
| `MaxPerDocument`   | Defaults to 100. Every mutation prunes the oldest snapshots beyond this positive per-document limit.                                |
| `AutosaveInterval` | Defaults to 30 seconds when zero; a configured value must be at least one second. It controls the admin's working-draft save timer. |

The server does not mutate documents on a timer. Autosave is the admin calling the normal draft
write endpoint, so access, validation, hooks, revisions and conflicts still apply. It saves working
content for unpublished and published documents, and creates a new draft after meaningful edits
without requiring a manual first save. Untouched new forms do not create documents. New auth
identities require an explicit credential-bearing save; autosave never creates accounts.

A saved pending draft survives reload and opening the document on another device. The admin
distinguishes saved pending changes from unsaved typing, allows publishing a clean saved draft and
offers **Discard draft changes**. Stale saves pause for review. An uncertain first-create outcome
is not automatically retried, because doing so could create a duplicate. Browser checkpoints remain
failure-recovery protection rather than the primary draft store. Zero currently selects the
30-second default rather than disabling the timer.

Every accepted create, duplicate, update, publish, unpublish, discard, and restore saves the canonical
stored document in the same transaction. Reads and delete/trash operations do not create
versions. Relationship population, computed output, and field redaction are response shapes and
are not copied into the stored snapshot. The current working and published heads are independent of
history retention: pruning versions cannot remove the live content or the media it references.

## Draft write semantics {#draft-writes}

`MutationOptions.Draft` has three states:

| Configuration/request                           | Create                                   | Existing update                                          |
| ----------------------------------------------- | ---------------------------------------- | -------------------------------------------------------- |
| Versions disabled                               | Ordinary unversioned document            | Ordinary unversioned update                              |
| Versions enabled, drafts disabled, `Draft: nil` | Published                                | Rejected for the published row; use `PublishChanges`     |
| Drafts enabled, `Draft: nil`                    | Draft                                    | Updates a draft; published rows require `PublishChanges` |
| `Draft: &true`                                  | Draft; rejected when drafts are disabled | Save working content without changing the live snapshot  |
| `Draft: &false`                                 | Published                                | Rejected; use the publish lifecycle                      |

Draft saves defer required fields, minimum text length and minimum row counts recursively. Types,
enum values, upper bounds, structural row identity, valid references, access and plugin codecs still
apply. Authentication identity and upload-file requirements are not editorial completeness rules.
Custom validators still run; publication-only rules can inspect
`operation.Context.WritePhase == operation.WritePhasePublished`. Drafts also stay incomplete when
a migration [makes a field required](./migrations/required-fields.md): it checks published
content, not drafts.

In REST/SDK calls, the equivalent write option is `draft: true` or `draft: false`. Publishing and
unpublishing are clearer lifecycle operations than changing status as part of an unrelated update,
because they run the publish/unpublish hook operation and are easier to audit.

The low-level Go and REST version surface is present on any versioned resource. Treat
publish/unpublish as a draft workflow and configure `Drafts: true`; restore-as-draft rejects a
versioned resource whose draft support is disabled, and the admin/GraphQL draft actions
are driven by that setting. Generated TypeScript `DraftCreate`/`DraftUpdate` models and Go `Draft`
models describe incomplete authored values without weakening block discriminators, reference IDs
or plugin structure. Go typed handles expose
[`CreateDraft`](https://riducms.com/reference/core/bound-typed-collection-create-draft-method/) and
[`SaveDraft`](https://riducms.com/reference/core/bound-typed-collection-save-draft-method/).

## Draft read semantics {#draft-reads}

Drafts are editorial work. By default only editors see them: users of the admin's user collection
(`Config.Admin.User`) whose `Admin` rule lets them in. Anonymous callers and your app's other auth
collections read published content, even when the collection's `Read` rule allows them.

The Local Go API's `FindOptions.Draft` and `ListOptions.Draft` are three-state:

| Read option     | Result set                                                                                         |
| --------------- | -------------------------------------------------------------------------------------------------- |
| `Draft: nil`    | Working documents for actors allowed by `ReadDrafts`; live snapshots otherwise.                    |
| `Draft: &false` | Live snapshots only, including for editors; never-published documents are absent.                  |
| `Draft: &true`  | Working documents, including unpublished ones; requires `ReadDrafts`, also for anonymous requests. |

`Draft: &true` means read working content, not draft-only status. REST uses `?draft=true` or
`?draft=false`; the SDK and GraphQL use `draft: true` or `draft: false`. Each enters the same draft
access rule. An anonymous Go call is not implicitly trusted; deliberately privileged server work
must choose `System` rather than obtaining permission by setting a draft selector.

Head selection precedes access predicates, caller filters, sorting, pagination, counts and
population in the store query. Public queries cannot match a pending title and then return the old
published title. Draft-aware population still checks each target's normal access rules.

Every database adapter stores live content with the working layout and the same indexes, so a
field's `Index()` serves public and editorial queries alike. PostgreSQL keeps live content in a
typed live table beside each versioned resource's working table; SQLite and MongoDB mirror their
working document storage. Version history is stored separately.

To choose who reads drafts yourself, set `ReadDrafts` on the collection or global. It returns
`Allow` or `Deny` and replaces the editor default:

```go title="content/access.go"
package content

import "github.com/riducms/ridu"

// Authors sign in through their own collection and preview their drafts.
func authorsReadDrafts(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor != nil && ctx.ActorCollection == "authors" {
		return ridu.Allow(), nil
	}
	return ridu.Deny(), nil
}
```

Set `Access: ridu.CollectionAccess{ReadDrafts: authorsReadDrafts}` on the collection. `Read` still
applies on top, so combine the two for ownership rules such as "authors see their own drafts". The
same rule decides whether a relationship may point at a draft and whether population returns one.
See [Access control](./access-control.md) for role and ownership predicates.

A draft is not a security boundary. Collection and field access always run, and requesting draft
content does not grant permission. Preview tokens provide a separate, short-lived, target-scoped
read path for a configured live preview; they do not weaken normal collection routes. See
[Editorial workflows](https://riducms.com/docs/editorial-workflows/).

## Publish and unpublish {#publish-unpublish}

Publish atomically replaces the live snapshot from the final working candidate; unpublish removes
live content while retaining working values. Both are mutations: they
run their dedicated collection access rule (falling back to `Update` only when omitted), update
field access, validation, field and collection change/operation hooks, optimistic revision checks,
snapshotting, and after-commit work. `PublishChanges` and the SDK's `publishChanges` atomically
apply edited values through that same publish lifecycle instead of performing an ordinary update.
Body-bearing publish and unpublish operations require both `Update` and their dedicated lifecycle
permission. Status-only transitions require only the dedicated permission. An ordinary update of a
published versioned row, or one that sets its status, returns `publish_required` (409); Ridu never
silently turns a generic update into a live publication. Save a published row's working content
with `draft: true`, then publish it. Alternatively use `PublishChanges`
(REST `POST /api/collections/{collection}/{id}/publish` with the changed fields) to edit and publish
atomically. A collection with versions but no drafts publishes every row, so it is edited
only through `PublishChanges`.

Publication validates the whole merged document, not just its changed fields. Supplied exact
translations must be complete and valid; entirely absent translations follow the existing optional
translation policy. Fallback content is never saved as a translation. Failed publication leaves
the saved working content and previous live snapshot unchanged.

```go
published, err := app.Local().Publish(ctx, "posts", post.ID, ridu.MutationOptions{ExpectedRevision: post.Revision, Actor: actor})
if err != nil {
	return err
}

edited, err := app.Local().PublishChanges(ctx, "posts", post.ID, store.Values{"title": store.String("Edited live")}, ridu.MutationOptions{ExpectedRevision: published.Revision, Actor: actor})
if err != nil {
	return err
}

draft, err := app.Local().Unpublish(ctx, "posts", post.ID, ridu.MutationOptions{ExpectedRevision: edited.Revision, Actor: actor})
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

In the admin, open a saved document or global and choose **Versions** to browse its history. Select
a revision to compare it with the previous revision, the latest published revision, or another
revision from **More versions**. The editor's version badge reads an authorized count-only endpoint;
opening history loads the retained snapshots.
The comparison shows changed fields by default; you can show all
fields and choose which content locales to compare. If a relationship target can no longer be read,
its ID remains visible. The comparison follows the current read permissions described above.

## Restore without rewriting history {#restore}

A restore does not move a pointer backward or delete later revisions. Ridu loads the authorized
snapshot, then runs its values through the publish lifecycle when the selected snapshot is
published, or removes live content when the selected snapshot is draft. Restoring a draft into an
already-unpublished document is an ordinary draft update. The transition requires Update plus any
matching Publish or Unpublish permission and runs current validation, relationship
checks, hooks, localization, and optimistic concurrency. The restored document receives the next
revision and the restore itself becomes a new snapshot.

```go
versions, err := app.Local().Versions(ctx, "posts", post.ID, ridu.FindOptions{Actor: actor})
if err != nil {
	return err
}

restored, err := app.Local().Restore(ctx, "posts", post.ID, versions[len(versions)-1].Revision, ridu.MutationOptions{ExpectedRevision: post.Revision, Actor: actor})
```

`Restore` uses the selected snapshot's publication lifecycle. Restoring a published snapshot
deliberately replaces live and working content together. `RestoreAsDraft` copies its values into
working content without unpublishing the existing live snapshot and requires drafts to be enabled.
Both actions take mutation options for
exact actor identity, returned population/output selection, revision fence, and
locale controls. Restore first requires version-read permission. Restoring as draft, or restoring
a draft when nothing is live, requires Update rather than Unpublish.

To reset a pending draft to the live snapshot, call `LocalAPI.DiscardDraft` or SDK `discardDraft`
with its working revision. This increments the working revision, clears pending changes and leaves
the public revision and timestamp intact. It requires Update and field update access, not Publish
or Unpublish. It runs operation observers and post-change hooks, but skips candidate-mutating
before-validate/before-change hooks so the reset remains exact. There must be a live snapshot and
pending changes; delete a never-published draft instead.

REST uses `POST /api/collections/:slug/:id/discard-draft`; globals use
`POST /api/globals/:slug/discard-draft` and SDK `discardGlobalDraft`.

For localized resources, the snapshot is restored as a canonical all-locale value set so one
locale cannot accidentally splice old data over another. The response can still be projected to
the requested locale.

## Schedule collection publication {#scheduling}

Scheduled publication changes are durable for versioned collection documents:

```go
job, err := app.SchedulePublish(
	ctx,
	"posts",
	post.ID,
	time.Date(2027, 9, 1, 9, 0, 0, 0, time.UTC),
	ridu.PublicationScheduleOptions{ExpectedRevision: post.Revision, TimeZone: "Europe/London"},
	identity,
)
```

For a saved collection document, use **Schedule** to choose Publish or Unpublish and a future time.
Save unsaved edits first: the drawer will not schedule while the form is dirty. Unpublish is offered
only for a currently published draft-capable document. The drawer lists upcoming and failed events,
which an authorized editor can cancel or dismiss.

The schedule drawer offers searchable choices from `Admin.Localization.TimeZones`. Each choice and
saved event shows its UTC offset at the selected date, configured label, and localized timezone name.
Changing the timezone preserves the scheduled instant and changes the displayed wall time. The
drawer starts with the configured default; clearing the choice uses the browser timezone. Each event
retains its chosen timezone without changing the account preference. The drawer rejects a wall time
that repeats when daylight saving ends rather than guessing which occurrence was intended.

The Go and SDK schedule methods take an absolute instant. `TimeZone` or `timeZone` only preserves a
display timezone; it does not change `runAt`. Valid values are `UTC`, a `±HH:mm` offset, or an IANA
region such as `Europe/London`. Omit the timezone when no display choice needs to be saved. The SDK
accepts `{ revision, timeZone }` in `schedulePublish` and `scheduleUnpublish`; REST accepts optional
`timeZone` alongside the required `action` and `runAt` fields.

Scheduled publish requires publish capability; scheduled unpublish requires unpublish capability
and a currently published document. If the expected revision is zero, Ridu captures the current
revision so a later edit makes the job stale rather than publishing unexpected content. The
requesting auth collection and user ID are persisted, not a stale copy of the user document.

At execution, the worker reloads that exact user, re-evaluates the selected action's access, and
applies the stored revision fence through the normal publish or unpublish operation. A deleted user,
changed role, removed collection, stale revision, or failed hook leaves an actionable failed
scheduled task rather than silently changing publication. `ScheduledPublications` lists queued and
failed items; running work is already executing and is not presented as cancelable.
`CancelScheduledPublication` cancels or dismisses a listed item after rechecking the selected
action's permission.

`ridu.Execute` runs the durable task worker. `HandlerOptions.TaskInterval` and `TaskBatch` tune its
polling. Directly embedded applications can call `RunScheduledPublications` from their own worker
boundary.

Scheduled publication changes currently target collection documents only. Globals support immediate
publish/unpublish and version restore, but not scheduled global publishing.

## Versioned globals {#globals}

Globals accept the same `Versions` and `VersionConfig` fields. An authorized working read of a missing
draft-enabled global returns a schema-shaped draft with defaults; a live read returns `not_found`
until publication. Its first `UpdateGlobal` persists revision 1. Use:

- `Global` and `UpdateGlobal`;
- `PublishGlobal` and `UnpublishGlobal`;
- `GlobalVersions` and `GlobalVersion`;
- `RestoreGlobal` and `RestoreGlobalAsDraft`.
- `DiscardGlobalDraft`.

`GlobalAccess.ReadVersions` falls back to `Read`; `GlobalAccess.Publish` and `Unpublish` each fall
back to `Update` when omitted. A filtered update cannot initialize a missing singleton because
there is no row to match, so its first write needs an unconditional allow decision. Retention,
autosave, redaction, restore-as-new-revision, and optimistic conflicts otherwise match collection
behaviour.

## Common surprises {#failures}

| Symptom                                           | Explanation                                                                                                               |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| A signed-in user cannot see a new document        | Draft-enabled creates default to draft, and only editors read drafts; publish it or allow the user with `ReadDrafts`.     |
| `Draft: &true` returns published documents too    | True selects working content, including documents that already have a live snapshot.                                      |
| Old snapshot has a redacted/missing field         | Current field access and after-read lifecycle apply to history reads.                                                     |
| Restore returns `access_denied`                   | Restore needs both access to that snapshot and update access to the current document.                                     |
| Restore/publish/update returns `conflict`         | The expected revision is stale; reload instead of silently retrying with zero.                                            |
| Only 100 snapshots remain                         | Zero `MaxPerDocument` resolves to the 100-version default; raise it if storage policy allows.                             |
| Setting autosave to zero does not stop it         | Zero resolves to the current 30-second default; the admin server-saves working drafts, including pending published edits. |
| Scheduled job failed after an editor role changed | Execution rehydrates and reauthorizes the original exact identity by design.                                              |

Version and publishing methods are listed in the [Local Go API](./local-api.md) and exact Go
signatures are in the [Go API reference](https://riducms.com/reference/ridu/). For browser and wire forms, see the
[TypeScript SDK](./typescript-sdk.md) and [REST API](./rest-api.md).
