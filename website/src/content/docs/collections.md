---
title: 'Collections and globals'
description: 'Define repeatable documents and singleton configuration, then add only the capabilities each resource needs.'
product: core
eyebrow: 'Content model'
order: 50
navigation:
  section: 'Model content'
  order: 20
  title: 'Collections & globals'
---

Collections hold many documents—posts, products, people, media. Globals hold exactly one logical
document—site settings, navigation, or a home-page composition. Both use the same fields, access,
validation, hooks, localization, and field redaction.

`ridu.Collection` and `ridu.Global` are typed Go config values. Their serializable shape becomes
generated Go and TypeScript contracts, OpenAPI, and the admin model.

## Define a collection {#define-a-collection}

A collection slug is its durable public address in REST, generated clients, relationships, and the
admin. Presentation belongs in `Labels` and `Admin`; runtime behaviour belongs in fields, access,
hooks, and explicit capabilities.

```go title="content/articles.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Articles = ridu.Collection{
	Slug: "articles",
	Labels: ridu.CollectionLabels{
		Singular: "Article",
		Plural:   "Articles",
	},
	Admin: ridu.CollectionAdmin{
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "category", "summary"},
		Group:          "Editorial",
		Description:    "Long-form stories published on the site.",
	},
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Textarea("summary"),
		field.Relationship("category", field.To("categories")),
		field.Group("seo", field.Fields(
			field.Text("slug", field.Required()),
		)),
	},
	Indexes: []ridu.CollectionIndex{
		{Fields: []string{"category", "seo.slug"}, Unique: true},
	},
}
```

Register the value in `ridu.Config.Collections`. Config resolution rejects unknown
relationship targets, invalid admin fields, duplicate slugs, and unsupported index paths.

## Collection configuration map {#collection-config}

| Property                              | Purpose                                                                                          |
| ------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `Slug`                                | URL-safe public resource name. It is used by APIs and relationships, not just the admin.         |
| `Labels`                              | Optional singular/plural author-facing names. Ridu derives readable defaults from the slug.      |
| `Admin`                               | Navigation, list, hierarchy, folder, and live-preview presentation. It never grants access.      |
| `Fields`                              | Stored, layout, computed, relationship, upload, and plugin field definitions.                    |
| `Indexes`                             | Ordered compound store indexes; use field-level `Unique()` for one-field uniqueness.             |
| `Auth`, `AuthConfig`                  | Make documents identities and configure passwords, sessions, recovery, API keys, and strategies. |
| `Upload`, `UploadConfig`              | Make documents file records backed by `Config.Storage`.                                          |
| `Versions`, `VersionConfig`           | Record snapshots and optionally enable draft authoring.                                          |
| `Trash`                               | Replace ordinary delete with recoverable trash, restore, and permanent-delete operations.        |
| `LockDocuments`, `DocumentLockConfig` | Persist editor locks and controlled takeover for coordinated authoring.                          |
| `Access`, `FieldAccess`               | Authorize resource operations and redact or reject individual field paths.                       |
| `Hooks`, `FieldHooks`                 | Run deterministic lifecycle callbacks, including transaction-reusing nested local calls.         |
| `Computed`                            | Resolve virtual response fields after storage without persisting them.                           |

See [Fields](/docs/fields/) for every builder and option. A field's name is part of stored and
generated contracts; its label and description affect presentation only.

## Admin metadata is presentation {#admin-metadata}

`CollectionAdmin` configures browsing and editing without changing API authorization:

| Property         | Current behaviour                                                                                                |
| ---------------- | ---------------------------------------------------------------------------------------------------------------- |
| `UseAsTitle`     | Names a direct field used as the document label in lists, relationships, and upload flows.                       |
| `DefaultColumns` | Names unique direct fields shown by a fresh list workspace. Authors can persist their own workspace preferences. |
| `Group`          | Groups the collection in admin navigation.                                                                       |
| `Description`    | Adds author-facing context for the resource.                                                                     |
| `FolderField`    | Names a singular, non-polymorphic relationship used to filter a collection into folders.                         |
| `ParentField`    | Names a singular relationship back to the same collection and enables the hierarchy view.                        |
| `LivePreview`    | Configures the editor preview URL and optional named viewport sizes.                                             |

`UseAsTitle`, columns, folder, and parent fields must name existing direct fields. Folder
relationships may target another collection; a parent relationship must target the collection
itself. Live-preview URL templates accept `{id}`, `{collection}`, and
`{field:path.to.value}` placeholders. Read [Admin](/docs/admin/) and
[Editorial workflows](/docs/editorial-workflows/) for the authoring experience.

> [!IMPORTANT]
> Admin visibility and capability responses are interface hints, never authorization. Every API
> request independently evaluates collection and field access inside its store transaction.

## Compound indexes {#indexes}

Use `CollectionIndex` when the tuple, rather than one field, is indexed or unique:

```go
Indexes: []ridu.CollectionIndex{
	{Fields: []string{"tenant", "seo.slug"}},
	{Fields: []string{"tenant", "externalID"}, Unique: true},
},
```

An index contains 2–32 unique, ordered paths. Paths can pass through non-repeated groups and end at
a supported scalar or a singular, non-polymorphic relationship/upload reference. Arrays, blocks,
rich text, objects, has-many references, and polymorphic references are not index terminals.

