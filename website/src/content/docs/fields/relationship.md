---
title: 'Relationship field'
description: 'Link a document to an author, category, or other related documents.'
product: core
eyebrow: 'Relationship and media fields'
order: 77
aliases:
  [
    'field.Relationship',
    'relationTo',
    'hasMany relationship',
    'polymorphic relationship'
  ]
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#Relationship',
    'go:github.com/riducms/ridu/field#Relationship',
    'go:github.com/riducms/ridu/field#PolymorphicRelationship'
  ]
navigation:
  section: 'Model content'
  parent: fields
  order: 170
  title: 'Relationship'
---

Use `field.Relationship` to link one document to another, such as a post to its author. When you
save, Ridu checks that the related document exists and that the current user can read it. Reads
return its ID; request population to include the related document too.

## In the admin {#admin-behavior}

![A populated Author relationship field in the Ridu admin referencing the documentation user.](../../../../../docs/assets/fields/relationship.png)

_The picker shows the target label; stored data contains a reference that can be populated on read._

## Link to one or several documents {#example}

```go title="content/posts.go"
field.Relationship("author", "users").
	Required().
	OnDelete(field.ReferenceDeleteRestrict)
```

A relationship to one document in one collection stores its document ID. Use `field.Relationships("reviewers", "users")`
for an ordered list of users.

Use `field.PolymorphicRelationship("subject", "posts", "media")` when the selected document can
come from more than one collection. Its value contains both `relationTo` and `id` to identify the
collection and document. Use `field.PolymorphicRelationships` to select several such documents.

## Configuration {#configuration}

| Constructor or method                                                                  | What it controls                                                               |
| -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `field.Relationship(name, collection)`                                                 | Stores one document ID from one target collection.                             |
| `field.Relationships(name, collection)`                                                | Stores an ordered list of IDs from one target collection.                      |
| `field.PolymorphicRelationship(name, collections...)`                                  | Stores one `{ relationTo, value }` reference from several allowed collections. |
| `field.PolymorphicRelationships(name, collections...)`                                 | Stores an ordered list of polymorphic references.                              |
| `.Required()`                                                                          | Requires one reference, or a non-empty list for plural fields.                 |
| `.FilterOptionRules(rules...)`                                                         | Narrows picker choices and server admission with finite query predicates.      |
| `.OnDelete(field.ReferenceDeleteRestrict)` / `.OnDelete(field.ReferenceDeleteNullify)` | Rejects a hard delete while referenced, or clears/removes matching references. |
| `.Index()` / `.Unique()`                                                               | Available on a singular, single-target relationship.                           |
| `.Localized()`                                                                         | Stores the reference or list separately for each content locale.               |
| `.Validate(...)`, `.LiveValidate(...)`, `.Access(...)`, `.Hooks(...)`                  | Adds application rules and lifecycle behavior.                                 |

## Filter the available choices {#option-filters}

```go title="content/posts.go"
field.Relationship("reviewer", "users").
	FilterOptionRules(field.OptionFilter(
		"team", field.FilterEquals, "editorialTeam",
	))
```

This picker shows users whose `team` matches the current document’s `editorialTeam`. The server
checks the same rule when saving. Authors also need read access to the selected user.

Use `OptionFilterValue` to compare against a fixed value, or `OptionFilterFor` to apply a rule to
a specific collection in a relationship that supports several collection types.

## Choose what happens when a related document is deleted {#behavior}

`ReferenceDeleteNullify` clears an optional singular value or removes list members when the target
is permanently deleted. `ReferenceDeleteRestrict` blocks that deletion while a current reference
exists. Required references always restrict. Version snapshots remain immutable.

## Read and translate relationships

Filter by the related ID or request `populate` through the Local API or SDK to read the related
document. Population respects access rules, localization, and the requested depth limit.
`.Localized()` lets each content locale select a different document.

## Common mistakes {#troubleshooting}

- The target collection must be declared, and upload-only semantics belong in [Upload](/docs/fields/upload/).
- Changing from one document to a list, or removing an allowed collection, requires migrating
  existing values.
- Picker visibility never replaces authorization.
- Use [Join](/docs/fields/join/) for the inverse view; do not duplicate both sides as manually
  synchronized ID lists.

See [`field.Relationship`](/reference/field/relationship/) and the complete
[relationship and population guide](/docs/relationships/).
