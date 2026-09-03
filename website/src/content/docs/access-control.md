---
title: 'Access control'
description: 'Allow, deny, or filter operations with transaction-scoped Go rules.'
product: core
eyebrow: 'Runtime'
order: 70
aliases: ['SiblingData', 'field sibling data', 'nested field access']
navigation:
  section: 'Work with data'
  order: 20
  title: 'Access control'
---

## Access decisions {#decisions}

A collection rule receives the actor, operation, target ID, submitted data, locale, and local API. Return `ridu.Allow()` or `ridu.Deny()` for any rule. Return `ridu.Where(expression)` only where Ridu can attach a document predicate to an atomic document read, version-history read, update, delete, or unlock query.

Keep reusable rules in an ordinary Go file beside your content model. This example allows anyone to read, requires a session to create, and builds an ownership predicate from the authenticated document ID.

```go title="content/access.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func publicRead(ridu.AccessContext) (ridu.AccessDecision, error) {
	return ridu.Allow(), nil
}

func signedIn(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor == nil {
		return ridu.Deny(), nil
	}
	return ridu.Allow(), nil
}

func ownDocuments(path query.Path) ridu.AccessRule {
	return func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil {
			return ridu.Deny(), nil
		}
		return ridu.Where(
			query.Equal(path, query.String(ctx.Actor.ID)),
		), nil
	}
}

func actorHasRole(actor *store.Document, allowed ...string) bool {
	if actor == nil {
		return false
	}
	role, ok := actor.Values["role"].StringValue()
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		if role == candidate {
			return true
		}
	}
	return false
}
```

## Collection rules {#collection-rules}

Attach those rules to the collection operation they protect. The path supplied to `ownDocuments` is the stored relationship field, so an author can update or delete only rows whose `author` ID matches their own document ID.

```go title="content/posts.go" add={24-30}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
)

func Posts() ridu.Collection {
	authorPath, err := query.NewPath("author")
	if err != nil {
		panic(err)
	}

	return ridu.Collection{
		Slug: "posts",
		Fields: []field.Definition{
			field.Text("title", field.Required()),
			field.Relationship("author", field.To("users"), field.Required()),
			field.Textarea("internalNotes"),
		},
		Access: ridu.CollectionAccess{
			Create: signedIn,
			Read:   publicRead,
			Update: ownDocuments(authorPath),
			Delete: ownDocuments(authorPath),
		},
		FieldAccess: postFieldAccess(),
	}
}
```

This example keeps `Posts` as a function because it performs fallible path construction before it
can return the collection. Ordinary static collection definitions should be package variables.

<aside class="callout" data-variant="important">
<strong>Filtered means filtered</strong>
<p>A <code>Where</code> decision stays attached to the atomic store query. Ridu does not fetch a document first and check ownership afterwards. Creates, global rules, and admin entry accept only <code>Allow</code> or <code>Deny</code>.</p>
</aside>

## Field access {#field-access}

Field rules return a boolean. A denied write is rejected; a denied read is redacted from the returned document. Presentation options such as `field.ReadOnly()` are not authorization.

Field-access map keys are authored field paths. `postFieldAccess()` is called by `Posts()` above, keeping the policy readable without hiding it in an anonymous collection literal.

```go title="content/field_access.go"
package content

import "github.com/riducms/ridu"

func postFieldAccess() map[string]ridu.FieldAccess {
	return map[string]ridu.FieldAccess{
		"internalNotes": {
			Create: func(ctx ridu.FieldAccessContext) (bool, error) {
				return actorHasRole(ctx.Actor, "admin"), nil
			},
			Read: func(ctx ridu.FieldAccessContext) (bool, error) {
				return actorHasRole(ctx.Actor, "editor", "admin"), nil
			},
			Update: func(ctx ridu.FieldAccessContext) (bool, error) {
				return actorHasRole(ctx.Actor, "admin"), nil
			},
		},
	}
}
```

Here only administrators may create or update `internalNotes`; editors may read it. Omitting the
`Create` rule would leave that write allowed by default, even if `Update` were restricted.

## Read sibling values in a field rule {#sibling-data}

`ctx.SiblingData` contains the values beside the field currently being authorized. For a root
field, that is the document's root value map. For a nested field, Ridu narrows it to that field's
Group, Array row, or Block instance.

This collection makes a link URL visible to everyone unless the checkbox in the same row marks it
as members-only:

```go title="content/pages.go" focus={20-24}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Pages = ridu.Collection{
	Slug: "pages",
	Fields: []field.Definition{
		field.Array("links", field.Fields(
			field.Text("label", field.Required()),
			field.Text("url", field.Required()),
			field.Checkbox("membersOnly", field.Default(false)),
		)),
	},
	FieldAccess: map[string]ridu.FieldAccess{
		"links.url": {
			Read: func(ctx ridu.FieldAccessContext) (bool, error) {
				membersOnly, _ := ctx.SiblingData["membersOnly"].BooleanValue()
				if !membersOnly {
					return true, nil
				}
				return ctx.Actor != nil, nil
			},
		},
	},
}
```

The map key remains the authored path `links.url`. When Ridu evaluates a particular row,
`ctx.RuntimePath` is concrete—for example, `links.2.url`—and `SiblingData` is that row's `label`,
`url`, and `membersOnly` values. It is a detached snapshot: changing it does not change the
document. Use a hook when you intend to mutate submitted data.

See [`ridu.FieldAccessContext`](/reference/ridu/field-access-context/) for the current value,
document, original document, locale, and transaction-reusing Local API available alongside sibling
data.
