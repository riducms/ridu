---
title: 'Hook and value performance'
description: 'Write hooks, validators, and nested-value code that avoids repeated work and unnecessary copies.'
product: core
eyebrow: 'Performance'
order: 232
aliases:
  [
    'hook performance',
    'validator performance',
    'embedded hooks',
    'store value performance'
  ]
navigation:
  section: 'Develop & operate'
  parent: performance
  order: 20
  title: 'Hooks and values'
---

Hooks and validators run inside the operation that triggered them. Their database calls, network
waits, scans, and allocations therefore become part of save or read latency. Repeated fields add
one callback occurrence per matching row or block, so callback work should usually be proportional
to the value it handles.

## Choose the cheapest correct phase {#phases}

| Need                                                        | Use                                                         | Transaction effect                                                                   |
| ----------------------------------------------------------- | ----------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| Normalize malformed or omitted input before built-in checks | Field or resource `BeforeValidate`                          | Runs before validation and may also run on reads/deletes; guard `ctx.Operation`.     |
| Change one typed field before it is saved                   | Field `BeforeChange`                                        | Return `Keep` or a replacement; final validation still runs.                         |
| Calculate several stored fields                             | Resource `BeforeChange`                                     | Mutate `ctx.Data` once rather than coordinating unrelated field callbacks.           |
| Save a related Ridu document atomically                     | Resource `AfterChange` or `AfterOperation` with `ctx.Local` | Shares the transaction when passed `ctx.Context`; failure rolls both writes back.    |
| Format a response                                           | `AfterRead`                                                 | Runs for every returned document and before final field redaction.                   |
| Send email, a webhook, or update an external index          | `AfterCommit`                                               | Runs after commit and cannot roll the document back. Use a task for durable retries. |

Keep external network work out of pre-commit phases unless its success is truly required before the
document commits. Pass `ctx.Context` to every database or network call so cancellation and
deadlines stop work the caller no longer needs.

## Return `Keep` for an unchanged field {#keep}

Ridu tracks immutable value backing through operation passes. Returning `operation.Keep` lets it
retain the current branch. Do not return an equal replacement merely to signal success.

```go title="content/titles.go" focus={17-27}
package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func trimTitle(
	_ operation.Context,
	value operation.Value[string],
) (operation.Change[string], error) {
	title, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	trimmed := strings.TrimSpace(title)
	if trimmed == title {
		// Keep preserves the current immutable value and avoids replacement work.
		return operation.Keep[string](), nil
	}
	return operation.Replace(operation.Present(trimmed)), nil
}

var Title = field.Text("title").Hooks(field.Hooks[string]{
	BeforeChange: []field.Transform[string]{trimTitle},
})
```

| Callback result                               | Meaning                                                | Relative cost                                 |
| --------------------------------------------- | ------------------------------------------------------ | --------------------------------------------- |
| `operation.Keep[T]()`                         | Leave the current logical value and backing unchanged. | Lowest when no change is needed.              |
| `operation.Replace(operation.Present(value))` | Publish a new logical value for this field.            | Revalidates and updates the affected branch.  |
| `operation.Replace(operation.Empty[T]())`     | Clear this field.                                      | Still runs requiredness and final validation. |
| Non-nil `error`                               | Stop the operation; ignore any returned replacement.   | Rolls back pre-commit database work.          |

## Read immutable values without copying {#read-values}

`operation.View` and nested `store.Value` containers are immutable snapshots. Reading them is safe
without defensive copies. Follow one object member at a time and iterate lists directly:

```go title="content/count_links.go" focus={6-14}
package content

import "github.com/riducms/ridu/operation"

func countNamedLinks(ctx operation.Context) int {
	links := ctx.Root.Get("links")
	count := 0
	for item := range links.Elements() {
		label, ok := item.Get("label").StringValue()
		if ok && label != "" {
			count++
		}
	}
	return count
}
```

