<script lang="ts">
	import type { SchemaField, ValidationIssue } from "@riducms/protocol";

	import { Checkbox } from "@admin/components/ui/checkbox";
	import { DateValueControl } from "@admin/components/ui/date-value-control";
	import { Input } from "@admin/components/ui/input";
	import { Textarea } from "@admin/components/ui/textarea";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldMessages from "@admin/fields/field-messages.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const runtime = getAdminRuntime();
	const value = $derived(form.get(field.path));
	const issues = $derived(form.issuesFor(field.path));
	let jsonError = $state<string>();
	const displayIssues = $derived<ValidationIssue[]>(
		jsonError === undefined
			? issues
			: [{ code: "invalid_json", path: field.path, message: jsonError }]
	);
	const hasMessage = $derived(displayIssues.length > 0 || field.admin.description !== undefined);
	const dateAppearance = $derived(field.date?.pickerAppearance);
	const stringConstraints = $derived(field.type === "code" ? field.code : field.textarea);

	$effect(() => form.register(field.path));

	function setJSON(encoded: string) {
		try {
			form.set(field.path, JSON.parse(encoded));
			jsonError = undefined;
		} catch {
			jsonError = runtime.i18n.t("errors:validJSON");
		}
	}
</script>

{#if field.type === "checkbox"}
	<div class="grid gap-2" data-field-path={field.path} data-invalid={displayIssues.length > 0}>
		<label
			class="flex min-h-6 w-fit cursor-pointer items-center gap-2 text-[13px] text-foreground-muted hover:text-foreground has-disabled:cursor-not-allowed has-disabled:text-foreground-faint"
			for={field.id}
		>
			<Checkbox
				id={field.id}
				required={field.required}
				aria-invalid={displayIssues.length > 0}
				aria-describedby={hasMessage ? `${field.id}-message` : undefined}
				aria-errormessage={displayIssues.length > 0 ? `${field.id}-message` : undefined}
				disabled={field.admin.readOnly}
				bind:checked={() => Boolean(value), (next) => form.set(field.path, next)}
			/>
			<span>{field.admin.label}</span>
			{#if field.required}<span class="ridu-field-required" aria-hidden="true">*</span>{/if}
			{#if field.admin.readOnly}<span class="ridu-field-status">
					{runtime.i18n.t("fields:readOnly")}
				</span>{/if}
		</label>
		<FieldMessages
			id="{field.id}-message"
			issues={displayIssues}
			description={field.admin.description}
		/>
	</div>
{:else}
	<FieldShell {field} issues={displayIssues}>
		{#if field.type === "textarea" || field.type === "code"}
			<Textarea
				id={field.id}
				name={field.path}
				rows={1}
				required={field.required}
				minlength={stringConstraints?.minLength}
				maxlength={stringConstraints?.maxLength}
				placeholder={field.admin.placeholder}
				aria-invalid={displayIssues.length > 0}
				aria-describedby={hasMessage ? `${field.id}-message` : undefined}
				aria-errormessage={displayIssues.length > 0 ? `${field.id}-message` : undefined}
				value={String(value ?? "")}
				readonly={field.admin.readOnly}
				class={field.type === "code"
					? "min-h-14 resize-y field-sizing-fixed font-mono text-[12.5px] leading-5"
					: undefined}
				data-language={field.code?.language}
				oninput={(event) => form.set(field.path, event.currentTarget.value)}
			/>
		{:else if field.type === "json"}
			<Textarea
				id={field.id}
				name={field.path}
				required={field.required}
				aria-invalid={displayIssues.length > 0}
				aria-describedby={hasMessage ? `${field.id}-message` : undefined}
				aria-errormessage={displayIssues.length > 0 ? `${field.id}-message` : undefined}
				class="min-h-40 font-mono"
				value={JSON.stringify(value ?? {}, null, 2)}
				readonly={field.admin.readOnly}
				oninput={(event) => setJSON(event.currentTarget.value)}
			/>
		{:else if field.type === "date"}
			<DateValueControl
				id={field.id}
				name={field.path}
				appearance={dateAppearance}
				value={String(value ?? "")}
				readonly={field.admin.readOnly}
				required={field.required}
				invalid={displayIssues.length > 0}
				label={field.admin.label}
				describedBy={hasMessage ? `${field.id}-message` : undefined}
				onValueChange={(next) => form.set(field.path, next)}
			/>
		{:else}
			<Input
				id={field.id}
				name={field.path}
				required={field.required}
				aria-invalid={displayIssues.length > 0}
				aria-describedby={hasMessage ? `${field.id}-message` : undefined}
				aria-errormessage={displayIssues.length > 0 ? `${field.id}-message` : undefined}
				type={field.type === "number" ? "number" : field.type === "email" ? "email" : "text"}
				min={field.type === "number" ? field.number?.min : undefined}
				max={field.type === "number" ? field.number?.max : undefined}
				step={field.type === "number" ? field.number?.step : undefined}
				placeholder={field.admin.placeholder}
				value={String(value ?? "")}
				readonly={field.admin.readOnly}
				oninput={(event) =>
					form.set(
						field.path,
						field.type === "number" ? event.currentTarget.valueAsNumber : event.currentTarget.value
					)}
			/>
		{/if}
	</FieldShell>
{/if}
