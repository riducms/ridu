<script lang="ts">
	import "@admin/fields/field-layout.scss";
	import type { SchemaField } from "@riducms/protocol";
	import { Checkbox, fieldControlARIA, Input, Textarea } from "@riducms/ui";
	import { DateValueControl } from "@admin/components/ui/date-value-control";

	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import CodeField from "@admin/fields/scalar/code-field.svelte";
	import FieldMessages from "@admin/fields/field-messages.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const runtime = getAdminRuntime();
	const value = $derived(form.get(field.path));
	const issues = $derived(form.issuesFor(field.path));
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);

	$effect(() => form.register(field.path));
</script>

{#if field.type === "code" || field.type === "json"}
	<CodeField {field} {form} />
{:else if field.type === "checkbox"}
	<div data-field-path={field.path} data-invalid={issues.length > 0}>
		<FieldMessages controlID={field.id} {issues} description={field.admin.description}>
			<label class="ridu-checkbox-field" for={field.id}>
				<Checkbox
					id={field.id}
					required={field.required && (!field.dynamicDefault || form.get(field.path) !== undefined)}
					{...controlARIA}
					disabled={editingBlocked}
					bind:checked={() => Boolean(value), (next) => form.set(field.path, next)}
				/>
				<span>{field.admin.label}</span>
				{#if field.required}
					<span class="ridu-field-required" aria-hidden="true">*</span>
				{/if}
				{#if field.admin.readOnly}
					<span class="ridu-field-status">
						{runtime.i18n.t("fields:readOnly")}
					</span>
				{/if}
			</label>
		</FieldMessages>
	</div>
{:else}
	<FieldShell {field} {issues}>
		{#if field.type === "textarea"}
			<Textarea
				id={field.id}
				name={field.path}
				rows={1}
				required={field.required && (!field.dynamicDefault || form.get(field.path) !== undefined)}
				minlength={field.textarea?.minLength}
				maxlength={field.textarea?.maxLength}
				placeholder={field.admin.placeholder}
				{...controlARIA}
				value={String(value ?? "")}
				readonly={editingBlocked}
				oninput={(event) => form.set(field.path, event.currentTarget.value)}
			/>
		{:else if field.type === "date"}
			<DateValueControl
				id={field.id}
				name={field.path}
				appearance={field.date?.format}
				value={String(value ?? "")}
				disabled={form.editingBlocked}
				readonly={field.admin.readOnly}
				required={field.required && (!field.dynamicDefault || form.get(field.path) !== undefined)}
				invalid={issues.length > 0}
				hasDescription={field.admin.description !== undefined}
				label={field.admin.label}
				onValueChange={(next) => form.set(field.path, next)}
			/>
		{:else}
			<Input
				id={field.id}
				name={field.path}
				required={field.required && (!field.dynamicDefault || form.get(field.path) !== undefined)}
				{...controlARIA}
				type={field.type === "number" ? "number" : field.type === "email" ? "email" : "text"}
				min={field.type === "number" ? field.number?.min : undefined}
				max={field.type === "number" ? field.number?.max : undefined}
				step={field.type === "number" ? field.number?.step : undefined}
				placeholder={field.admin.placeholder}
				value={String(value ?? "")}
				readonly={editingBlocked}
				oninput={(event) =>
					form.set(
						field.path,
						field.type === "number" ? event.currentTarget.valueAsNumber : event.currentTarget.value
					)}
			/>
		{/if}
	</FieldShell>
{/if}
