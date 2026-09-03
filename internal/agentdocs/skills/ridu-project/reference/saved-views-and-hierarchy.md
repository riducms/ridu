<!-- Generated from website/src/content/docs/saved-views-and-hierarchy.md by scripts/sync-agent-docs.ts. -->

# Saved views, folders, and hierarchy

Saved views preserve an author's list workspace. Folder and parent fields add relationships that
the list can use for organization. Neither feature changes access.

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
	Slug: "folders",
	Admin: ridu.CollectionAdmin{UseAsTitle: "name"},
	Fields: []field.Definition{
		field.Text("name", field.Required()),
	},
}

var Pages = ridu.Collection{
	Slug: "pages",
	Admin: ridu.CollectionAdmin{
		UseAsTitle: "title",
		FolderField: "folder",
		ParentField: "parent",
	},
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Relationship("folder", field.To("folders")),
		field.Relationship("parent", field.To("pages")),
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

See [Collections](./collections.md#admin-metadata) for metadata contracts and
[Relationships](https://riducms.com/docs/fields/relationship/) for reference behavior.
