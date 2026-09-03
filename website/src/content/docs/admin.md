---
title: 'The Ridu admin'
description: 'Browse, create, edit, localize, publish, and extend content in Ridu’s embedded admin.'
product: admin
eyebrow: 'Admin'
order: 140
navigation:
  section: 'Admin & workflows'
  order: 10
  title: 'Admin'
---

Ridu includes a Svelte 5 admin. Your application provides a Vite entry, theme, and plugin imports.
Production assets are
static files embedded in the Go binary, so the authoring UI does not require Node, Bun, SvelteKit,
or a separate JavaScript server at runtime.

## How the admin is configured {#framework-owned-shell}

At startup the admin reads the canonical manifest, current identity, and access capabilities. That
single contract drives navigation, collection columns, field layout, localization, resource
features, and plugin pairing. The generated Fetch SDK is its transport; REST, local Go calls, and
the admin therefore enter the same validation, access, hooks, versioning, and transaction engine.

The authenticated shell includes:

- dashboard, grouped collection/global navigation, breadcrumbs, command-menu navigation and create
  shortcuts;
- login plus configured recovery and verification screens;
- profile, password, session, and API-key account workflows; and
- responsive navigation, page titles, loading progress, notifications, confirmation dialogs, and
  safe redirects back to the originally requested admin route.

Admin visibility is not authorization. Hidden navigation or a read-only field only describes the
current interface; the server re-evaluates collection, document, and field access on every request.
See [Access control](/docs/access-control/) and [Authentication](/docs/authentication/).

## Find and organize content {#lists}

Collection list routes support title search, pagination, stable sorting, typed filters, selectable
columns, page-size preferences, saved views, workflow status, and locale switching. ID, created,
updated, and status metadata can be selected independently. Nested group paths can be columns,
filters, and sort keys when the schema permits them; relationship and upload cells resolve readable
labels through access-checked requests.

When a collection declares a folder relationship, authors can filter by folder and switch to a
hierarchical view. Trash-enabled collections have a separate trash workspace. List state is kept in
the URL and actor preferences, so filtered and saved workspaces remain navigable rather than living
only in component memory.

Authors can select the current page or resolve all filtered results. Bulk edit, publish, unpublish,
delete, restore, and permanent delete are performed as one access-checked server request. The
atomic maximum is 100 documents; narrow a filter when it resolves to more. Bulk create/import is
not a generic admin workflow today—use an application task or purpose-built importer.

## Create and edit documents {#documents}

The schema-driven document route handles collections and globals. It renders scalar fields,
relationships, uploads, groups, arrays, blocks, rows, tabs, collapsibles, joins, localization, and
statically registered custom fields. It reconciles a manifest change without silently submitting
removed or incompatible values.

The form controller owns nested values, registration, client validation, dirty state, field access,
server issues, conditional presentation, localized inheritance, and submit state. Save, save draft,
publish, unpublish, duplicate, delete/trash, and copy-locale controls appear only when the resource
and evaluated access allow them. Optimistic revisions turn a competing write into a visible
conflict instead of overwriting it.

Relationship and upload fields open an access-aware reference browser with debounced search,
pagination, filters, multi-selection, and inline create/edit where permitted. Upload collections
also provide direct and SSRF-guarded remote ingestion, a retryable bulk-upload queue, media preview,
metadata editing, replacement, and persisted crop/focal-point controls.

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
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Relationship("author", field.To("users")),
		field.Select("status", field.OneOf("draft", "published")),
	},
}
```

## Versions, locks, and preview {#editorial}

Version-enabled resources have history and revision-detail routes. Authors can compare field-level
changes, show only modified values, restore a revision, and publish or unpublish. Collections also
show durable scheduled publications and allow an authorized author to schedule or cancel them.
Global scheduling is not implemented.

Document locks warn when another editor owns the record, make the form read-only, and allow an
authorized takeover. Account login-attempt locks are separate: authorized users can force-unlock an
auth document from its document actions.

Configured live preview opens an isolated iframe panel with named viewport breakpoints. The admin
mints a short-lived resource-bound preview grant and posts subsequent form updates through the
framework preview protocol. Preview grants are process-local today, so multi-replica deployments
need affinity for the preview session. Read [Live preview](/guides/live-preview/) before integrating
the receiving site.

Every create/edit/global route also has a stable API view. Its URL can be linked directly and
survives reload, which is useful when comparing the current authoring state with JSON consumers.

## Field layout {#field-layout}

`Label`, `Description`, `Columns`, `Tab`, `ShowWhen`, `ReadOnly`, rows, tabs, and collapsibles affect
authoring presentation. They do not grant access or rename stored paths.

```go title="admin-layout.go"
package content

