<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";

	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import FieldRenderer from "@admin/fields/field-renderer.svelte";
	import { createFieldLayout } from "@admin/fields/field-layout";
	import FieldTabLayout from "@admin/fields/field-tab-layout.svelte";
	import FieldCollapsible from "@admin/fields/field-collapsible.svelte";

	let {
		fields,
		form,
		suppressedTabGroupID = "",
	}: {
		fields: readonly SchemaField[];
		form: FormController;
		suppressedTabGroupID?: string;
	} = $props();
</script>

{#snippet fieldGrid(tabFields: readonly SchemaField[])}
	<div class="grid grid-cols-1 gap-x-4 gap-y-6 sm:grid-cols-12">
		{#each createFieldLayout(tabFields, suppressedTabGroupID) as group (group.key)}
			{#if group.tabGroup !== undefined}
				<FieldTabLayout fields={group.fields} {form} tabGroup={group.tabGroup} />
			{:else if group.collapsible !== undefined}
				<FieldCollapsible fields={group.fields} {form} collapsible={group.collapsible} />
			{:else if group.row !== undefined}
				<div
					class="col-span-1 grid grid-cols-1 gap-x-4 gap-y-6 sm:col-span-12 sm:grid-cols-12"
					data-field-row={group.row.id}
				>
					{#each group.fields as field (field.id)}
						<FieldRenderer {field} {form} />
					{/each}
				</div>
			{:else}
				<FieldRenderer field={group.fields[0]} {form} />
			{/if}
		{/each}
	</div>
{/snippet}

{@render fieldGrid(fields)}
