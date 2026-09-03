<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";

	import { RadioCardItem, RadioGroup } from "@admin/components/ui/radio-group";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const value = $derived(String(form.get(field.path) ?? ""));
	const issues = $derived(form.issuesFor(field.path));
	const hasMessage = $derived(issues.length > 0 || field.admin.description !== undefined);

	$effect(() => form.register(field.path));
</script>

<FieldShell {field} {issues}>
	<RadioGroup
		id={field.id}
		class="flex flex-wrap gap-x-7 gap-y-1"
		name={field.path}
		{value}
		required={field.required}
		readonly={field.admin.readOnly}
		aria-labelledby={`${field.id}-label`}
		aria-invalid={issues.length > 0}
		aria-describedby={hasMessage ? `${field.id}-message` : undefined}
		aria-errormessage={issues.length > 0 ? `${field.id}-message` : undefined}
		onValueChange={(next) => form.set(field.path, next)}
	>
		{#each field.select?.choices ?? [] as choice (choice.value)}
			<RadioCardItem value={choice.value} label={choice.label} />
		{/each}
	</RadioGroup>
</FieldShell>
