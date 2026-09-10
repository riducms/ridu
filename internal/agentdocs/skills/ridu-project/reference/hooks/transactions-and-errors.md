<!-- Generated from website/src/content/docs/hooks/transactions-and-errors.md by scripts/sync-agent-docs.ts. -->

# Transactions and errors

Use hooks inside the transaction for database changes that must succeed together, such as
saving a post and its audit entry. Use `AfterCommit` for email or webhooks once that save
has succeeded, and `AfterError` to report a failed operation.

## Choose the hook for the work {#configuration}

| Hook or option                 | Use it for                                                                       | Failure behavior                                                                 |
| ------------------------------ | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `AfterChange`                  | Related Ridu writes that must commit with the document.                          | An error rolls the transaction back.                                             |
| `AfterOperation`               | Final database work for any operation before response processing and commit.     | An error can still roll a write back.                                            |
| `AfterCommit`                  | Email, webhooks, external search, and effects that require a committed document. | An error is reported but cannot undo the commit.                                 |
| `Config.AfterCommit`           | Central scheduling or instrumentation of after-commit effects.                   | The dispatcher owns when `effect.Run` executes; it is not automatically durable. |
| A registered task              | Retryable background work with serializable input.                               | The selected store owns durable queue state and retries.                         |
| `AfterError`                   | Logging or observing the original operation failure.                             | Returning `nil` does not make the failed operation succeed.                      |
| `ctx.Local` with `ctx.Context` | Nested reads/writes through the normal engine and active transaction.            | Access, validation, hooks, and cancellation still apply.                         |

## Save related documents together {#nested-operations}

Use a collection or global hook's `ctx.Local` to write a related document. Pass `ctx.Context`
so the related write shares the transaction: either both writes succeed or neither is saved.
The related operation still runs its own access rules, validation, and hooks.

This example adds an audit record after a post changes. Add `AuditLog` to `Config.Collections`,
and choose its access rules as you would for any other collection:

```go title="content/audit_log.go" focus={21-24,29-34,36-37}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

var AuditLog = ridu.Collection{
	Slug: "audit-log",
	Fields: field.Fields{
		field.Text("document").Required(),
		field.Text("operation").Required(),
	},
}

func writeAuditEntry(ctx ridu.HookContext) error {
	if ctx.Document == nil {
		return nil
	}
	// Reuse the transaction so the post and audit entry save together.
	_, err := ctx.Local.CreateWithOptions(
		ctx.Context,
		"audit-log",
		store.Values{
			"document":  store.String(ctx.Document.ID),
			"operation": store.String(string(ctx.Operation)),
		},
		// Local API calls do not inherit the user or locale.
		ridu.MutationOptions{
			Actor:           ctx.Actor,
			ActorCollection: ctx.ActorCollection,
			Locale:          ctx.Locale,
		},
	)
	// An audit failure must also fail the post's save.
	return err
}
```

Register `writeAuditEntry` on the posts collection:

```go focus={3}
Hooks: ridu.CollectionHooks{
	// Save the audit entry in the same transaction as the post.
	AfterChange: []ridu.Hook{writeAuditEntry},
},
```

After a successful save, there is an audit record with the post ID and operation. If the audit
write is rejected, the post change is rolled back too. A failed nested operation marks the shared
transaction as failed; swallowing its error does not make the outer save succeed.

