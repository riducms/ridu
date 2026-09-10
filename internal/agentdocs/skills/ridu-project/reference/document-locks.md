<!-- Generated from website/src/content/docs/document-locks.md by scripts/sync-agent-docs.ts. -->

# Coordinate editors with document locks

Document locks coordinate authors who open the same collection document. An active lease makes the
second editor's form read-only and identifies the current owner; an authorized editor can take it
over. Locks improve the authoring experience, but they do not grant update access and do not replace
optimistic revision checks.

## Enable locks {#configure}

```go title="content/posts.go"
package content

import (
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Posts = ridu.Collection{
	Slug:          "posts",
	LockDocuments: true,
	DocumentLockConfig: ridu.DocumentLockConfig{
		Duration: 5 * time.Minute,
	},
	Fields: field.Fields{
		field.Text("title").Required(),
	},
}
```

Zero duration uses five minutes. A custom duration must be at least ten seconds. Save this change
with `ridu dev` running; it regenerates contracts, safely synchronizes the development store, and
reloads the admin. The selected store must implement Ridu's document-lock contract; all official
database adapters do. Before deployment, create, review, and verify the adapter migration.

## What authors experience {#admin-behavior}

When an authorized author opens an existing post, the admin acquires a persisted lease and refreshes
it while the editor remains active. Leaving the route releases an owned lease; an abandoned lease
expires after the configured duration.

If another author owns the active lease, the form becomes read-only and shows the owner's label and
expiry. **Take over** appears only when the current actor has unlock capability. A successful
takeover replaces the previous lease; it does not save or discard either editor's local form state.

Locking applies to existing collection documents. Create forms and globals do not acquire document
leases.

## Authorize takeover separately {#access}

Update access is required to acquire or refresh a lock. `CollectionAccess.Unlock` controls takeover
of another editor's lease and falls back to `Update` when omitted. Define it when takeover should
be limited to a smaller role:

```go
Access: ridu.CollectionAccess{
	Update: canEditPosts,
	Unlock: administratorsOnly,
},
```

Reading lock state returns not-found behavior when the document itself is not readable, so lock
metadata cannot reveal a hidden document. The server re-evaluates access for acquisition and
takeover; showing or hiding the admin button is only presentation.

## Use locks outside the admin {#sdk}

Custom authoring clients can use the generated SDK:

```ts title="edit-lease.ts"
let state = await ridu.acquireDocumentLock('posts', post.id);

if (!state.owned && state.canTakeOver) {
	state = await ridu.acquireDocumentLock('posts', post.id, true);
}

if (!state.owned) {
	throw new Error(
		`This post is being edited by ${state.lock?.ownerLabel ?? 'another author'}.`
	);
}

try {
	await ridu.update(
		'posts',
		post.id,
		{ title: 'Reviewed title' },
		{ revision: post._revision }
	);
} finally {
	await ridu.releaseDocumentLock('posts', post.id);
}
```

Long-lived clients must refresh an owned lease before it expires. Reacquiring the same lock refreshes
it. Browser unload delivery is best-effort, so expiry must remain the recovery path.

Always send the last observed `_revision` for versioned updates. A lock can expire, be taken over, or
be bypassed by a non-authoring client; the revision fence is what prevents a stale write from
overwriting a newer committed document.

## Document locks are not account locks {#account-locks}

An auth account can also be locked after repeated failed sign-ins. That is a separate authentication
feature controlled by `AuthConfig.MaxLoginAttempts` and `AuthConfig.LockDuration`. An authorized
administrator uses **Force unlock** or `RiduClient.forceUnlock`; document-lock takeover does not
change account state.

## Troubleshooting {#troubleshooting}

| Symptom                       | What to check                                                                                                               |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| No lock appears               | Confirm `LockDocuments: true`, keep `ridu dev` running, and open an existing collection document rather than a create form. |
| The form remains read-only    | The lease belongs to another identity. Wait for expiry or ask an actor with `Unlock` access to take over.                   |
| Take over is missing          | The actor lacks evaluated unlock capability, or the lease is already owned/expired.                                         |
| Lock calls return `not_found` | The collection has no lock support, the document is absent, or read access hides it.                                        |
| Save still returns `conflict` | Another committed write changed `_revision`. Refresh and reapply the intended edit; the lease is not a concurrency bypass.  |

See [Create and edit documents](./editing-documents.md), [Access control](./access-control.md),
and the exact
[`RiduClient.acquireDocumentLock`](https://riducms.com/reference/sdk/ridu-client-acquire-document-lock/) and
[`App.AcquireDocumentLock`](https://riducms.com/reference/core/app-acquire-document-lock/) contracts.
