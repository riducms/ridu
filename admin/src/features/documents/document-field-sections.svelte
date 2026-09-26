<script lang="ts">
	import "@admin/features/documents/document-field-sections.scss";
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
		class={["ridu-document-sections", !stacked && "ridu-document-sections--sidebar"]}
		data-document-field-columns
	>
		{#if partition.content.length > 0}
			<div class="ridu-document-sections__main" data-document-main-fields>
				<FieldLayout fields={partition.content} {form} />
			</div>
		{/if}
		<aside class="ridu-document-sections__sidebar" data-document-sidebar-fields>
			<FieldLayout fields={partition.sidebar} {form} />
		</aside>
	</div>
{/if}
