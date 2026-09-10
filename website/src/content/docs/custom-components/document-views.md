---
title: 'Custom document tabs'
description: 'Add a tab beside Edit and API to display saved document information.'
product: admin
order: 156
eyebrow: 'Custom components'
aliases: ['document tab', 'document view', 'edit tabs', 'summary tab']
navigation:
  section: 'Admin & workflows'
  parent: custom-components
  order: 60
  title: 'Document tabs'
---

Add a tab to a document when editors need another way to inspect it, such as a summary, report,
or application-specific history. Ridu keeps the existing Edit and API tabs.

This example adds a Summary tab to Posts. It uses a saved article's `title` and `readingMinutes`
fields; add `field.Number("readingMinutes")` if your collection does not have it yet.

## Configuration {#configuration}

| Option                       | Required | What it does                                                                         |
| ---------------------------- | -------- | ------------------------------------------------------------------------------------ |
| `documentViews[].key`        | Yes      | Gives the tab a unique key; `edit` and `api` are reserved.                           |
| `documentViews[].label`      | Yes      | Sets the fallback tab text.                                                          |
| `documentViews[].labelKey`   | No       | Uses a registered translated message for the tab.                                    |
| `documentViews[].collection` | No       | Limits the tab to one collection or global slug; omission shows it on all documents. |
| `documentViews[].component`  | Yes      | Renders the saved document and the supplied refresh/notification host.               |

The component receives saved data. It does not see unsaved values from the Edit tab.

## 1. Create the tab content {#component}

```svelte title="admin/src/components/document-summary.svelte"
<script lang="ts">
	import type { AdminDocumentExtensionProps } from '@riducms/plugin';

	// This is the saved document; unsaved form edits are not included.
	let { document }: AdminDocumentExtensionProps = $props();
</script>

<section aria-labelledby="document-summary-heading">
	<h2 id="document-summary-heading">Article summary</h2>
	<dl>
		<dt>Title</dt>
		<dd>
			{typeof document.title === 'string'
				? document.title
				: 'Untitled'}
		</dd>
		<dt>Reading time</dt>
		<dd>
			{typeof document.readingMinutes === 'number'
				? `${document.readingMinutes} minutes`
				: 'Not set'}
		</dd>
	</dl>
</section>
```

`document` is the saved document returned by the API. The component checks optional values before
displaying them. It does not receive the unsaved values from the Edit form.

## 2. Register the tab {#register}

```ts title="admin/src/admin.config.ts" focus={7-14}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import DocumentSummary from './components/document-summary.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	documentViews: [
		{
			key: 'summary',
			label: 'Summary',
			collection: 'posts',
			component: DocumentSummary
		}
	]
});
```

`key` identifies the tab, and `label` is its visible title. `edit` and `api` are reserved keys.
`collection: 'posts'` limits it to Posts. This option can also name a global; omit it to show the
tab for every collection and global.

Merge the entry into your existing admin config and keep `bun run dev` running.
Open a saved Post and choose Summary beside Edit and API. After changing the reading time on
Edit and saving, return to Summary to see the saved value.

## Updating data from a tab {#updates}

Use the [generated SDK](/docs/typescript-sdk/) if your tab performs an application operation.
After that request succeeds, `host.refresh()` reloads the current document, and
`host.notify('success', 'Updated')` can confirm the result. Neither function saves the Edit
form's unsaved changes.

To change the Edit screen itself, see [List and edit views](/docs/custom-components/custom-views/).
To add a button alongside document actions, see [Document buttons](/docs/custom-components/document-actions/).