| Read method                       | Returns                             | Copies a container?          |
| --------------------------------- | ----------------------------------- | ---------------------------- |
| `view.Lookup(name)`               | Direct child and membership flag    | No                           |
| `view.Get(name)`                  | Direct child, or `Null` when absent | No                           |
| `view.String(name)`               | Direct string and type-match flag   | No                           |
| `value.Entries()`                 | Iterator over object members        | No                           |
| `value.Elements()`                | Iterator over list items            | No                           |
| `value.ListItem(index)`           | One list item                       | No                           |
| `value.CopyObject()`              | Detached mutable `store.Values`     | Yes                          |
| `value.CopyList()`                | Detached mutable `[]store.Value`    | Yes                          |
| `value.CopyDocument()`            | Detached populated document         | Yes                          |
| `value.WithListItem(index, item)` | New list sharing unchanged branches | Copies only the changed path |

Use a `Copy...` method at the point where another API requires a mutable Go container or where you
intend to edit it. Repeatedly calling `CopyList` or `CopyObject` inside a loop materializes the same
container again. The top-level `store.Values` map is mutable Go state; call `store.CloneValues`
before editing a separate top-level copy.

## Keep embedded callbacks linear {#embedded}

A field hook attached inside an array, Blocks field, or plugin-declared embedded tree runs for each
matching occurrence. Ridu now traverses each embedded tree in a batch and applies scalar
replacements without cloning the whole enclosing list each time. Application code can still turn a
linear operation into quadratic work by rescanning the complete root list from every row callback.

| Pattern                                        | Cost as rows grow                             | Better approach                                                                                          |
| ---------------------------------------------- | --------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| Read `ctx.Siblings` or the current typed value | Work local to this occurrence                 | Preferred for per-row rules.                                                                             |
| Read a direct root scalar from `ctx.Root`      | Constant lookup                               | Fine for a shared setting.                                                                               |
| Scan all N rows from each of N row hooks       | Roughly N² reads                              | Calculate once in a resource hook, or maintain an application-owned lookup outside the per-row callback. |
| Copy the full list from every row hook         | N full materializations plus replacement work | Return `Keep` when unchanged; replace only the current field.                                            |
| Write a related document from every row hook   | N nested operations and lifecycle runs        | Batch the coordination in one resource hook when the domain permits it.                                  |

Stable row keys identify retained array and block occurrences after reordering. Use `ctx.Siblings`
and `ctx.Prior` rather than searching by the current array index. Shared block definitions share
schema metadata; their access rules and hooks still run independently at each placement and
occurrence.

## Avoid repeated reads inside one callback {#callback-reads}

`ctx.Local.FindByID` preserves the actor, exact locale, cancellation, and active transaction. Ridu
does not promise an application-level memoized result for arbitrary callback reads. If several
checks in one callback need the same target, read it once and reuse the returned immutable
document. If many occurrences need the same lookup, reconsider whether one resource hook can do
the work once.

Do not hide mutable cross-request caches inside field definitions. If a cache is necessary, define
its authorization dimensions, size bound, expiration, invalidation, and concurrency behavior, then
measure it under the expected workload.

## Reuse block definitions {#shared-blocks}

Register a block once when several fields use the same schema. `References` also controls the
picker order for that field:

```go title="content/config.go" focus={8-16,22-24}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Hero = field.Block{
	Slug: "hero",
	Fields: field.Fields{
		field.Text("heading").Required(),
		field.Textarea("summary"),
	},
}

func Config() ridu.Config {
	return ridu.Config{
		Name:   "Editorial",
		Blocks: []field.Block{Hero},
		Collections: []ridu.Collection{{
			Slug: "pages",
			Fields: field.Fields{
				field.Blocks("layout").References("hero"),
			},
		}},
	}
}
```

Use inline `field.Blocks("layout", Hero)` when a definition belongs to one field. Do not register
and inline the same slug on one field. See [Blocks](/docs/fields/blocks/#references) for labels,
localization, migrations, and generated types.

## Profile before introducing a cache {#profile}

Start with a CPU and allocation profile of the actual operation. A cache cannot fix an O(N²)
callback, an unnecessary copy, or a response that populates unused content. It can also retain
caller-specific documents longer than expected. [Measure performance](/docs/performance/measurement/)
shows the repository benchmark commands and the measurements to collect for an application.
