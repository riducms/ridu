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
