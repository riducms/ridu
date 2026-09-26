---
title: 'Show collection results as cards'
description: 'Replace a collection table with your own layout while keeping search, filters, pagination, and bulk actions.'
product: admin
order: 157
eyebrow: 'Custom components'
aliases:
  ['list results', 'collection cards', 'documentHref', 'custom table']
navigation:
  section: 'Admin & workflows'
  parent: custom-components
  order: 75
  title: 'List layouts'
---

Use `listResultsRenderers` when you want a different layout for a collection's documents, such as
cards or a compact list. Ridu still handles search, filters, pagination, permissions,
and bulk actions. Your component displays the current results.

To replace the toolbar and the rest of the page too, use a
[whole-screen replacement](/docs/custom-components/custom-views/#replace) instead.

## 1. Render the documents {#component}

```svelte title="admin/src/components/post-cards.svelte" focus={11,14-18,20-33}
<script lang="ts">
	import type { AdminListResultsRendererProps } from '@riducms/plugin';
	import { Link } from '@riducms/admin/routing';
	import { Button } from '@riducms/ui';
	import './post-cards.css';

	let { list }: AdminListResultsRendererProps = $props();
</script>

<div class="grid gap-4 py-4 sm:grid-cols-2">
	{#each list.documents as document (document.id)}
		<article class="space-y-3 rounded-lg border p-5">
			<Link
				to={list.documentHref(document)}
				class="block text-lg font-medium hover:underline"
			>
				{list.title(document)}
			</Link>
			<Button
				variant="outline"
				class="post-cards__select"
				aria-label={`Select ${list.title(document)}`}
				aria-pressed={list.selectedIDs.has(document.id)}
				disabled={!list.selectionEnabled}
				onclick={() =>
					list.toggleDocument(
						document.id,
						!list.selectedIDs.has(document.id)
					)}
			>
				{list.selectedIDs.has(document.id) ? 'Selected' : 'Select'}
			</Button>
		</article>
	{/each}
</div>
```

[`AdminListResultsRendererProps`](/reference/plugin/admin-list-results-renderer-props/) supplies the
current `list`. Use it directly—do not fetch the documents again or copy them into local state.

`list.title(document)` uses the collection's configured title field, falling back to its ID.
`list.documentHref(document)` builds the document destination with its encoded ID and content
locale. `Link` adds the configured admin base path and keeps normal admin navigation,
including unsaved-change blocking.

You own the markup. You can make an entire card the link, put an image inside it, or use a
separate button-shaped link. Keep selection controls outside links so they remain separate
keyboard and pointer targets.

The Select button updates Ridu's existing selection. Selected records can then use the
normal bulk actions; `selectionEnabled` disables changes while the list cannot accept them.

### Style the selection control {#styling}

`Button` keeps its Ridu variant classes when you pass `class`. The example imports this
component's stylesheet, which can set the selection button's alignment:

```css title="admin/src/components/post-cards.css"
@import '@riducms/ui/layers.css';

@layer app {
	.post-cards__select {
		justify-content: flex-start;
	}
}
```

The `app` layer follows the framework's `ridu` layer. Use the
[CSS customization guide](/docs/admin/customizing-css/) for theme variables and other control
overrides.

## 2. Choose the collection {#register}

```ts title="admin/src/admin.config.ts" focus={7-9}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import PostCards from './components/post-cards.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	listResultsRenderers: [
		{ key: 'post-cards', collection: 'posts', component: PostCards }
	]
});
```

Add this entry to your existing `admin.config.ts`, keeping its other settings.
With `bun run dev` running, open **Posts**. Existing posts should appear as cards below
the normal search and filter controls. Select one to use the collection's bulk actions,
or click its title to open it.

Create a post first if the collection is empty. Ridu supplies the empty, loading, and error
messages around your results component. Trash continues to use the built-in restore/delete table.

Only one results component can target a collection. Do not register both `listResultsRenderers` and
a `collectionList` whole-screen replacement for the same collection.

To change only one cell in the normal table, use [Table cells](/docs/custom-components/list-cells/).
