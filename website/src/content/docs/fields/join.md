---
title: 'Join field'
description: 'Expose an access-controlled inverse view of documents that point to the current collection.'
product: core
eyebrow: 'Computed and plugin fields'
order: 82
aliases: ['field.Join', 'inverse relationship', 'JoinLimit', 'mutateJoin']
relatedSymbolIds:
  ['go:github.com/riducms/ridu/field#Join', 'go:github.com/riducms/ridu/field#JoinLimit']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Computed & plugin'
  order: 220
  title: 'Join'
---

Use `field.Join` for the inverse side of a singular relationship. It queries documents that point
to the current document instead of storing a second synchronized ID list.

## In the admin {#admin-behavior}

![A populated Articles in this category Join field in the Ridu admin showing the related article row.](../../../../../docs/assets/fields/join.png)

_The table is computed from forward relationships, so the category stores no duplicate list of post IDs._

## Smallest working example {#example}

```go title="content/categories.go"
var Categories = ridu.Collection{
	Slug: "categories",
	Fields: []field.Definition{
		field.Text("name", field.Required()),
		field.Join("posts", "posts", "category"),
	},
}

var Posts = ridu.Collection{
	Slug: "posts",
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Relationship("category", field.To("categories")),
	},
}
```

The constructor arguments are the output field name, target collection, and target relationship
path. That path must identify a singular, non-polymorphic relationship back to the source
collection.

## Configure the admin view {#options}

```go title="content/categories.go"
field.Join(
	"posts", "posts", "category",
	field.JoinLimit(20),
	field.JoinColumns("title", "status", "updatedAt"),
	field.JoinDefaultSort("-updatedAt"),
	field.JoinAllowCreate(true),
)
```

`JoinLimit` is between 1 and 100. Columns, sorting, and inline-create availability describe the
admin table; target read/create access still applies. Joins must live at a collection root
and are not supported on globals.

The value appears in generated output but never in create/update input. Reads apply target access,
hooks, localization, and field redaction. Select output fields to omit an expensive join when a
consumer does not need it. Use SDK `mutateJoin` for an explicit atomic addition/removal; it updates
the target documents' forward relationships through validation and access.

## Common mistakes {#troubleshooting}

- Do not create and manually synchronize a second forward relationship.
- `JoinAllowCreate(true)` is an affordance, not permission.
- Changing the target/on path can be a relationship-shape migration even though the Join itself is
  response-only.

See [`field.Join`](/reference/field/join/) and [Relationships and joins](/docs/relationships/#inverse-join).