The options pass the signed-in user, auth collection, and locale to the related write.
See [Hook context](./context.md#nested-operations) for how to carry those values through
local API calls.

Keep related writes out of a standalone read's hooks: that transaction is read-only. Reads made
while preparing a write response may share a writable transaction, which is another reason to
choose a write hook for this work.

Register this hook on Posts, not AuditLog: logging the audit collection's own changes would
create a loop. See [Avoid repeated hook calls](./context.md#recursion) before writing
back to a document from its hooks.

## Send notifications after a successful save {#after-commit}

Use `AfterCommit` for email, webhooks, and other work outside the database. `AfterChange` still
runs inside the transaction: a later failure could roll back the document after you had already
sent an email. `AfterCommit` runs only once that transaction has committed.

This helper sends a JSON webhook with the saved post ID and operation. It ignores reads and
deletes and gives the HTTP request a five-second timeout:

```go title="content/notifications.go" focus={18-24,43-47}
package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
)

func notifyPosts(webhookURL string) ridu.Hook {
	// Ridu waits for this hook by default, so limit the delivery delay.
	client := &http.Client{Timeout: 5 * time.Second}
	return func(ctx ridu.HookContext) error {
		// AfterCommit runs on reads too; notify only on saves.
		switch ctx.Operation {
		case operation.Create, operation.Duplicate, operation.Update,
			operation.Publish, operation.Unpublish:
		default:
			return nil
		}
		if ctx.Document == nil {
			return nil
		}

		body, err := json.Marshal(struct {
			ID        string         `json:"id"`
			Operation operation.Kind `json:"operation"`
		}{ID: ctx.Document.ID, Operation: ctx.Operation})
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(
			ctx.Context, http.MethodPost, webhookURL, bytes.NewReader(body),
		)
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		// The post stays saved even if delivery fails from this point.
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("post webhook returned %s", response.Status)
		}
		return nil
	}
}
```

Configure `POST_WEBHOOK_URL` in your server environment, import `os` in the config file, and
register the helper on the posts collection:

```go focus={3}
Hooks: ridu.CollectionHooks{
	// Start delivery only after the database commit succeeds.
	AfterCommit: []ridu.Hook{notifyPosts(os.Getenv("POST_WEBHOOK_URL"))},
},
```

Saving a post sends a request such as `{"id":"post_123","operation":"update"}`. Reading it sends
nothing. A timeout or non-2xx response returns an error from the hook, but the post remains saved.

### What happens if the notification fails? {#after-commit-errors}

By default, Ridu waits for after-commit hooks before finishing the request. An error is reported
as `hook_failed` with `Committed: true` on the Go `ridu.OperationError`. Other queued
post-commit hooks are still attempted. The database change cannot be rolled back at this point.
Retry the notification, not the whole create request, which could create a second document.

For background delivery or retries, configure `Config.AfterCommit` with an
[`AfterCommitDispatcher`](https://riducms.com/reference/ridu/after-commit-dispatcher/). A dispatcher can arrange how
to run an effect after commit; it does not make an external service part of the database
transaction. Design retries so the receiver can recognize an event it has already processed.
There is also a possible gap between committing a document and successfully queuing external
work; plan how you will recover missed work when delivery matters.

Calls to `ctx.Local` from `AfterCommit` start new transactions. Updating the same document there
can trigger its hooks again, so keep the notification helper focused on delivery.

## Log a failed operation {#handle-errors}

Use `AfterError` to log a failure or record a metric. It receives the original error in
`ctx.Error`. Returning `nil` does not make the failed request succeed, and returning another
error does not replace the original failure.

```go title="content/log_failures.go" focus={12,14-15}
package content

import (
	"log"

	"github.com/riducms/ridu"
)

func logFailure(ctx ridu.HookContext) error {
	log.Printf(
		"Ridu %s failed (collection=%s global=%s): %v",
		ctx.Operation, ctx.CollectionID, ctx.GlobalID, ctx.Error,
	)
	// Finish logging; the original failure still reaches the caller.
	return nil
}
```

Attach it to a collection or global with:

```go
Hooks: ridu.CollectionHooks{
	AfterError: []ridu.Hook{logFailure},
},
```

For failures anywhere in the application, also add `logFailure` to
`Config.Hooks.AfterError`. If a collection or global is known, its error hooks run before the
application's error hooks. Avoid attaching the same logger in both places if you want one entry
per failure.

For a top-level write that fails before commit, error hooks run after rollback. If an after-commit
hook fails, the write is already committed. A failure in a nested local API operation can run error
hooks while the outer transaction is marked for rollback. Keep this hook
focused on logging; do not try to repair the failed write by starting more writes from it.

### Return validation messages from a validator {#hook-errors}

A non-nil error from an ordinary hook stops the operation and normally becomes `hook_failed`.
Before commit, that failure rolls back the write and related transactional writes. Even an
`AfterChange`, `AfterOperation`, or `AfterRead` failure can still trigger a rollback.

Use [custom validation](../fields/validation.md) to tell an author that a field value is invalid.
Its `operation.Issue` messages appear beside the appropriate inputs. Return an ordinary error
when your code could not finish its work, such as a failed related lookup or unavailable service.

## Keep hooks predictable and fast {#performance}

Read hooks can run for every document in a list, and a field hook can run for every array or block
row. Avoid repeated external requests in those hooks. Compute a value when saving if it can be
stored. When several fields need the same lookup, fetch once and update those fields together
in a collection hook.

Use `ctx.Context` for local API and network calls so cancellation and deadlines propagate. Keep
long-running work after commit, and make any retried delivery safe to run more than once. Test
both successful saves and failures: check the saved values, the response, related records, and
whether a notification ran after a rollback.

Keep per-row and per-block callbacks local to their current value; scanning the complete list from
every embedded occurrence turns N callbacks into roughly N² reads. Return `operation.Keep` when a
field does not change, and use immutable `Get`, `Lookup`, `Entries`, and `Elements` reads rather
than copying containers just to inspect them.

The [hook and value performance guide](https://riducms.com/docs/performance/hooks-and-values/) explains embedded
callback scaling, explicit mutable copies, operation-local reuse, and when to move work into one
resource hook.

See [`ridu.CollectionHooks`](https://riducms.com/reference/ridu/collection-hooks/) for all available hooks and
[`ridu.Hook`](https://riducms.com/reference/ridu/hook/) for the function signature.
