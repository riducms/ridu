---
title: 'Relationships, joins, and population'
description: 'Reference one or many documents, model polymorphic targets, expose inverse joins, and populate safely.'
product: core
eyebrow: 'Content model'
order: 65
aliases:
  [
    'relationship',
    'reference',
    'relationTo',
    'hasMany',
    'polymorphic',
    'join',
    'populate',
    'inverse relationship'
  ]
navigation:
  section: 'Model content'
  order: 40
  title: 'Relationships & joins'
---

Relationship fields store forward references. Join fields derive the inverse view by querying a
target collection. Population replaces stored references with authorized target documents in an
operation response. This distinction determines storage, mutation, and read cost.

## Store one relationship {#one}

```go
field.Relationship("author", "users").
	Required().
	OnDelete(field.ReferenceDeleteRestrict)
```

The stored value is the target document ID. The target collection must exist, and writes validate
that the selected document exists, passes target read access, and satisfies any option filter. A
required reference cannot use nullify-on-delete because automatic cleanup would create a value
ordinary validation rejects.

## Store many or polymorphic relationships {#many-polymorphic}

Use `Relationships` for a list to one collection, or `PolymorphicRelationships` for a polymorphic list:

```go
field.Relationships("reviewers", "users")

field.PolymorphicRelationships("subjects", "posts", "media")
```

A polymorphic wire value carries both the target collection and document ID so an ID collision
between collections is unambiguous. Generated TypeScript keeps that discriminated shape. Singular
and list cardinality are schema contracts; changing them requires a reviewed migration.

Relationship and upload references can be localized. The selected locale controls which forward
reference is read or changed, and population uses the same locale/fallback chain.

## Narrow author choices {#option-filters}

Option filters derive server-validated predicates from the current document. A category can, for
example, limit an editor picker to users with a matching category:

```go
field.Relationship("editor", "users").
	FilterOptionRules(field.OptionFilter(
		"category", field.FilterEquals, "category",
	))
```

Add more rules for multiple conditions, or use `OptionFilterFor` for polymorphic targets. Supported
comparisons are equals, not-equals, like, contains, and ordered greater/less variants.

The admin uses these predicates when listing choices, and the operation engine revalidates them on
write. They never grant target read access. A target must pass both ordinary authorization and the
option predicate; hiding a picker is not security.

## Choose delete behavior {#delete-behavior}

`OnDelete` controls current documents when a target is permanently deleted:

| Action                    | Behavior                                                      |
| ------------------------- | ------------------------------------------------------------- |
| `ReferenceDeleteNullify`  | Clear a singular value or remove matching members from a list |
| `ReferenceDeleteRestrict` | Reject the target deletion while a surviving reference exists |

Cascade is not supported. Required references resolve to restrict. Version snapshots are immutable
and are not rewritten when a current target is deleted; restoring old content can therefore expose
a historical reference that must still pass current validation and access rules.

Soft deletion moves the target into trash rather than applying permanent-delete cleanup. Plan
retention and restore behavior before choosing nullify versus restrict.

## Add an inverse join {#inverse-join}

Suppose each post stores `category`. A category can display the posts that point back to it without
duplicating IDs:

```go
ridu.Collection{
	Slug: "categories",
	Fields: field.Fields{
		field.Text("name").Required(),
		field.Join("posts", "posts", "category").
			Limit(20).
			DefaultColumns("title", "status", "updatedAt").
			DefaultSort("-updatedAt").
			AllowCreate(true),
	},
}
```

Join fields are read-only computed output and are supported on collections, not globals. The target
path must be a compatible relationship back to the source collection. `Limit` is between 1 and 100. Columns, default sort, and inline-create preference configure the admin table; they do not
authorize target reads or creates.

Reads query the target collection through its access rule, hooks, localization, and redaction.
Callers can omit expensive join output with the local API's `OutputFields` selection.

## Mutate a join atomically {#mutate-join}

The join itself is derived, so changing membership updates the target documents' forward field.
Use the delta operation:

```ts
const result = await client.mutateJoin(
	'categories',
	category.id,
	'posts',
	{
		additions: ['post_123', 'post_456'],
		removals: ['post_789']
	}
);

console.log(result.added, result.removed, result.doc.posts);
```

One request accepts between 1 and 100 unique target IDs. The same ID cannot appear in both deltas.
Ridu locks and rechecks the source and targets, requires source-field visibility plus target update
access, runs every target update through normal validation/hooks, and rolls the complete set back on
failure. A concurrent reparent produces `conflict` rather than silently stealing membership.

## Populate forward references {#population}

Without population, a relationship returns its stored ID/reference. Ask for target documents only
when the response needs them:

```ts
const post = await client.find('posts', 'post_123', {
	populate: {
		author: { depth: 1, select: { name: true, avatar: true } }
	}
});
```

Population batches work, reapplies target access and field redaction, respects localization, and
supports nested relationships under groups, arrays, and blocks. The operation-wide limits are 64
explicit paths, depth 5, 256 expanded schema paths, and 4,096 materialized related documents.
See [Querying data](/docs/querying/#populate) for REST encoding and cost controls.

## Migration implications {#migrations}

Removing or narrowing a target list, changing cardinality, removing a relationship, or shrinking a
nested root can leave current values or version snapshots that later bind to reused identities.
Ridu's migration planner rejects these unsafe reference-shape changes.

Add targets additively where possible. For destructive changes, use an application-owned data
transition, verify it on restored data, then create the schema artifact. Confirmed collection/field
renames use stable identity and schema-addressed rewrites. Read [PostgreSQL migrations](/docs/migrations/)
before evolving live relationships.

See [Fields](/docs/fields/) for all constructors and the [`field` reference](/reference/field/) for
exact typed options.
