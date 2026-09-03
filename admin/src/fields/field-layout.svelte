<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";

	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldRenderer from "@admin/fields/field-renderer.svelte";
	import { createFieldLayout } from "@admin/fields/field-layout";
	import FieldTabLayout from "@admin/fields/field-tab-layout.svelte";

	let {
		fields,
		form,
		suppressedTabGroupID = "",
	}: {
		fields: readonly SchemaField[];
		form: FormController;
		suppressedTabGroupID?: string;
	} = $props();
	const runtime = getAdminRuntime();
</script>

{#snippet fieldGrid(tabFields: readonly SchemaField[])}
	<div class="grid grid-cols-1 gap-x-4 gap-y-6 sm:grid-cols-12">
		{#each createFieldLayout(tabFields, suppressedTabGroupID) as group (group.key)}
			{#if group.tabGroup !== undefined}
				<FieldTabLayout fields={group.fields} {form} tabGroup={group.tabGroup} />
			{:else if group.collapsible !== undefined}
				<details
					class="col-span-1 rounded-[4px] border border-control-border bg-background sm:col-span-12"
					open={!group.collapsible.initiallyCollapsed}
					data-field-collapsible={group.collapsible.id}
				>
					<summary class="cursor-pointer px-5 py-4 text-[13.5px] font-medium text-foreground">
						{runtime.i18n.text(group.collapsible.label, group.collapsible.labelTranslations)}
					</summary>
					<div
						class="grid grid-cols-1 gap-x-4 gap-y-6 border-t border-control-border p-5 sm:grid-cols-12"
					>
						{#each group.fields as field (field.id)}
							<FieldRenderer {field} {form} />
						{/each}
					</div>
				</details>
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
