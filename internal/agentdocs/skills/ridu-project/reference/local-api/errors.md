<!-- Generated from website/src/content/docs/local-api/errors.md by scripts/sync-agent-docs.ts. -->

# Transactions and errors

## Calls inside hooks share a transaction {#transactions}

A Local API call made while an operation is still saving, from a `BeforeChange` hook for example,
joins that operation's database transaction. If either fails, both roll back:

```text title="A post save that updates its author" diagram
┌─ transaction ─────────────────────────────────────────────┐
│ 1. BeforeChange hook: ctx.Local.Update("users", authorID) │
│      runs the user's access rules, validation and hooks   │
│ 2. save the post                                          │
│ 3. commit both, or roll both back                         │
└───────────────────────────────────────────────────────────┘
```

The nested call still runs its own access rules, validation, hooks and version snapshot. If it
fails, the whole transaction is rolled back even when the hook ignores the error.

After-commit and after-error hooks run outside the transaction, so their calls start a new one. An
error there means the original change is already saved: don't retry it blindly. For work outside
the database, such as email or webhooks, use an after-commit hook or a [durable task](https://riducms.com/docs/tasks/).

Ridu stops calls that loop back into the same hooked operation too many times, with
`operation_recursion`. Treat that as a safety net, not a way to end a loop. There is no manual
begin, commit or rollback; put related writes in hooks. See
[Transactions and errors in hooks](../hooks/transactions-and-errors.md).

## Handle an error {#errors}

Failed calls return a `*ridu.OperationError`. Branch on its `Code`, not its `Message`:

```go
post, err := app.Local().Update(ctx, "posts", postID, values,
	ridu.MutationOptions{Actor: user},
)
if err != nil {
	var failure *ridu.OperationError
	if !errors.As(err, &failure) {
		return err
	}
	switch failure.Code {
	case "validation":
		return showFieldIssues(failure.Issues)
	case "conflict":
		return reloadAndAskTheAuthor(failure)
	case "access_denied", "not_found":
		return hideUnavailableDocument()
	}
	return err
}
```

| Field             | Holds                                                                         |
| ----------------- | ----------------------------------------------------------------------------- |
| `Code`            | the stable code, such as `validation`, `conflict` or `not_found`              |
| `Status`          | the matching HTTP status                                                      |
| `Issues`          | one entry per invalid field, with its `Path`, `Code` and `Message`            |
| `Committed`       | the change was saved, and the error came from work after the commit           |
| `CommitAttempted` | the commit was attempted but its outcome is unknown, so check before retrying |

`not_found` can also mean the caller's access rules hide the document. [Writing data](../writing-data.md#errors)
lists the codes a write returns, and [Filters](../querying/filters.md#errors) the ones a query
returns.
