<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";
	import "@admin/fields/field-layout.scss";

	let { field }: { field: SchemaField } = $props();
</script>

<aside class="ridu-ui-field" data-ui-field={field.name}>
	<p class="ridu-ui-field__title">{field.admin.label}</p>
	{#if field.admin.description}
		<p class="ridu-ui-field__description">
			{field.admin.description}
		</p>
	{/if}
</aside>
