---
title: 'Custom document buttons'
description: 'Add a working button beside the standard document actions.'
product: admin
order: 157
eyebrow: 'Custom components'
aliases: ['document button', 'document action', 'copy document ID']
navigation:
  section: 'Admin & workflows'
  parent: custom-components
  order: 70
  title: 'Document buttons'
---

Add a document button for a task such as copying an ID, opening a related service, or starting
an application-specific operation. Your component implements what the button does.

This example copies the current Post's ID to the clipboard and confirms the result.

## Configuration {#configuration}

| Option or prop                        | Required         | What it does                                                                                                            |
| ------------------------------------- | ---------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `documentActions[].key`               | Yes              | Gives the action a unique registration name.                                                                            |
| `documentActions[].component`         | Yes              | Renders the button or control and implements the action.                                                                |
| `documentActions[].collection`        | No               | Limits the action to one non-global collection; omission shows it for every collection.                                 |
| `documentActions[].requires`          | No               | Hides the action unless the document exposes that operation capability. Server authorization still decides the request. |
| Component `document` / `collection`   | Supplied by Ridu | Provides the current saved document and schema.                                                                         |
| `host.refresh()` / `host.notify(...)` | Supplied by Ridu | Reloads saved data after a successful operation or shows a temporary message.                                           |

## 1. Create the button {#component}

```svelte title="admin/src/components/copy-document-id.svelte" focus={7-18}
<script lang="ts">
	import type { AdminDocumentExtensionProps } from '@riducms/plugin';
	import { Button } from '@riducms/ui';

	let { document, host }: AdminDocumentExtensionProps = $props();

	async function copyID() {
		try {
			// Request clipboard access after the user clicks the button.
			await navigator.clipboard.writeText(document.id);
			host.notify('success', 'Document ID copied');
		} catch {
			host.notify(
				'error',
				'Could not copy the ID',
				"Check your browser's clipboard permission."
			);
		}
	}
</script>

<Button type="button" onclick={copyID}>Copy document ID</Button>
```

`document.id` is the saved document's ID. `host.notify` shows the success or error message.
Clipboard access works on HTTPS sites and localhost; the browser may ask for permission.
The code only requests access after a click.

## 2. Add it to Posts {#register}

```ts title="admin/src/admin.config.ts" focus={7-9}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import CopyDocumentID from './components/copy-document-id.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	documentActions: [
		{ key: 'copy-id', collection: 'posts', component: CopyDocumentID }
	]
});
```

Merge this entry into your existing admin config and keep `bun run dev` running.
Open a saved Post and click Copy document ID. Paste into a text editor and confirm it matches
the ID in the document's URL.

Omit `collection` to show the button on saved documents in all collections. Document actions
are not available for globals or for documents that have not been created yet.

## Buttons that change data {#changes}

Use the [generated SDK](/docs/typescript-sdk/) or your application's API to perform the change.
Handle errors in the component, then call `host.refresh()` after success to reload the saved
document. Refreshing does not save changes from the Edit form.

For an action that requires update permission, add `requires: 'update'` to its registration.
Ridu then hides the button when that permission is absent. The server operation must still check
permission; hiding a button does not protect an API endpoint.

Document buttons receive saved values. If a button must work with what someone is currently
typing, put it inside a [field component](/docs/custom-components/field-components/).
