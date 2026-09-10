---
title: 'Saved views, folders, and hierarchy'
description: 'Persist an author’s list workspace and configure folder filters or parent-child organization.'
product: admin
eyebrow: 'Admin tasks'
order: 122
aliases:
  [
    'saved views',
    'folders',
    'folder filter',
    'hierarchy view',
    'ParentField',
    'FolderField'
  ]
navigation:
  section: 'Admin & workflows'
  parent: admin
  group: 'Find and organize'
  order: 20
  title: 'Saved views & hierarchy'
---

Saved views preserve an author's list workspace. Folder and parent fields add relationships that
the list can use for organization. Neither feature changes access.

## Configuration {#configuration}

| Option or method                | What it controls                                                            |
| ------------------------------- | --------------------------------------------------------------------------- |
| `Collection.Admin.FolderField`  | Names a direct singular relationship whose target documents become folders. |
| `Collection.Admin.ParentField`  | Names a direct singular self-relationship used to build the hierarchy.      |
| SDK `preference(key)`           | Reads the current actor's private saved preference.                         |
| SDK `setPreference(key, value)` | Creates or replaces one saved preference.                                   |
| SDK `deletePreference(key)`     | Removes one saved preference without changing content.                      |
| SDK `resetPreferences()`        | Removes the current actor's saved admin preferences.                        |

Saved views also capture filters, columns, sort, locale, folder, page size, and list/hierarchy mode
from the list UI; they never preserve access the actor later loses.

## Save a list workspace {#saved-views}

Configure filters, columns, sort, locale, folder, and list/hierarchy mode in a collection list, then
choose **Save view**. Give it a descriptive name such as “French posts ready for review.” Applying
the view restores those inputs; deleting it removes only the preference, never documents.

Preferences are owned by the exact auth collection and user ID. They remain private convenience
state and cannot preserve access that the actor later loses. The SDK also exposes `preference`,
`setPreference`, `deletePreference`, and `resetPreferences` for application/admin extensions.

## Add folders and hierarchy {#configure}

```go title="content/pages.go"
var Folders = ridu.Collection{
	Slug:  "folders",
	Admin: ridu.CollectionAdmin{UseAsTitle: "name"},
	Fields: field.Fields{
		field.Text("name").Required(),
	},
}

var Pages = ridu.Collection{
	Slug: "pages",
	Admin: ridu.CollectionAdmin{
		UseAsTitle:  "title",
		FolderField: "folder",
		ParentField: "parent",
	},
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Relationship("folder", "folders"),
		field.Relationship("parent", "pages"),
	},
}
```

`FolderField` must be a direct singular, non-polymorphic relationship. The toolbar then lists
readable folders, filters pages by the chosen ID, and links to the folder collection for management.
`ParentField` must be a direct singular relationship back to the same collection and enables the
hierarchy/list switch.

Save the config with `ridu dev` running. It regenerates contracts, safely synchronizes the additive
development schema, and reloads the admin. Create a few folders and parent/child pages, then verify
list, hierarchy, filter, saved-view, and SDK reads. Before deployment, create, review, and verify the
selected adapter's migration.

## Constraints and troubleshooting {#troubleshooting}

- Folder and parent values are relationship fields. Target existence, read access, delete behavior,
  localization, and migration safety still apply.
- A saved folder ID can become unavailable; the view must not reveal or resurrect it.
- Parent cycles are a content-model concern. Add validation when your application requires a strict
  acyclic tree.
- Trash mode uses its own workspace; deleted parents are not shown as live nodes.

See [Collections](/docs/collections/#admin-metadata) for metadata contracts and
[Relationships](/docs/fields/relationship/) for reference behavior.
