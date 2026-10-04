<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";

	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import FieldRenderer from "@admin/fields/field-renderer.svelte";
	import { createFieldLayout } from "@admin/fields/field-layout";
	import {
		getFieldLayoutPresentation,
		setFieldLayoutPresentation,
	} from "@admin/fields/field-layout-presentation";
	import "@admin/fields/field-layout.scss";
	import FieldTabLayout from "@admin/fields/field-tab-layout.svelte";
	import FieldCollapsible from "@admin/fields/field-collapsible.svelte";

	let {
		fields,
		form,
		inset = false,
		suppressedTabGroupID = "",
	}: {
		fields: readonly SchemaField[];
		form: FormController;
		inset?: boolean;
		suppressedTabGroupID?: string;
	} = $props();
	const parentPresentation = getFieldLayoutPresentation();
	setFieldLayoutPresentation({
		get groupActions() {
			return !inset && (parentPresentation?.groupActions ?? true);
		},
	});
</script>

{#snippet fieldGrid(tabFields: readonly SchemaField[])}
	<div class={["ridu-field-grid", inset && "ridu-field-grid--inset"]}>
		{#each createFieldLayout(tabFields, suppressedTabGroupID) as group (group.key)}
			{#if group.tabGroup !== undefined}
				<FieldTabLayout fields={group.fields} {form} tabGroup={group.tabGroup} />
			{:else if group.collapsible !== undefined}
				<FieldCollapsible fields={group.fields} {form} collapsible={group.collapsible} />
			{:else if group.row !== undefined}
				<div class="ridu-field-row" data-field-row={group.row.id}>
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
