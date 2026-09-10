<!-- Generated from website/src/content/docs/collections.md by scripts/sync-agent-docs.ts. -->

# Collections and globals

Collections hold many documents, such as posts, products, people, or media. Globals hold one
document, such as site settings or the main navigation. Both support fields, permissions,
validation, hooks, and translations.

Define them with `ridu.Collection` and `ridu.Global`. Ridu generates the matching API, Go and
TypeScript types, and admin screens.

## Define a collection {#define-a-collection}

A collection slug, such as `articles`, identifies it in the API, relationships, and admin URLs.
Use `Fields` to list the fields each article should have.

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
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Textarea("summary"),
		field.Relationship("category", "categories"),
		field.Group("seo", field.Fields{
			field.Text("slug").Required(),
		}),
	},
	Indexes: []ridu.CollectionIndex{
		{Fields: []string{"category", "seo.slug"}, Unique: true},
	},
}
```

Register the value in `ridu.Config.Collections`. Config resolution rejects unknown
relationship targets, invalid admin fields, duplicate slugs, and unsupported index paths.

## Collection settings {#collection-config}

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
| `Access`                              | Authorize resource operations.                                                                   |
| `Hooks`                               | Run deterministic lifecycle callbacks, including transaction-reusing nested local calls.         |

See [Fields](./fields.md) for examples. A field name becomes a stored property and appears in
generated types; its label and description only change what authors see.

## Customize the collection in the admin {#admin-metadata}

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
`{field:path.to.value}` placeholders. Read [Admin](./admin.md) and
[Editorial workflows](https://riducms.com/docs/editorial-workflows/) for the authoring experience.

> [!IMPORTANT]
> Admin visibility and capability responses are interface hints, never authorization. Every API
> request independently evaluates collection and field access inside its store transaction.

## Index several fields together {#indexes}

Use `CollectionIndex` to index a combination of fields. For example, an external ID can be
unique within each tenant while still being reused by a different tenant:

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

## Add accounts, uploads, drafts, or trash {#capabilities}

A collection supports create, duplicate, find, list, update, delete, filtering, pagination,
selection, population, access evaluation, hooks, and generated contracts. Flags add features:

| Configuration         | What it adds                                                                                             |
| --------------------- | -------------------------------------------------------------------------------------------------------- |
| `Auth: true`          | User accounts, passwords, sessions, account recovery, and optional API keys                              |
| `Upload: true`        | File records, image processing, and file delivery; configure `Config.Storage` and `StorageNamespace` too |
| `Versions: true`      | Saved history, restore, optional drafts, and scheduled publishing                                        |
| `Trash: true`         | Recoverable deletion, restore, and permanent deletion                                                    |
| `LockDocuments: true` | Editing locks and authorized takeover                                                                    |

The official database adapters support these features; MongoDB requires its
[documented production setup](./mongodb.md). If you write your own adapter, it must implement
support for each feature you enable. Ridu checks that when the application starts.
Continue with [Authentication](https://riducms.com/docs/authentication/),
[Uploads](./uploads.md), [Drafts and versions](./drafts-and-versions.md), and
[Editorial workflows](https://riducms.com/docs/editorial-workflows/) for each feature.

## Set permissions and run hooks {#runtime-behaviour}

`CollectionAccess` has independent `Admin`, `Create`, `Read`, `ReadVersions`, `Update`, `Publish`,
`Unpublish`, `Delete`, and `Unlock` rules. A nil `ReadVersions` rule falls back to `Read`; nil
`Publish`, `Unpublish`, and `Unlock` rules fall back to `Update`. Read and mutation rules can return
`ridu.Where(...)`, which the store must combine atomically with the caller's filter.

Attach field access and hooks directly with `.Access(field.Access{...})` and
`.Hooks(field.Hooks[T]{...})`. Reusing the field also reuses its rules and hooks.
Root `field.Virtual` values own their resolver and can be limited with output selection. Learn the order and transaction boundaries in
[Access control](./access-control.md) and [Hooks](./hooks.md).

## Globals {#globals}

Use a global for content that has one instance, such as your site settings. Before it is first
saved, reading it returns its fields with their defaults. Its first update saves the document
using the global slug as its ID.

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
	Fields: field.Fields{
		field.Text("siteName").Required(),
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

```go title="content/articles.go"
Fields: field.Fields{
	field.Text("title").Required(),
	field.Text("headline").Required(),
},
```

Review the resulting storage and API change—especially relationships, indexes, auth identities,
localized fields, and removed capabilities—before applying it. See
[PostgreSQL migrations](./migrations.md), [SQLite](./sqlite.md), and
[Generated contracts](./generated-contracts.md).
