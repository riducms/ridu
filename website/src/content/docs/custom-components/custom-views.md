---
title: 'Customize list and edit views'
description: 'Add content around built-in admin screens or replace a screen with your own.'
product: admin
order: 158
eyebrow: 'Custom components'
aliases:
  ['list view', 'edit view', 'replace editor', 'wrap admin page']
navigation:
  section: 'Admin & workflows'
  parent: custom-components
  order: 80
  title: 'List and edit views'
---

Use a view component when you want to add content around a collection list, a document editor,
or a global's edit screen. Your component receives the built-in screen as `defaultView`.
Rendering it keeps the working list or form on the page.

This example adds an editorial reminder above the Post editor.

## 1. Wrap the built-in screen {#component}

```svelte title="admin/src/components/editorial-screen.svelte" focus={10-11}
<script lang="ts">
	import type { AdminCoreViewProps } from '@riducms/plugin';

	let { defaultView }: AdminCoreViewProps = $props();
</script>

<aside aria-label="Editorial reminder">
	Check image credits before publishing an article.
</aside>
<!-- Keep the built-in editor below the reminder. -->
{@render defaultView()}
```

A Svelte **snippet** is a piece of UI that a component can render. In this example,
`{@render defaultView()}` displays Ridu's existing editor, including its fields and save controls.
Put your content before or after that line to place it around the editor.

## 2. Choose the screen {#register}

```ts title="admin/src/admin.config.ts" focus={9-12}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import EditorialScreen from './components/editorial-screen.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	views: [
		{
			key: 'posts-editor',
			surface: 'collectionEdit',
			collection: 'posts',
			component: EditorialScreen
		}
	]
});
```

Keep `bun run dev` running and open an existing Post. The reminder should appear
above the normal editor, and editing and saving should still work.

## Other screens {#screens}

Set `surface` to the screen you want to customize:

| Screen            | `surface`          | Limit it to…               |
| ----------------- | ------------------ | -------------------------- |
| Collection table  | `collectionList`   | `collection: 'posts'`      |
| Create a document | `collectionCreate` | `collection: 'posts'`      |
| Edit a document   | `collectionEdit`   | `collection: 'posts'`      |
| Edit a global     | `global`           | `global: 'site-settings'`  |
| Page not found    | `notFound`         | The admin's not-found page |

Omit the collection or global name to apply a component to every screen of that type. A
registration for a specific collection or global takes precedence over a general one.
Give each registration a different key. Two replacements for the same target are an error.

## Replace a screen {#replace}

If your component does not render `defaultView`, it replaces the screen completely. You then
provide its UI and data requests yourself. For most additions, render `defaultView` so you can
keep the built-in editing and list features.

These components can receive `collection`, `global`, and `documentID`, depending on the screen.
They do not receive the editor's unsaved form values. Use a
[field component](/docs/custom-components/field-components/) to work with those values, or a
[document tab](/docs/custom-components/document-views/) to display saved document data.
