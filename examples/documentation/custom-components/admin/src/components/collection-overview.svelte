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
