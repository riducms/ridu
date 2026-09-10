<!-- Generated from website/src/content/docs/admin.md by scripts/sync-agent-docs.ts. -->

# The Ridu admin

Ridu includes an admin for browsing, editing, and publishing your content. Define your collections
and fields in Go, then use the admin at `/admin`. You can change its theme and add your own Svelte
components to suit your editors.

The admin is bundled into your Go application when you build it. Production needs no separate
JavaScript server.

## Admin configuration {#configuration}

| Option                                         | What it controls                                                                                   |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `Config.Admin.User`                            | Chooses the auth-enabled collection whose sessions may enter the admin.                            |
| `Config.Admin.Localization`                    | Declares interface languages and editor timezones, separate from content locales.                  |
| `Collection.Admin.UseAsTitle`                  | Chooses the direct field used as a document's readable name.                                       |
| `Collection.Admin.DefaultColumns`              | Sets the initial list columns; authors can personalize their view.                                 |
| `Collection.Admin.Group` / `Description`       | Organizes and explains the collection in navigation and list screens.                              |
| `Collection.Admin.FolderField` / `ParentField` | Enables folder filtering or hierarchy for compatible direct relationships.                         |
| `Collection.Admin.LivePreview`                 | Adds a preview URL template and optional named viewport sizes.                                     |
| Field `.Admin(field.Admin{...})`               | Sets label-adjacent description, layout, visibility, editor, and row presentation.                 |
| `admin/src/admin.config.ts`                    | Registers application Svelte components, providers, routes, messages, and installed admin plugins. |

## How the admin is configured {#framework-owned-shell}

Your Go config determines which collections appear, how fields are laid out, and what each user
can do. The admin reads that configuration and displays the available actions for the signed-in
user. The API checks permissions and validates data again when a document is saved.

After signing in, the dashboard and navigation give you access to your collections and globals.
Use the command menu to find a screen or start a new document. Account screens let you manage
your profile, password, sessions, and API keys.

Hiding a collection or disabling an input does not secure its data. Set the Go access rules to
control who can read or change it. See [Access control](./access-control.md) and
[Authentication](https://riducms.com/docs/authentication/).

## Find and organize content {#lists}

Open a collection to search its titles, filter results, sort columns, and choose which columns
to show. You can include fields inside groups and built-in values such as ID, status, and creation
date. Relationships and uploads display readable names when the user has permission to see them.
Save a view to return to the same filters and columns later.

Collections with folders can also be browsed by folder. Collections with trash enabled have a
separate screen for restoring or permanently deleting documents. Filters are kept in the URL,
so you can bookmark or share a filtered list.

Select documents to edit, publish, unpublish, delete, or restore them together. Each bulk action
supports up to 100 documents and checks permission for each one. Narrow the filter if it selects
more. General content import is not built into the admin; use an application task or custom importer.

## Create and edit documents {#documents}

Open a collection document or global to edit its fields. The form follows your Go field definitions,
including groups, arrays, blocks, tabs, and custom fields. If you change those definitions while
editing, the form detects removed or incompatible fields before saving.

The editor keeps track of unsaved changes and displays validation errors beside the affected
fields. Save, publish, duplicate, delete, and copy-locale controls appear when they are enabled
and you have permission to use them. If another user saves a competing change, Ridu displays a
conflict instead of silently overwriting it.

Relationship and upload fields let you search for an existing document, select one or more results,
and create or edit a related document when permitted. Upload collections let you upload files or
import them from a URL, preview media, edit metadata, replace a file, and adjust its crop and focal
point. Failed uploads can be retried.

This collection configuration turns on drafts and versions, chooses the default columns, and adds
a live preview URL:

```go title="content/posts.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Posts = ridu.Collection{
	Slug:     "posts",
	Versions: true,
	VersionConfig: ridu.VersionConfig{
		Drafts: true,
	},
	Admin: ridu.CollectionAdmin{
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "author", "status"},
		Group:          "Editorial",
		Description:    "Stories published across Acme properties",
		LivePreview: ridu.LivePreviewConfig{
			URL: "https://www.example.com/preview/posts/{id}",
			Breakpoints: []ridu.PreviewBreakpoint{
				{Name: "mobile", Label: "Mobile", Width: 390, Height: 844},
				{Name: "desktop", Label: "Desktop", Width: 1440, Height: 900},
			},
		},
	},
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Relationship("author", "users"),
		field.Select("status", "draft", "published"),
	},
}
```

## Versions, locks, and preview {#editorial}

Enable versions to browse a document's history, compare changes, and restore an earlier revision.
Collections also support scheduled publication: an authorized editor can choose a publication time
or cancel it later. Globals cannot be scheduled yet.

Document locks show when someone else is editing. The form becomes read-only until the lock is
released or an authorized editor takes over. This is separate from an account lock caused by failed
sign-in attempts; authorized users can unlock those accounts from their document actions.

Live preview shows your frontend beside the form and sends it unsaved changes. Configure sizes
such as mobile and desktop to check both layouts. Read [Live preview](./live-preview.md) to
connect your frontend and configure deployments with multiple server instances.

Use the API tab to inspect a document's JSON. Its URL can be bookmarked or shared and works after
a page reload.

## Field layout {#field-layout}

Use `.Label(...)` to name a field and `.Admin(field.Admin{...})` to set its description, width,
or other display options. Rows, tabs, and collapsible sections organize the form without changing
where its values are stored.

This example places first and last name side by side and groups SEO fields in a collapsed section:

```go title="admin-layout.go"
package content

import "github.com/riducms/ridu/field"

func profileFields() field.Fields {
	return field.Fields{
		field.Row(field.Fields{
			field.Text("firstName").Admin(field.Admin{Columns: 6}),
			field.Text("lastName").Admin(field.Admin{Columns: 6}),
		}),
		field.Collapsible("SEO", field.Fields{
			field.Text("metaTitle"),
			field.Textarea("metaDescription"),
		}).Admin(field.Admin{InitiallyCollapsed: true}),
	}
}
```

See [Fields](./fields.md) for the available field types and layout options, or
[Rich text](./rich-text.md) to add a formatted-text editor.

## Custom components {#extend-the-admin}

Add your own Svelte components to make the admin fit your editors' work. Register them in
`admin/src/admin.config.ts`, alongside the plugins your application already uses.

Start with [Custom components](./custom-components.md) for a complete first example. Then
choose the part of the admin you want to change:

- [Field components](./custom-components/field-components.md) — replace an input or add a
  character counter.
- [Row labels](./custom-components/row-labels.md) and
  [table cells](./custom-components/list-cells.md) — make lists easier to scan.
- [Dashboard](./custom-components/dashboard.md) and
  [custom pages](./custom-components/custom-pages.md) — add instructions, reports, or tools.
- [Document tabs](./custom-components/document-views.md),
  [document buttons](./custom-components/document-actions.md), and
  [list and edit views](./custom-components/custom-views.md) — customize document workflows.
- [Branding and navigation](./custom-components/branding-and-navigation.md) — change the logo,
  login screen, and navigation.
- [Shared settings](./custom-components/providers.md) — share settings between your components.

You can use these components directly from your application. Build a
[plugin](./plugins.md) when your customization also adds Go behavior or a new field type.
See [Admin localization](./localization.md#admin-language) for translated component labels and messages.

## Current limitations {#boundaries}

Related list and edit screens do not yet refresh automatically after every change. Reload the
screen if it still shows an older value.

Tablet interactions, keyboard and screen-reader support, and warnings about leaving unsaved changes
are still being improved. Check the editing tasks your team needs on its intended devices.
See [Capability status](./capabilities.md) for feature availability across Ridu.
