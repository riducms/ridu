---
title: 'Transactions and errors'
description: 'Save related documents together, send notifications after commit, and handle failed hook operations.'
product: core
eyebrow: 'Hooks'
order: 85
navigation:
  section: 'Extend Ridu'
  parent: hooks
  order: 50
  title: 'Transactions and errors'
---

Every create, update, or delete runs in a database transaction. Hooks that run before the
commit can save related documents in the same transaction, so everything is saved together or
nothing is. `AfterCommit` hooks run once the save has succeeded, which makes them the place for
email and webhooks. `AfterError` hooks report operations that failed.

## Choose the hook for the work {#configuration}

| Hook or option                    | Use it for                                                         | If it fails                                                                 |
| --------------------------------- | ------------------------------------------------------------------ | --------------------------------------------------------------------------- |
| `AfterChange`, `AfterDelete`      | Related Ridu writes that must be saved together with the document. | The whole transaction rolls back.                                           |
| `AfterOperation`                  | Final database work for any kind of operation.                     | The transaction rolls back.                                                 |
| `AfterCommit`                     | Email, webhooks, search indexes, and cache purges.                 | The document stays saved; the caller receives an error.                     |
| `Config.AfterCommit`              | Scheduling every after-commit effect centrally.                    | The dispatcher decides when effects run. Ridu does not store or retry them. |
| A registered [task](/docs/tasks/) | Background work that must be retried until it succeeds.            | The store keeps the task and retries it.                                    |
| `AfterError`                      | Logging or reporting a failed operation.                           | The original failure still reaches the caller.                              |

## Save related documents together {#nested-operations}

A collection or global hook can create, update, or delete other documents through `ctx.Local`.
Pass `ctx.Context` as the first argument so the related write joins the current transaction:
either both writes are saved, or neither is.

```go
_, err := ctx.Local.Create(ctx.Context, "audit-log", values, ridu.MutationOptions{
	// Local API calls do not inherit these; pass them explicitly.
	Actor:           ctx.Actor,
	ActorCollection: ctx.ActorCollection,
	Locale:          ctx.Locale,
})
return err // An error here rolls back the original save too.
```

The [`AfterChange` audit example](/docs/hooks/collections/#after-change) shows the complete hook.
A few rules apply to every related write:

- The related operation runs its own access rules, validation, and hooks.
- If it fails, the shared transaction fails. Ignoring the error does not save the original
  document.
- A standalone read runs in a read-only transaction, so write from a save or delete hook, not
  from `AfterRead` or `BeforeRead`.
- Attach the hook to the collection being changed, not to the collection it writes to. An audit
  hook on the audit collection would log its own entries forever. See
  [Avoid triggering the same hook again](/docs/hooks/context/#recursion).

## Send notifications after a successful save {#after-commit}

Use `AfterCommit` for email, webhooks, and other work outside the database. `AfterChange` still
runs inside the transaction: a later failure could roll back the document after you had already
sent an email. `AfterCommit` runs only once that transaction has committed.

This helper sends a JSON webhook with the saved post ID and operation. It ignores deletes and
restores, which also run `AfterCommit`, and gives the HTTP request a five-second timeout:

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
		// AfterCommit also runs after deletes; notify only on saves.
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
[`AfterCommitDispatcher`](/reference/ridu/after-commit-dispatcher/). A dispatcher can arrange how
to run an effect after commit; it does not make an external service part of the database
transaction. Design retries so the receiver can recognize an event it has already processed.
There is also a possible gap between committing a document and successfully queuing external
work; plan how you will recover missed work when delivery matters.

Calls to `ctx.Local` from `AfterCommit` start new transactions. Updating the same document there
can trigger its hooks again, so keep the notification helper focused on delivery.

## Log a failed operation {#handle-errors}

`AfterError` hooks run when an operation fails, with the failure in `ctx.Error`. The
[`AfterError` example](/docs/hooks/collections/#after-error) logs it. Two rules to remember:

- Returning `nil` does not make the failed operation succeed, and returning another error does
  not replace the original one.
- For a write that fails before commit, the hook runs after the rollback. Log the failure; do not
  try to repair the write by starting new writes from this hook.

To log failures from every collection and global, add the hook to your application's
`ridu.Config` instead:

```go
config.Hooks.AfterError = []ridu.Hook{logFailure}
```

Application error hooks also see failures that happen before Ridu knows which collection is
involved. When a collection or global is known, its own `AfterError` hooks run first. Add a logger
in one place only if you want one entry per failure.

### What the caller sees when a hook fails {#hook-errors}

Any error from a hook stops the operation. Before commit, it also rolls back the write and any
related writes, even when it comes from `AfterChange`, `AfterOperation`, or `AfterRead`. What the
caller sees depends on the error:

| The hook returns                               | The caller receives                                                                          |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `ridu.Reject("…")`                             | A `rejected` error (HTTP 422) with your message and any issues. The admin shows the message. |
| `ridu.Reject("…", operation.Issue{Target: …})` | The same, and the admin marks each issue's field.                                            |
| Any other error                                | An `internal` error (HTTP 500). Your message stays in the server logs and in `AfterError`.   |

Use `ridu.Reject` for a deliberate refusal the editor can act on, such as “Remove the article from
the homepage first.” Return an ordinary error when your code could not finish its work, such as a
failed lookup or an unavailable service. In the Go local API, both arrive as a
`*ridu.OperationError`: `Code` is `"rejected"` or `"hook_failed"`, and `errors.Unwrap` reaches
an ordinary error's original cause.

To reject a single field value whenever it is saved, [custom validation](/docs/fields/validation/)
is usually simpler: it runs on every save and can also check values while the editor types.

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

The [hook and value performance guide](/docs/performance/hooks-and-values/) explains embedded
callback scaling, explicit mutable copies, operation-local reuse, and when to move work into one
resource hook.

See [`ridu.CollectionHooks`](/reference/ridu/collection-hooks/) for all available hooks and
[`ridu.Hook`](/reference/ridu/hook/) for the function signature.
