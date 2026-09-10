---
title: 'Durable tasks'
description: 'Run typed Go work with database-backed leases, retries, cancellation, and bounded retention.'
product: data
eyebrow: 'Data and APIs'
order: 130
navigation:
  section: 'Develop & operate'
  order: 40
  title: 'Durable tasks'
---

Ridu tasks are typed Go handlers with database-backed scheduling, retries, and leases. PostgreSQL
supports workers on multiple hosts. SQLite tasks are limited to one application host, and MongoDB
tasks follow its [bounded production profile](/docs/mongodb/). Use the local API from handlers when
content operations need access rules, validation, hooks, transactions, and redaction.

## Define and register a task {#define}

```go title="content/tasks.go"
package content

import (
	"time"

	"github.com/riducms/ridu"
)

type EmailInput struct {
	MessageID string `json:"messageID"`
	To        string `json:"to"`
}

type EmailOutput struct {
	ProviderID string `json:"providerID"`
}

var SendEmail = ridu.NewTask(
	"send-email",
	func(task ridu.TaskContext, input EmailInput) (EmailOutput, error) {
		providerID, err := send(task.Context, input.MessageID, input.To)
		if rateLimited(err) {
			return EmailOutput{}, ridu.RetryTaskAfter(
				"email_rate_limited", err, time.Minute,
			)
		}
		if invalidAddress(err) {
			return EmailOutput{}, ridu.AbortTask(
				"email_address_invalid",
				err,
			)
		}
		return EmailOutput{ProviderID: providerID}, err
	},
	ridu.TaskQueue("email"),
	ridu.TaskRetries(
		5,
		time.Second,
		time.Hour,
		ridu.TaskBackoffExponential,
	),
	ridu.TaskTimeout(2*time.Minute),
	ridu.TaskRetention(30*24*time.Hour),
)
```

Register the definition in executable config:

```go title="content/config.go"
func Config() ridu.Config {
	return ridu.Config{
		Name:  "Acme Editorial",
		Tasks: []ridu.TaskDefinition{SendEmail},
	}
}
```

Only a value returned by `ridu.NewTask` can implement `TaskDefinition`. Persisted JSON can select a
registered slug and provide validated data; it cannot provide executable code. Duplicate, reserved,
missing-handler, and invalid retry-policy definitions fail config validation.

## Enqueue and inspect work {#enqueue}

Use the same typed definition from trusted Go code:

```go
receipt, err := SendEmail.Enqueue(ctx, app, EmailInput{
	MessageID: requestID,
	To:        "author@example.com",
}, ridu.TaskEnqueueOptions{
	RunAt:          time.Now().Add(5 * time.Minute),
	ConcurrencyKey: "author@example.com",
})
if err != nil {
	return err
}

result, err := SendEmail.Result(ctx, app, receipt.ID)
```

`RunAt` schedules future admission. `Queue` can override the definition's queue. A concurrency key
serializes active leases for that key within one queue. Optional `Target` and `RequestedBy`
references attach lifecycle-safe collection/document identities; hard deletion removes tasks that
refer to the deleted identity.

`Result` returns typed output plus state, attempts, retained failure code and message, and completion
time. `HasOutput` distinguishes a successful zero value from work that has not succeeded. `Cancel`
atomically stops queued work and fences a running lease so a stale heartbeat or completion cannot
win afterward.

## Delivery is at least once {#at-least-once}

> [!WARNING]
> A handler can complete an external side effect and crash before recording success. Tasks use
> at-least-once delivery. Use a stable idempotency key for email, payments, webhooks, object
> writes, and other external effects.

Every claim has an opaque lease token. Heartbeats extend it; an expired lease can be reclaimed; and
completion, failure, release, and cancellation require the current token. A process stopping
gracefully cancels cooperative handlers and returns leases immediately. A handler that ignores
`task.Context` can delay drain, but its stale lease remains fenced and can be recovered after
expiry.

`TaskContext.Local` is the local API. If work represents a user, persist a stable user reference and
choose the actor when making the call. A nil actor is not an administrator.

## Recover a commit-to-enqueue gap {#admission-reconciliation}

If application state commits before its task is admitted, attach a bounded recovery scan to the
task definition:

```go
ridu.TaskAdmissionReconciler(func(
	ctx context.Context,
	app *ridu.App,
) error {
	return reconcilePendingExports(ctx, app)
})
```

Ridu runs the reconciler before every task claim cycle, including the first cycle at startup.
Implementations must be idempotent and safe across concurrent application instances. A failure is
reported through `HandlerOptions.JobError`, while work that is already queued remains eligible to
run. Use this only to repair durable admission gaps; it is not a cron scheduler.

