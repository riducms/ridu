<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";

	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { partitionDocumentFields } from "@admin/features/documents/document-field-partition";
	import FieldLayout from "@admin/fields/field-layout.svelte";

	let {
		fields,
		form,
		stacked = false,
	}: {
		fields: readonly SchemaField[];
		form: FormController;
		stacked?: boolean;
	} = $props();
	const partition = $derived(partitionDocumentFields(fields));
</script>

{#if partition.sidebar.length === 0}
	<FieldLayout fields={partition.content} {form} />
{:else}
	<div
		class={[
			"grid min-w-0 gap-8",
			!stacked && "xl:grid-cols-[minmax(0,1fr)_minmax(260px,340px)] xl:items-start",
		]}
		data-document-field-columns
	>
		{#if partition.content.length > 0}
			<div class="min-w-0" data-document-main-fields>
				<FieldLayout fields={partition.content} {form} />
			</div>
		{/if}
		<aside
			class={[
				"min-w-0 border-t border-control-border pt-7",
				!stacked && "xl:border-t-0 xl:border-s xl:pt-0 xl:ps-7",
				!stacked && partition.content.length === 0 && "xl:col-start-2",
			]}
			data-document-sidebar-fields
		>
			<FieldLayout fields={partition.sidebar} {form} />
		</aside>
	</div>
{/if}
