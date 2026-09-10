<!-- Generated from website/src/content/docs/custom-components/list-cells.md by scripts/sync-agent-docs.ts. -->

# Custom table cells

A table cell component changes how a field looks in the collection list. The field's value and
its document editor stay the same. This is useful for badges, thumbnails, links, or formatted
numbers.

This example displays a `readingMinutes` number as `5 min read` in the Posts table. Add
`field.Number("readingMinutes")` to Posts if you do not already have it.

## Configuration {#configuration}

| Option                   | Required | What it does                                                                 |
| ------------------------ | -------- | ---------------------------------------------------------------------------- |
| `listCells[].key`        | Yes      | Gives the registration a unique name.                                        |
| `listCells[].collection` | Yes      | Selects one collection slug.                                                 |
| `listCells[].field`      | Yes      | Selects one top-level field name/path in that collection.                    |
| `listCells[].label`      | Yes      | Provides the fallback column heading.                                        |
| `listCells[].labelKey`   | No       | Uses a registered translated message for the heading.                        |
| `listCells[].component`  | Yes      | Displays the saved `value` and document; it is not an editable form binding. |

## 1. Create the cell {#component}

```svelte title="admin/src/components/reading-time-cell.svelte"
<script lang="ts">
	import type { AdminListCellProps } from '@riducms/plugin';

	let { value }: AdminListCellProps = $props();
</script>

<span>
	{typeof value === 'number' ? `${value} min read` : 'Not set'}
</span>
```

Ridu passes this cell's `value` to the component. The example checks that it is a number before
formatting it and shows `Not set` for an empty value. You can also receive `document` to read
other saved values from the same row.

## 2. Register it for a column {#register}

```ts title="admin/src/admin.config.ts" focus={10-13}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import ReadingTimeCell from './components/reading-time-cell.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	listCells: [
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
`Admin.DefaultColumns`. See [Browse content](../browsing-content.md) for list configuration.

Table cells receive saved data. To customize the input used while editing a document, use a
[field component](./field-components.md). To add content above or below the
whole table, use [List and edit views](./custom-views.md).
