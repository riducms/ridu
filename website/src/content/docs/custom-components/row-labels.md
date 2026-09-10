---
title: 'Custom row labels'
description: 'Show useful summaries in the headings of array and block rows.'
product: admin
order: 152
eyebrow: 'Custom components'
aliases:
  [
    'array row label',
    'block row label',
    'custom row heading',
    'field.Admin.RowLabel'
  ]
navigation:
  section: 'Admin & workflows'
  parent: custom-components
  order: 20
  title: 'Row labels'
---

Array and block rows can show a short summary while collapsed. If a row already has a `label`
field, set `RowLabelPath: "label"` in its `field.Admin` settings to use that text as the heading.
Use a component when you want to combine values, such as a link's label and URL.

This example turns a row heading into `About — /about` and updates it as the user edits the row.

## Configuration {#configuration}

| Option or prop                            | Required                          | What it does                                                                                  |
| ----------------------------------------- | --------------------------------- | --------------------------------------------------------------------------------------------- |
| `defineRowLabel({ component })`           | Yes                               | Registers the Svelte row-heading component.                                                   |
| `decodeConfig(value)`                     | When Go supplies component config | Validates unknown JSON synchronously and returns typed component settings.                    |
| `rowLabels['app:name']`                   | Yes                               | Gives the local registration the same key selected in Go.                                     |
| `field.Admin.RowLabel`                    | Yes for a custom heading          | Selects `field.Component("app:name")`, with optional JSON-safe settings.                      |
| `field.Admin.RowLabelPath`                | Recommended                       | Supplies plain text for row actions and screen readers.                                       |
| Component `row`, `rowNumber`, `blockType` | Supplied by Ridu                  | Exposes current unsaved row values, a one-based position, and the block slug when applicable. |

## 1. Create the label component {#component}

```svelte title="admin/src/components/link-row-label.svelte" focus={5-11}
<script lang="ts">
	import type { RowLabelProps } from '@riducms/plugin/admin';

	let { row, rowNumber }: RowLabelProps = $props();
	// Recalculate the label as the row is edited or reordered.
	const label = $derived(
		typeof row.label === 'string' && row.label
			? row.label
			: `Link ${rowNumber}`
	);
	const url = $derived(typeof row.url === 'string' ? row.url : '');
</script>

<span>
	{label}
	{#if url}
		— {url}
	{/if}
</span>
```

`row` contains the current row's values, including unsaved changes. `rowNumber` starts at one
and changes when the row moves. The example uses it when the label is empty.

A row label displays information. Put inputs and buttons for editing values inside the row's
fields, rather than changing data from the label.

## 2. Register the label {#register}

Add the import and `rowLabels` entry to `admin/src/admin.config.ts`:

```ts title="admin/src/admin.config.ts" focus={8-10}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import { defineRowLabel } from '@riducms/plugin/admin';
import LinkRowLabel from './components/link-row-label.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	rowLabels: {
		'app:linkSummary': defineRowLabel({ component: LinkRowLabel })
	}
});
```

## 3. Use it on an array {#select}

Use the same `app:linkSummary` name on the array:

```go title="content/pages.go" focus={15-19}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Pages = ridu.Collection{
	Slug: "pages",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Array("links", field.Fields{
			field.Text("label"),
			field.Text("url"),
		}).Admin(field.Admin{
			// Use the label text for screen readers and row actions.
			RowLabelPath: "label",
			RowLabel:     field.Component("app:linkSummary"),
		}),
	},
}
```

Keep `RowLabelPath: "label"` as well. Ridu uses that plain-text name for row actions and screen
readers, while your component provides the visible heading. If the array already has admin
settings, add these two properties to them: `.Admin(...)` replaces the whole group of settings.

Add `content.Pages` to your Go config if it is a new collection. Keep `bun run dev` running;
it regenerates the schema when you save the Go changes and reloads your admin components.
Open a Page, add a Links row, and enter its label and URL.
Collapse the row and confirm that the heading shows both values.

## Use labels on blocks {#blocks}

`field.Admin.RowLabel` also works on `field.Blocks`. The component receives each block's values
in `row`, including `row.blockType`, so it can display a different summary for each block type.

Application row labels work in collections and globals, including nested groups, arrays, and
blocks. You can also use them for arrays and blocks inside a rich-text block's form.

See [RowLabelProps](/reference/plugin/row-label-props/) for the available component props.
