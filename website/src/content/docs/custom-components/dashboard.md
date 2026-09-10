---
title: 'Customize the dashboard'
description: 'Add a dashboard panel or replace the default dashboard.'
product: admin
order: 154
eyebrow: 'Custom components'
aliases: ['dashboard panel', 'custom dashboard', 'welcome message']
navigation:
  section: 'Admin & workflows'
  parent: custom-components
  order: 40
  title: 'Dashboard'
---

Add panels above or below Ridu's dashboard to show instructions, shortcuts, or information from
your application. A panel is an ordinary Svelte component registered in `admin.config.ts`.

The [overview](/docs/custom-components/#first-component) shows a welcome panel. This
example uses the props Ridu supplies to show the collections available to the signed-in user.

## Configuration {#configuration}

| Option                          | Required                | What it does                                                                                          |
| ------------------------------- | ----------------------- | ----------------------------------------------------------------------------------------------------- |
| `dashboard[].key`               | Yes                     | Gives the panel a unique registration name.                                                           |
| `dashboard[].component`         | Yes                     | Supplies the Svelte panel component.                                                                  |
| `dashboard[].position`          | No; defaults to `after` | Places the panel `before` or `after` Ridu's overview, or uses `replace` for one complete replacement. |
| Component `manifest` prop       | Supplied by Ridu        | Contains collections and globals visible in this admin session.                                       |
| Component `user` / `i18n` props | Supplied by Ridu        | Exposes the signed-in document when available and current interface formatting.                       |

## 1. Create a panel {#component}

```svelte title="admin/src/components/collection-overview.svelte" focus={10-13}
<script lang="ts">
	import type { AdminDashboardPanelProps } from '@riducms/plugin';

	// The manifest lists configured collections, not their documents.
	let { manifest }: AdminDashboardPanelProps = $props();
</script>

<section aria-labelledby="collection-overview-heading">
	<h2 id="collection-overview-heading">Your collections</h2>
	<ul>
		{#each manifest.collections as collection (collection.slug)}
			<li>{collection.labels.plural}</li>
		{/each}
	</ul>
</section>
```

`manifest.collections` describes the collections available in the current admin session. Here,
each collection's plural label becomes a list item. The panel updates if those props change.
Dashboard panels can also receive `user` and `i18n` for the signed-in user and interface translations.

## 2. Place it on the dashboard {#register}

```ts title="admin/src/admin.config.ts" focus={7-13}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import CollectionOverview from './components/collection-overview.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	dashboard: [
		{
			key: 'collections',
			component: CollectionOverview,
			position: 'before'
		}
	]
});
```

Add this entry alongside any panels you already registered. Keep `bun run dev` running
and open `/admin` after signing in. Your panel should appear above the default
dashboard.

Use `position: 'after'` to put a panel below the default dashboard. Multiple panels can appear
before and after it; each needs its own key, and panels in the same position follow registration
order. An omitted position means `after`.

## Replace the dashboard {#replace}

Use `position: 'replace'` when your component should be the dashboard. The built-in dashboard
will no longer appear. Only one replacement is allowed, including replacements from installed
plugins. A replacement takes over the whole dashboard, including the before and after panels. Include
everything you want displayed inside your replacement component.

## Load application data {#data}

Use your [generated SDK](/docs/typescript-sdk/) to fetch counts, reports, or recent documents.
Show loading, empty, and error states in your component. The API applies the signed-in user's
permissions to those requests.

The collection descriptions supplied in `manifest` are configuration, not a list of stored
documents. A panel that shows article totals needs to request those totals from the API.
