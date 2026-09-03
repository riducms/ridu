<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";
	import XIcon from "~icons/lucide/x";

	import { Button } from "@admin/components/ui/button";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";
	import {
		removeSelectValue,
		selectChoiceLabel,
		selectManyValues,
	} from "@admin/fields/select/select-value";

	interface Props {
		field: SchemaField;
		form: FormController;
	}

	let { field, form }: Props = $props();
	const runtime = getAdminRuntime();
	const hasMany = $derived(field.type === "select" && field.select?.hasMany === true);
	const value = $derived(String(form.get(field.path) ?? ""));
	const values = $derived(selectManyValues(form.get(field.path)));
	const placeholder = $derived(
		field.admin.placeholder ?? runtime.i18n.t("fields:select", { label: field.admin.label })
	);
	const selectedLabel = $derived(
		field.select?.choices.find((choice) => choice.value === value)?.label ?? placeholder
	);
	const selectedLabels = $derived(
		values.map((selected) => selectChoiceLabel(field.select?.choices ?? [], selected))
	);
	const issues = $derived(form.issuesFor(field.path));
	const hasMessage = $derived(issues.length > 0 || field.admin.description !== undefined);

	$effect(() => form.register(field.path));

	function setValues(next: string[]) {
		form.set(field.path, [...next]);
	}

	function removeValue(selected: string) {
		form.set(field.path, removeSelectValue(values, selected));
	}
</script>

<FieldShell {field} {issues}>
	{#if hasMany}
		<Select
			type="multiple"
			value={values}
			onValueChange={setValues}
			disabled={field.admin.readOnly}
		>
			<SelectTrigger
				id={field.id}
				class="w-full"
				aria-invalid={issues.length > 0}
				aria-required={field.required}
				aria-describedby={hasMessage ? `${field.id}-message` : undefined}
				aria-errormessage={issues.length > 0 ? `${field.id}-message` : undefined}
				aria-label={field.admin.label}
			>
				<span class={values.length === 0 ? "text-foreground-placeholder" : "truncate"}>
					{values.length === 0 ? placeholder : runtime.i18n.formatList(selectedLabels)}
				</span>
			</SelectTrigger>
			<SelectContent>
				{#each field.select?.choices ?? [] as choice (choice.value)}
					<SelectItem value={choice.value} label={choice.label} />
				{/each}
			</SelectContent>
		</Select>
		{#if values.length > 0}
			<ul
				class="mt-2 flex flex-wrap gap-1.5"
				aria-label={runtime.i18n.t("general:selected", { count: values.length })}
			>
				{#each values as selected, index (`${selected}:${index}`)}
					<li
						class="inline-flex max-w-full items-center gap-1 rounded-[3px] border border-control-border bg-background py-0.5 pe-0.5 ps-2 text-[12px] text-foreground-muted"
					>
						<span class="max-w-64 truncate">
							{selectChoiceLabel(field.select?.choices ?? [], selected)}
						</span>
						{#if !field.admin.readOnly}
							<Button
								variant="ghost"
								size="icon-xs"
								onclick={() => removeValue(selected)}
								aria-label={runtime.i18n.t("fields:remove", {
									label: selectChoiceLabel(field.select?.choices ?? [], selected),
								})}
							>
								<XIcon class="size-3" aria-hidden="true" />
							</Button>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	{:else}
		<Select
			type="single"
			{value}
			allowDeselect={!field.required}
			onValueChange={(next) => form.set(field.path, next)}
			disabled={field.admin.readOnly}
		>
			<SelectTrigger
				id={field.id}
				class="w-full"
				aria-invalid={issues.length > 0}
				aria-required={field.required}
				aria-describedby={hasMessage ? `${field.id}-message` : undefined}
				aria-errormessage={issues.length > 0 ? `${field.id}-message` : undefined}
				aria-label={field.admin.label}
			>
				<span class={value === "" ? "text-foreground-placeholder" : undefined}>
					{selectedLabel}
				</span>
			</SelectTrigger>
			<SelectContent>
				{#each field.select?.choices ?? [] as choice (choice.value)}
					<SelectItem value={choice.value} label={choice.label} />
				{/each}
			</SelectContent>
		</Select>
	{/if}
</FieldShell>
