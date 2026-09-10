<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA } from "@riducms/ui";

	import { RadioCardItem, RadioGroup } from "@admin/components/ui/radio-group";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const value = $derived(String(form.get(field.path) ?? ""));
	const issues = $derived(form.issuesFor(field.path));
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);

	$effect(() => form.register(field.path));
</script>

<FieldShell {field} {issues}>
	<RadioGroup
		id={field.id}
		class="flex flex-wrap gap-x-7 gap-y-1"
		name={field.path}
		{value}
		required={field.required && (!field.dynamicDefault || form.get(field.path) !== undefined)}
		readonly={editingBlocked}
		aria-labelledby={`${field.id}-label`}
		{...controlARIA}
		onValueChange={(next) => {
			if (!editingBlocked) form.set(field.path, next);
		}}
	>
		{#each field.select?.options ?? [] as option (option.value)}
			<RadioCardItem value={option.value} label={option.label} />
		{/each}
	</RadioGroup>
</FieldShell>
