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
