---
title: 'Virtual field'
description: 'Return a typed value computed by trusted Go code without accepting or storing it.'
product: core
eyebrow: 'Computed and plugin fields'
order: 83
aliases:
  [
    'field.Virtual',
    'computed field',
    'ComputedContext',
    'ValueString',
    'ctx.Document.Values',
    'sibling values'
  ]
relatedSymbolIds:
  ['go:github.com/riducms/ridu/field#Virtual', 'go:github.com/riducms/ridu/core#Computed']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Computed & plugin'
  order: 230
  title: 'Virtual'
---

Use `field.Virtual` when trusted Go code derives a response value from a document or another
access-controlled read. Virtual values appear in generated output contracts but are never accepted
as create/update data and are not stored.

## In the admin {#admin-behavior}

![A read-only Computed label virtual field in the Ridu admin showing a value resolved by Go code.](../../../../../docs/assets/fields/virtual.png)

_The admin renders resolver output as read-only; create and update inputs never accept or persist it._

## Define the field and resolver {#example}

```go title="content/people.go" focus={18-20}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

var People = ridu.Collection{
	Slug: "people",
	Fields: []field.Definition{
		field.Text("firstName", field.Required()),
		field.Text("lastName", field.Required()),
		field.Virtual("displayName", field.ValueString),
	},
	Computed: map[string]ridu.Computed{
		"displayName": func(ctx ridu.ComputedContext) (store.Value, error) {
			first, _ := ctx.Document.Values["firstName"].StringValue()
			last, _ := ctx.Document.Values["lastName"].StringValue()
			return store.String(first + " " + last), nil
		},
	},
}
```

### Read sibling values {#sibling-data}

Virtual fields are root fields, so their sibling values live in `ctx.Document.Values`. In the
example, `displayName` reads the stored `firstName` and `lastName` siblings with `StringValue()`.
The second return value reports whether the value is actually a string; the example can ignore it
because both source fields are required Text fields.

Use `BooleanValue()` or `NumberValue()` for other scalar siblings, `ObjectValue()` for a Group or
named Tabs value, and `Values()` for an Array or Blocks value. A Relationship or Upload sibling
contains its stored reference shape—an ID, ID list, or polymorphic object—not an automatically
populated document. Use `ctx.Local` when the resolver also needs to read that related document
through its normal access rules.

Choose `ValueString`, `ValueNumber`, `ValueBoolean`, or `ValueJSON`. The resolver must return the
matching `store.Value`. Missing resolvers fail startup; a mismatched runtime value fails the
operation.

Virtual fields must live at a collection or global root. Resolvers receive the operation, actor and
auth collection, current document, locale selection, context, and access-controlled Local API. Use
that Local API for additional reads.

## Cost, access, and selection {#behavior}

Unselected virtual fields do not run. Consumers should select only expensive computed output they
need. A resolver is trusted application code, so bound its work and avoid network calls on every
list row unless caching and failure behavior are defined. The output remains subject to field
read visibility.

## Common mistakes {#troubleshooting}

- Do not try to filter or sort by a value that is never stored. Materialize/index a real field when
  the database must query it.
- Avoid nondeterministic or secret-bearing output unless an access rule permits it.
- A Virtual is not an admin-only UI element; use [UI](/docs/fields/ui/) for presentation with no
  response value.

See [`field.Virtual`](/reference/field/virtual/) and [`ridu.Computed`](/reference/core/computed/).
