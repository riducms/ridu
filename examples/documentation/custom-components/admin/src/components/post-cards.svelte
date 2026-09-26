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
