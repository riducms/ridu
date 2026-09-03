<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";

	let { field }: { field: SchemaField } = $props();
</script>

<aside class="border-s-2 border-control-border py-0.5 ps-3" data-ui-field={field.name}>
	<p class="text-[13px] font-medium text-foreground">{field.admin.label}</p>
	{#if field.admin.description}<p class="mt-1 text-[12.5px] leading-5 text-foreground-muted">
			{field.admin.description}
		</p>{/if}
</aside>
