<script lang="ts">
	import type { SchemaField, ValidationIssue } from "@riducms/protocol";
	import { fieldControlARIA } from "@riducms/ui";
	import { onDestroy } from "svelte";

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
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	let jsonError = $state<string>();
	let releaseUnfinishedInput: (() => void) | undefined;
	onDestroy(() => releaseUnfinishedInput?.());
	const displayIssues = $derived<ValidationIssue[]>(
		jsonError === undefined
			? issues
			: [{ code: "invalid_json", path: field.path, message: jsonError }]
	);
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, displayIssues.length > 0)
	);
	const dateAppearance = $derived(field.date?.format);
	const stringConstraints = $derived(field.type === "code" ? field.code : field.textarea);

	$effect(() => form.register(field.path));

	function setJSON(encoded: string) {
		if (editingBlocked) return;
		try {
			form.set(field.path, JSON.parse(encoded));
			releaseUnfinishedInput?.();
			releaseUnfinishedInput = undefined;
			jsonError = undefined;
		} catch {
			releaseUnfinishedInput?.();
			releaseUnfinishedInput = form.setLiveInputUnavailable(field.path);
			jsonError = runtime.i18n.t("errors:validJSON");
		}
	}
</script>

{#if field.type === "checkbox"}
	<div data-field-path={field.path} data-invalid={displayIssues.length > 0}>
		<FieldMessages
			controlID={field.id}
			issues={displayIssues}
			description={field.admin.description}
		>
			<label
				class="flex min-h-6 w-fit cursor-pointer items-center gap-2 text-[13px] text-foreground-muted hover:text-foreground has-disabled:cursor-not-allowed has-disabled:text-foreground-faint"
				for={field.id}
			>
				<Checkbox
					id={field.id}
					required={field.required && (!field.dynamicDefault || form.get(field.path) !== undefined)}
					{...controlARIA}
					disabled={editingBlocked}
					bind:checked={() => Boolean(value), (next) => form.set(field.path, next)}
				/>
				<span>{field.admin.label}</span>
				{#if field.required}<span class="ridu-field-required" aria-hidden="true">*</span>{/if}
				{#if field.admin.readOnly}<span class="ridu-field-status">
						{runtime.i18n.t("fields:readOnly")}
					</span>{/if}
			</label>
		</FieldMessages>
	</div>
{:else}
	<FieldShell {field} issues={displayIssues}>
		{#if field.type === "textarea" || field.type === "code"}
			<Textarea
				id={field.id}
				name={field.path}
				rows={1}
				required={field.required && (!field.dynamicDefault || form.get(field.path) !== undefined)}
				minlength={stringConstraints?.minLength}
				maxlength={stringConstraints?.maxLength}
				placeholder={field.admin.placeholder}
				{...controlARIA}
				value={String(value ?? "")}
				readonly={editingBlocked}
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
				required={field.required && (!field.dynamicDefault || form.get(field.path) !== undefined)}
				{...controlARIA}
				class="min-h-40 font-mono"
				value={JSON.stringify(value ?? {}, null, 2)}
				readonly={editingBlocked}
				oninput={(event) => setJSON(event.currentTarget.value)}
			/>
		{:else if field.type === "date"}
			<DateValueControl
				id={field.id}
				name={field.path}
				appearance={dateAppearance}
				value={String(value ?? "")}
				disabled={form.editingBlocked}
				readonly={field.admin.readOnly}
				required={field.required && (!field.dynamicDefault || form.get(field.path) !== undefined)}
				invalid={displayIssues.length > 0}
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
