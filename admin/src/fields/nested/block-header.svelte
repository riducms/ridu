<script lang="ts">
	import type { SchemaBlockType } from "@riducms/protocol";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { scopeRepeatedRowField } from "@admin/fields/nested/scoped-field";
	import BlockNameInput from "@admin/fields/nested/block-name-input.svelte";

	let {
		block,
		path,
		instance,
		form,
		readOnly = false,
		disabled = false,
		onChange,
	}: {
		block: SchemaBlockType;
		path: string;
		instance: string;
		form: FormController;
		readOnly?: boolean;
		disabled?: boolean;
		onChange?: (change: { field: string; value: string }) => void;
	} = $props();
	const runtime = getAdminRuntime();
	const child = $derived(block.fields.find((field) => field.name === block.admin?.nameField));
	const schema = $derived(
		child === undefined ? undefined : scopeRepeatedRowField(child, path, instance)
	);
</script>

{#if schema !== undefined}
	{#key form}
		{#key `${form.editorEpoch}:${runtime.manifestRevision}:${schema.id}`}
			<BlockNameInput {schema} {block} {form} {readOnly} {disabled} {onChange} />
		{/key}
	{/key}
{/if}
