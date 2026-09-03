---
title: 'Hooks'
description: 'Normalize input, derive values, coordinate nested work, and react after commit.'
product: core
eyebrow: 'Runtime'
order: 80
navigation:
  section: 'Extend Ridu'
  order: 20
  title: 'Hooks'
---

## The lifecycle {#lifecycle}

Hooks run as part of the same operation engine used by REST, the local API, the SDK, and the admin. Within a phase, each `[]ridu.Hook` runs in slice order. Choose the narrowest phase that owns the work: normalize before validation, derive before persistence, and leave external effects until after commit.

| Phase                         | When it runs                              | Good for                                          |
| ----------------------------- | ----------------------------------------- | ------------------------------------------------- |
| `BeforeDuplicate`             | After the source is copied                | Resetting slugs and copy-only fields              |
| `BeforeValidate`              | Before field validators                   | Trimming and normalizing input                    |
| `BeforeChange`                | After validation, before persistence      | Derived values and audit fields                   |
| `BeforeOperation`             | Immediately before storage                | Last transactional preparation                    |
| `BeforeRead`                  | Before documents are read                 | Request-scoped read setup                         |
| `BeforeDelete`                | After the original is loaded              | Dependent transactional cleanup                   |
| `AfterChange` / `AfterDelete` | After persistence, inside the transaction | Writes that must commit or roll back together     |
| `AfterRead`                   | Before field-level redaction              | Decorating the returned document                  |
| `AfterOperation`              | After the operation, before commit        | General transactional follow-up                   |
| `AfterError`                  | When the resource operation fails         | Metrics and contextual logging                    |
| `AfterCommit`                 | Only after commit succeeds                | Email, webhooks, indexing, and cache invalidation |

<aside class="callout" data-variant="note">
<strong>Collection and field hooks</strong>
<p>Collection hooks receive the whole operation. Field hooks receive the same context with <code>FieldPath</code> set and support duplicate, validate, change, operation, delete, after-read, and after-commit phases. Field paths are map keys, so rely on slice order within one path—not map order across paths. <code>BeforeRead</code> and <code>AfterError</code> are resource-level phases.</p>
</aside>

## Read HookContext {#hook-context}

Application code should use [`ridu.HookContext`](/reference/ridu/hook-context/). It is the ergonomic alias of [`core.HookContext`](/reference/core/hook-context/), so both names describe the same value.

| Value                   | What it contains                                                                            |
| ----------------------- | ------------------------------------------------------------------------------------------- |
| `Operation`             | The create, duplicate, read, update, delete, publish, or unpublish operation                |
| `Actor`                 | The authenticated document, or `nil` for an anonymous operation                             |
| `Data`                  | Mutable incoming values during write phases                                                 |
| `Document`              | The current result once the phase has one                                                   |
| `Original`              | The persisted value before update or delete                                                 |
| `Context`               | Cancellation, deadline, and the active transaction boundary                                 |
| `Local`                 | Nested operations through the normal engine; pre-commit phases reuse the active transaction |
| `Error`                 | The original failure while `AfterError` runs                                                |
| `Locale` / `AllLocales` | The locale view selected for this operation                                                 |

Fields are [`store.Value`](/reference/store/value/) values rather than `any`. Read strings with [`StringValue()`](/reference/store/value-string-value/) and write them with [`store.String(...)`](/reference/store/string/).

## Normalize before validation {#normalize-input}

`BeforeValidate` is the right place to make user input canonical. The validator sees the value written back to `ctx.Data`.

```go title="content/posts.go" add={18-22}
func trimString(name string) ridu.Hook {
	return func(ctx ridu.HookContext) error {
		value, exists := ctx.Data[name]
		if !exists {
			return nil
		}

		text, valid := value.StringValue()
		if valid {
			ctx.Data[name] = store.String(strings.TrimSpace(text))
		}
		return nil
	}
}

var Posts = ridu.Collection{
	Slug: "posts",
	FieldHooks: map[string]ridu.CollectionHooks{
		"title": {
			BeforeValidate: []ridu.Hook{trimString("title")},
		},
	},
}
```

