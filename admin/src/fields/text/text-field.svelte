<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA } from "@riducms/ui";

	import { Input } from "@admin/components/ui/input";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";
	import SlugField from "@admin/fields/text/slug-field.svelte";

	interface Props {
		field: SchemaField;
		form: FormController;
	}

	let { field, form }: Props = $props();
	const value = $derived(String(form.get(field.path) ?? ""));
	const issues = $derived(form.issuesFor(field.path));
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);

	$effect(() => form.register(field.path));
</script>

{#if field.text?.slug !== undefined}
	<SlugField {field} {form} />
{:else}
	<FieldShell {field} {issues}>
		<Input
			id={field.id}
			name={field.path}
			required={field.required && (!field.dynamicDefault || form.get(field.path) !== undefined)}
			minlength={field.text?.minLength}
			maxlength={field.text?.maxLength}
			placeholder={field.admin.placeholder}
			{...controlARIA}
			readonly={editingBlocked}
			{value}
			oninput={(event) => form.set(field.path, event.currentTarget.value)}
		/>
	</FieldShell>
{/if}