import "github.com/riducms/ridu/field"

func profileFields() []field.Definition {
	return []field.Definition{
		field.Row(
			field.Text("firstName", field.Columns(6)),
			field.Text("lastName", field.Columns(6)),
		),
		field.Collapsible("SEO", true,
			field.Text("metaTitle"),
			field.Textarea("metaDescription"),
		),
	}
}
```

Use [Fields](/docs/fields/) for the complete vocabulary and [Rich text](/docs/rich-text/) for the
official paired editor plugin.

## Extend the admin {#extend-the-admin}

Admin plugins are published TypeScript/Svelte packages, imported statically into the generated
registry. Backend partners are compiled Go packages. The generator and startup resolver validate
the stable plugin key, admin API version, plugin-owned pairing version, exported symbol, route list,
and asset list before exposing extensions. There is no runtime package installation or dynamic
code download in production.

Use `defineAdminPlugin` to register only the surface you need:

```ts title="src/admin.ts"
import { ADMIN_PLUGIN_API_VERSION, defineAdminPlugin } from '@riducms/plugin';
import EditorialPanel from './EditorialPanel.svelte';
import ReviewRoute from './ReviewRoute.svelte';

export const admin = defineAdminPlugin({
	apiVersion: ADMIN_PLUGIN_API_VERSION,
	key: 'editorial-tools',
	pairingVersion: 1,
	fields: [],
	dashboard: [{ key: 'queue', component: EditorialPanel, position: 'after' }],
	routes: [
		{
			path: 'editorial/review',
			component: ReviewRoute,
			navigation: { label: 'Review queue', group: 'Editorial' }
		}
	]
});
```

Plugins can provide exact namespaced message catalogs with `defineAdminMessages`. Every extension
receives `i18n`, and route, list-cell, and document-view registrations can use translated
`labelKey` values. Missing locale keys and changed placeholders fail the TypeScript build.

The public extension families are:

| Need                     | Contract                                                                                                                               |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| Custom stored field UI   | `FieldPlugin` receives the exact schema field, narrow `FieldForm`, and optional authoring host for document lookup/reference browsing. |
| Authenticated page       | `routes`; each relative path must match the compiled backend descriptor. Navigation is optional.                                       |
| Dashboard composition    | `dashboard` panels before/after the default, or one collision-checked replacement.                                                     |
| Shell composition        | `login`, `account`, `navigation`, `logoutButton`, `branding`, `shell`, and ordered `providers`.                                        |
| Core route composition   | `views` can wrap/replace collection list/create/edit, global, or not-found surfaces and receive a narrow refresh/notification host.    |
| Resource details         | `listCells`, `documentActions`, and read-only/operational `documentViews`.                                                             |
| Static companion modules | `assets`, validated against the backend descriptor and imported by the generated registry.                                             |

Replacement components receive the framework `defaultView` as a Svelte snippet, so a plugin can
wrap the working screen instead of recreating it. Hosts expose focused operations such as refresh,
login/logout, document-change notification, and toasts; they do not leak the admin’s internal
stores. Extension keys and scoped replacement targets are collision-checked for deterministic
composition.

Start with [Plugins](/docs/plugins/) and [Custom fields](/guides/custom-fields/). Exact contracts are
in the [`@riducms/plugin` reference](/reference/plugin/); reusable primitives are documented in
[`@riducms/ui`](/reference/ui/), and generated alias behavior in
[`@riducms/build`](/reference/build/).

## Current boundaries {#boundaries}

The main author workflows above are implemented, but related list/edit screens do not yet
live-refresh after every mutation.

- broader tablet acceptance and known dirty-state, leave-guard, and accessibility defects remain in
  progress;
- array/block sorting and crop/focal editing work; extend keyboard, touch, and focus coverage when
  those interactions change or a defect exposes a gap;
- route/bootstrap recovery exists, but plugin-isolated recovery and route-level plugin chunking do
  not; and
- large relationship datasets are paginated and searched, but no virtualized picker is promised.

For an evaluation-level view across the whole product, including planned features that are not
callable today, see [Capability status](/docs/status/).