## Derive values before persistence {#derive-values}

Use `BeforeChange` when a value should be derived from already-valid input and written in the same transaction. Inspect `Operation` when behaviour differs between create and update.

```go title="content/posts.go"
func recordLastEditor(ctx ridu.HookContext) error {
	if ctx.Actor == nil {
		return nil
	}
	if ctx.Operation != ridu.OperationCreate &&
		ctx.Operation != ridu.OperationUpdate {
		return nil
	}

	ctx.Data["lastEditedBy"] = store.String(ctx.Actor.ID)
	return nil
}

var Posts = ridu.Collection{
	Hooks: ridu.CollectionHooks{
		BeforeChange: []ridu.Hook{recordLastEditor},
	},
}
```

## Keep related writes atomic {#nested-operations}

During a transactional phase such as `AfterChange`, `ctx.Local` runs nested work through normal access rules, validation, and hooks while reusing the outer transaction. If the nested call fails, return its error and the outer operation rolls back too.

```go title="content/posts.go"
func writeAuditEntry(ctx ridu.HookContext) error {
	if ctx.Document == nil {
		return nil
	}

	_, err := ctx.Local.Create(
		ctx.Context,
		"audit-log",
		store.Values{
			"document":  store.String(ctx.Document.ID),
			"operation": store.String(string(ctx.Operation)),
		},
		ctx.Actor,
	)
	return err
}

var Posts = ridu.Collection{
	Hooks: ridu.CollectionHooks{
		AfterChange: []ridu.Hook{writeAuditEntry},
	},
}
```

<aside class="callout" data-variant="warning">
<strong>Avoid accidental recursion</strong>
<p>A hook can trigger another hooked collection. Keep the dependency direction clear, and guard on <code>Operation</code> or use a dedicated destination collection when a nested write could re-enter the same hook.</p>
</aside>

## Cross the transaction boundary {#after-commit}

Use `AfterCommit` for work that must not happen when the database rolls back. The write is already durable: a returned error is reported as a committed-hook failure and cannot roll the document back. Configure `Config.AfterCommit` with a dispatcher when effects need retries or a durable worker boundary.

```go title="content/posts.go"
func reindexPost(ctx ridu.HookContext) error {
	if ctx.Document == nil {
		return nil
	}
	return search.Enqueue(ctx.Context, ctx.Document.ID)
}

var Posts = ridu.Collection{
	Hooks: ridu.CollectionHooks{
		AfterCommit: []ridu.Hook{reindexPost},
	},
}
```

<aside class="callout" data-variant="important">
<strong>Transaction boundary</strong>
<p><code>AfterChange</code> and <code>AfterOperation</code> run inside the transaction. <code>AfterCommit</code> runs after it and <code>ctx.Local</code> starts a new transaction there. Do not blindly retry the original write when only an after-commit effect failed.</p>
</aside>

## Observe failures without hiding them {#handle-errors}

`AfterError` runs outside the failed transaction and receives the original failure on `ctx.Error`. Use it for context-rich logging and metrics; returning `nil` does not turn the failed operation into a success.

```go title="content/posts.go"
func countFailure(ctx ridu.HookContext) error {
	metrics.OperationFailure(
		string(ctx.Operation),
		string(ctx.CollectionID),
	)
	log.Printf("ridu operation failed: %v", ctx.Error)
	return nil
}

var Posts = ridu.Collection{
	Hooks: ridu.CollectionHooks{
		AfterError: []ridu.Hook{countFailure},
	},
}
```

See [`ridu.CollectionHooks`](/reference/ridu/collection-hooks/) for every phase and [`ridu.Hook`](/reference/ridu/hook/) for the callback contract.
