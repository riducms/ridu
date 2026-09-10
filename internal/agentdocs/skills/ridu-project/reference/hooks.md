<!-- Generated from website/src/content/docs/hooks.md by scripts/sync-agent-docs.ts. -->

# Hooks

Hooks are Go functions that run when Ridu creates, reads, updates, or deletes content.
Use them to clean up a field value, record who edited a document, save an audit entry,
or send a notification after a successful save.

They run on the server for requests from the admin, REST, the SDK, and the local Go API.

## Choose where your hook belongs {#choose-a-hook}

| What you want to do                                                       | Start here                                                      |
| ------------------------------------------------------------------------- | --------------------------------------------------------------- |
| Change one field, such as trimming a title or formatting a returned value | [Field hooks](./hooks/fields.md)                              |
| Work with a whole document, such as recording its last editor             | [Collection hooks](./hooks/collections.md)                    |
| Run code when a singleton such as site settings changes                   | [Global hooks](./hooks/globals.md)                            |
| Read the user, previous values, or current locale                         | [Hook context](./hooks/context.md)                            |
| Save related documents, send notifications, or handle failures            | [Transactions and errors](./hooks/transactions-and-errors.md) |

A field hook follows the field wherever you reuse it, including inside groups, arrays, and blocks.
A collection or global hook is a better fit when several fields need to change together.

## Before or after saving? {#lifecycle}

- **Before saving:** clean up or calculate values with `BeforeValidate` or `BeforeChange`.
- **After storage, before commit:** use `AfterChange` for related database writes that must
  succeed or roll back together.
- **After commit:** use `AfterCommit` to notify another service once the document has been saved.
- **When preparing a response:** use `AfterRead` to change what the caller receives without
  changing the stored value.

<span id="save-order"></span>

See the [collection hook sequence](./hooks/collections.md#save-order) for the exact order,
including validation, permissions, and field hooks. Some stages also run for reads and deletes;
check `ctx.Operation` when your code should run only on saves.

Ridu waits for each hook to finish. Use [tasks](https://riducms.com/docs/tasks/) for work that needs background
processing or retries, and read [after-commit behavior](./hooks/transactions-and-errors.md#after-commit)
before sending email or calling a webhook.

## Find an example {#examples}

- <span id="normalize-input"></span>[Trim a field before validation](./hooks/fields.md#normalize-input).
- <span id="typed-field-hooks"></span>[Change a typed field before saving](./hooks/fields.md#typed-field-hooks).
- <span id="read-hooks"></span>[Format a returned value](./hooks/fields.md#read-hooks).
- <span id="derive-values"></span>[Record who edited a document](./hooks/collections.md#derive-values).
- <span id="global-hooks"></span>[Add a hook to site settings](./hooks/globals.md#global-hooks).
- <span id="hook-context"></span><span id="stored-and-returned-values"></span>[Read and change hook values](./hooks/context.md).
- <span id="nested-operations"></span>[Save an audit entry in the same transaction](./hooks/transactions-and-errors.md#nested-operations).
- <span id="recursion"></span>[Avoid triggering the same hook again](./hooks/context.md#recursion).
- <span id="after-commit"></span><span id="after-commit-errors"></span>[Send a notification after a save](./hooks/transactions-and-errors.md#after-commit).
- <span id="handle-errors"></span><span id="hook-errors"></span>[Log and handle failures](./hooks/transactions-and-errors.md#handle-errors).
- <span id="performance"></span>[Keep frequently run hooks fast](./hooks/transactions-and-errors.md#performance).

## Handle errors across the application {#application-hooks}

`Config.Hooks.AfterError` runs for failures across the application, including failures that happen
before Ridu knows which collection or global is involved. Use it for a central logger or error
reporting service. Collection and global error hooks run before the application error hooks.

Follow [Log a failed operation](./hooks/transactions-and-errors.md#handle-errors) for a complete
example and the difference between a rolled-back write and a failure after commit.