Official stores use PostgreSQL-style `NULLS DISTINCT` semantics: a unique tuple can appear more
than once when any component is null or absent. Localized values are unique per exact locale, not
across a fallback result. Trashed documents leave the active unique set; restoring one can fail
with `conflict` if another active document has claimed its tuple.

## Opt-in capabilities {#capabilities}

A collection supports create, duplicate, find, list, update, delete, filtering, pagination,
selection, population, access evaluation, hooks, and generated contracts. Flags add features:

| Configuration         | Adds                                                                                                                 | Required runtime contract                                                                         |
| --------------------- | -------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `Auth: true`          | Password credentials, sessions, account lockout, recovery/verification, optional API keys, custom request strategies | Store implements `store.AuthStore`; the production server also expects auth maintenance support   |
| `Upload: true`        | Server-owned file metadata, validation, image variants, delivery, regeneration, and cleanup                          | `Config.Storage`, `StorageNamespace`, and upload-aware store capabilities                         |
| `Versions: true`      | `_revision`, `_status`, retained snapshots, restore, and scheduled collection publishing                             | Store implements `store.VersionTransaction`; scheduling needs durable task or publish-job support |
| `Trash: true`         | Soft delete, trash-only reads, restore, permanent delete, empty trash, and cleanup of owned state                    | Store implements the trash and reference-cleanup contracts                                        |
| `LockDocuments: true` | Inspect/acquire/release locks and authorized takeover                                                                | Store implements `store.DocumentLockStore`                                                        |

All three official database adapters supply these store capabilities; MongoDB does so inside its
[bounded production profile](/docs/mongodb/). Application construction fails if an adapter cannot
support an enabled feature.
Continue with [Authentication](/docs/authentication/),
[Uploads](/docs/uploads/), [Drafts and versions](/docs/drafts-and-versions/), and
[Editorial workflows](/docs/editorial-workflows/) for each feature.

## Access, hooks, and computed output {#runtime-behaviour}

`CollectionAccess` has independent `Admin`, `Create`, `Read`, `ReadVersions`, `Update`, `Publish`,
`Unpublish`, `Delete`, and `Unlock` rules. A nil `ReadVersions` rule falls back to `Read`; nil
`Publish`, `Unpublish`, and `Unlock` rules fall back to `Update`. Read and mutation rules can return
`ridu.Where(...)`, which the store must combine atomically with the caller's filter.

`FieldAccess` and `FieldHooks` maps use authored paths such as `seo.slug`. Repeated field
occurrences also receive a concrete runtime path. `Computed` values are response-only and can be
limited with local API output selection. Learn the order and transaction boundaries in
[Access control](/docs/access-control/) and [Hooks](/docs/hooks/).

## Globals {#globals}

Globals use singleton routes and generated singleton handles. Reading a global before its first
write returns a schema-shaped value with defaults; the first update persists the row under the
global slug.

```go title="content/globals.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var SiteSettings = ridu.Global{
	Slug:  "site-settings",
	Label: "Site settings",
	Admin: ridu.GlobalAdmin{
		Group:       "Settings",
		Description: "Site-wide identity and support details.",
	},
	Fields: []field.Definition{
		field.Text("siteName", field.Required()),
		field.Email("supportEmail"),
	},
	Access: ridu.GlobalAccess{
		Read:   publicRead,
		Update: administratorsOnly,
	},
}
```

Register it in `ridu.Config.Globals`. A global has no collection-style create, duplicate, list,
delete/trash, auth, upload, document-lock, folder, hierarchy, or compound-index configuration.
Its supported surface is smaller:

| Concern               | Collection                                                      | Global                                                                                        |
| --------------------- | --------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| Cardinality           | Many documents with generated IDs                               | One document whose ID is its slug                                                             |
| Base operations       | Create, duplicate, find/list, update, delete, bulk              | Read and update                                                                               |
| Access                | `CollectionAccess` per CRUD/version/admin/lock operation        | `GlobalAccess.Read`, `ReadVersions`, `Update`, `Publish`, and `Unpublish`                     |
| Admin                 | Title, columns, folders, hierarchy, group, description, preview | Group, description, preview                                                                   |
| Optional capabilities | Auth, upload, versions/drafts, trash, locks                     | Versions/drafts only                                                                          |
| Versions              | Per-document history, publish/unpublish, restore, scheduling    | Singleton history, publish/unpublish, restore; scheduled global publishing is not implemented |

For a missing singleton, a filtered `GlobalAccess.Update` decision cannot match a row; the first
update therefore requires an unconditional `Allow`. Once persisted, filtered read/update/version
decisions are applied atomically to the row or each snapshot.

## Renames and migrations {#renames}

Rename the authored slug or field name, regenerate, then run `ridu migrate create`. PostgreSQL
migration creation presents compatible remove/add pairs for confirmation and records accepted
continuity in the immutable artifact. SQLite requires a named compiled transform to rewrite stored
canonical JSON. Do not maintain hand-authored public schema IDs or edit generated manifests to
force a rename.

This changes the public field from `title` to `headline`; the migration must preserve the stored
value:

```go title="content/articles.go" remove={2} add={3}
Fields: []field.Definition{
	field.Text("title", field.Required()),
	field.Text("headline", field.Required()),
},
```

Review the resulting storage and API change—especially relationships, indexes, auth identities,
localized fields, and removed capabilities—before applying it. See
[PostgreSQL migrations](/docs/migrations/), [SQLite](/docs/sqlite/), and
[Generated contracts](/docs/generated-contracts/).