For a document-backed admission queue, use `LocalAPI.ListWindow` over one direct, unique, indexed,
non-localized text field. It supports a bounded half-open range, not arbitrary filters, population,
versions, or trash. Put pending and terminal records in separate key ranges and move each examined
record forward with a compare-and-set.

## Retry, abort, and timeout {#failure-policy}

Return an ordinary error to use the definition's retry policy. Use the helpers when provider
semantics are known:

| Handler result                          | Outcome                                                                                                    |
| --------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `nil` error                             | Store the typed output and mark the task succeeded.                                                        |
| `ridu.RetryTask(code, err)`             | Queue another attempt using fixed, linear, or exponential definition backoff.                              |
| `ridu.RetryTaskAfter(code, err, delay)` | Queue another attempt after at least the supplied delay, useful for provider rate limits.                  |
| `ridu.AbortTask(code, err)`             | Mark a terminal failure and retain it until the configured retention expires.                              |
| Attempt timeout                         | Cancel the handler context and retry as `task_timeout` while attempts remain. Cancellation is cooperative. |

The first execution counts toward `TaskRetries`' maximum attempts. Linear and exponential growth is
deterministic and capped by the configured maximum delay.

## Bounds and sensitive data {#bounds}

Inputs and outputs are strict JSON for their registered Go types. Unknown fields are rejected, and
both payloads are capped at 1 MiB. Store large files in an upload backend and enqueue a small stable
reference.

Task inputs, outputs, error messages, target identities, and requester identities are database
records. Do not put credentials, bearer tokens, or other secrets in them. Error text, attempts,
timeouts, retry delays, claim/prune batches, concurrency keys, and retention are bounded by the
runtime.

## Failure codes {#failure-codes}

Application-defined codes such as `email_rate_limited` belong to your runbook. Ridu also exposes
stable boundary and persisted-state codes:

| Code                      | Where it appears              | Meaning                                                                                                                         |
| ------------------------- | ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `task_not_registered`     | local error or terminal state | The typed definition is not registered in this running application, or persisted work names a slug the binary does not contain. |
| `task_unavailable`        | local error                   | The application/store cannot provide the durable-task runtime.                                                                  |
| `task_input_invalid`      | local error or terminal state | Input could not be strictly decoded, encoded, or admitted within its bound.                                                     |
| `task_output_invalid`     | local error                   | Retained output does not match the exact typed definition.                                                                      |
| `task_output_too_large`   | terminal state                | A handler returned more than the 1 MiB output limit.                                                                            |
| `task_not_found`          | local error                   | The ID is absent or belongs to another typed task definition.                                                                   |
| `task_store_failed`       | local error                   | A store claim, read, mutation, or prune operation failed.                                                                       |
| `task_lease_lost`         | local error                   | Another lease state won; the stale attempt cannot commit.                                                                       |
| `task_timeout`            | retry or terminal state       | The attempt exceeded its configured timeout.                                                                                    |
| `task_handler_panicked`   | retry state                   | Ridu contained a handler panic without persisting the panic value and applied retry policy.                                     |
| `task_attempts_exhausted` | terminal state                | Lease recovery found work beyond its maximum attempts.                                                                          |
| `worker_shutdown`         | queued state                  | Graceful shutdown released the task for immediate recovery.                                                                     |

Monitor terminal codes and sustained retries by queue and slug. A diagnostic sink can receive the
underlying causes, so redact sensitive data there too.

## Worker operation {#worker}

`ridu.Execute` starts the worker when registered tasks or versioned collections require it.
`HandlerOptions` configures polling interval, claim batch, queues, lease duration, heartbeat
interval, and prune batch; zero values use defaults. PostgreSQL supports multiple workers across
hosts. SQLite supports same-host processes only. MongoDB remains limited to its documented
replica-set profile.

Scheduled publishing uses the reserved `ridu-schedule-publish` task. Use the scheduling API; do not
register that task yourself.

## Limits {#availability}

Tasks do not provide workflow graphs, declarative cron, a job admin, email or webhook adapters,
exactly-once external effects, or a sandbox for untrusted handlers.

See [Transactions and errors](/docs/hooks/transactions-and-errors/#after-commit) for after-commit dispatch, [Drafts and versions](/docs/drafts-and-versions/)
for scheduled publishing, [Security](/docs/security/) for trusted-code boundaries, and
[Production](/docs/production/) for worker drain and monitoring.
