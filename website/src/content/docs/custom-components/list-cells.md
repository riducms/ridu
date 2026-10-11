---
title: 'Custom table cells'
description: 'Change how an existing field appears in a collection list.'
product: admin
order: 153
eyebrow: 'Custom components'
aliases: ['table cells', 'custom cell', 'list column', 'reading time']
navigation:
  section: 'Admin & workflows'
  parent: custom-components
  order: 30
  title: 'Table cells'
---

A table cell component changes how a field looks in the collection list. The field's value and
its document editor stay the same. This is useful for badges, thumbnails, links, or formatted
numbers.

This example displays a `readingMinutes` number as `5 min read` in the Posts table. Add
`field.Number("readingMinutes")` to Posts if you do not already have it.

## Configuration {#configuration}

| Option                           | Required | What it does                                                                 |
| -------------------------------- | -------- | ---------------------------------------------------------------------------- |
| `listCellRenderers[].key`        | Yes      | Gives the registration a unique name.                                        |
| `listCellRenderers[].collection` | Yes      | Selects one collection slug.                                                 |
| `listCellRenderers[].field`      | Yes      | Selects one top-level field name/path in that collection.                    |
| `listCellRenderers[].label`      | Yes      | Provides the fallback column heading.                                        |
| `listCellRenderers[].labelKey`   | No       | Uses a registered translated message for the heading.                        |
| `listCellRenderers[].fields`     | No       | Names other top-level fields the cell reads from `document`.                 |
| `listCellRenderers[].component`  | Yes      | Displays the saved `value` and document; it is not an editable form binding. |

## 1. Create the cell {#component}

```svelte title="admin/src/components/reading-time-cell.svelte"
<script lang="ts">
	import type { AdminListCellRendererProps } from '@riducms/plugin';

	let { value }: AdminListCellRendererProps = $props();
</script>

<span>
	{typeof value === 'number' ? `${value} min read` : 'Not set'}
</span>
```

Ridu passes this cell's `value` to the component. The example checks that it is a number before
formatting it and shows `Not set` for an empty value.

The component also receives `document`, which holds what the list shows: the row's ID,
timestamps, title field and visible columns. The list doesn't return other fields. To use another
value from the row, name its field in the registration's `fields`, for example
`fields: ['status']`. Ridu returns those fields whenever the cell's column is visible. A
relationship or upload field named there arrives as the related document's ID and title, like a
relationship column; the cell's own `value` is always the saved value.

## 2. Register it for a column {#register}

```ts title="admin/src/admin.config.ts" focus={10-13}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import ReadingTimeCell from './components/reading-time-cell.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	listCellRenderers: [
		{
			key: 'reading-time',
			collection: 'posts',
			field: 'readingMinutes',
			label: 'Reading time',
			component: ReadingTimeCell
		}
	]
});
```

`collection` is the collection's slug. `field` is the existing field's name or supported field
path. `label` is the column heading displayed in the table. This registration does not add a
new stored field.

Merge the entry into your existing admin config and keep `bun run dev` running. It reloads
your component and registration, and regenerates the schema if you added the Go field.

## 3. Show the column {#show-column}

Open Posts and use the column picker to show Reading time. An article saved with
`readingMinutes: 5` should display `5 min read`. Edit the article and confirm that its number
field still works normally.

To show the column by default, include `readingMinutes` in the collection's
`Admin.DefaultColumns`. See [Browse content](/docs/browsing-content/) for list configuration.

Table cells receive saved data. To customize the input used while editing a document, use a
[field component](/docs/custom-components/field-components/). To add content above or below the
whole table, use [List and edit views](/docs/custom-components/custom-views/).
