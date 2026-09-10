<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA } from "@riducms/ui";
	import XIcon from "~icons/lucide/x";

	import { Button } from "@admin/components/ui/button";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";
	import {
		removeSelectValue,
		selectOptionLabel,
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
		field.select?.options.find((option) => option.value === value)?.label ?? placeholder
	);
	const selectedLabels = $derived(
		values.map((selected) => selectOptionLabel(field.select?.options ?? [], selected))
	);
	const issues = $derived(form.issuesFor(field.path));
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);

	$effect(() => form.register(field.path));

	function setValues(next: string[]) {
		if (editingBlocked) return;
		form.set(field.path, [...next]);
	}

	function removeValue(selected: string) {
		if (editingBlocked) return;
		form.set(field.path, removeSelectValue(values, selected));
	}
</script>

<FieldShell {field} {issues}>
	{#if hasMany}
		<Select type="multiple" value={values} onValueChange={setValues} disabled={editingBlocked}>
			<SelectTrigger
				id={field.id}
				class="w-full"
				{...controlARIA}
				aria-required={field.required}
				aria-label={field.admin.label}
			>
				<span class={values.length === 0 ? "text-foreground-placeholder" : "truncate"}>
					{values.length === 0 ? placeholder : runtime.i18n.formatList(selectedLabels)}
				</span>
			</SelectTrigger>
			<SelectContent>
				{#each field.select?.options ?? [] as option (option.value)}
					<SelectItem value={option.value} label={option.label} />
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
							{selectOptionLabel(field.select?.options ?? [], selected)}
						</span>
						{#if !field.admin.readOnly}
							<Button
								variant="ghost"
								size="icon-xs"
								disabled={editingBlocked}
								onclick={() => removeValue(selected)}
								aria-label={runtime.i18n.t("fields:remove", {
									label: selectOptionLabel(field.select?.options ?? [], selected),
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
			onValueChange={(next) => {
				if (!editingBlocked) form.set(field.path, next);
			}}
			disabled={editingBlocked}
		>
			<SelectTrigger
				id={field.id}
				class="w-full"
				{...controlARIA}
				aria-required={field.required}
				aria-label={field.admin.label}
			>
				<span class={value === "" ? "text-foreground-placeholder" : undefined}>
					{selectedLabel}
				</span>
			</SelectTrigger>
			<SelectContent>
				{#each field.select?.options ?? [] as option (option.value)}
					<SelectItem value={option.value} label={option.label} />
				{/each}
			</SelectContent>
		</Select>
	{/if}
</FieldShell>
