<script lang="ts">
	import type { RowLabelProps } from "@riducms/plugin/admin";
	let { row, rowNumber, config }: RowLabelProps<{ prefix: string }> = $props();
</script>

<span data-unified-row-key={String(row._key)}>
	{config.prefix}
	{rowNumber}: {String(row.label ?? row.sku ?? row.accent ?? "")}
</span>
