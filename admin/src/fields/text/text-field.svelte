<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";

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
	const hasMessage = $derived(issues.length > 0 || field.admin.description !== undefined);

	$effect(() => form.register(field.path));
</script>

{#if field.text?.slug !== undefined}
	<SlugField {field} {form} />
{:else}
	<FieldShell {field} {issues}>
		<Input
			id={field.id}
			name={field.path}
			required={field.required}
			minlength={field.text?.minLength}
			maxlength={field.text?.maxLength}
			placeholder={field.admin.placeholder}
			aria-invalid={issues.length > 0}
			aria-describedby={hasMessage ? `${field.id}-message` : undefined}
			aria-errormessage={issues.length > 0 ? `${field.id}-message` : undefined}
			readonly={field.admin.readOnly}
			{value}
			oninput={(event) => form.set(field.path, event.currentTarget.value)}
		/>
	</FieldShell>
{/if}
