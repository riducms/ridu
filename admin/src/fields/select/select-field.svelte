<script lang="ts">
	import {
		Combobox,
		ComboboxInput,
		ComboboxTrigger,
		ComboboxPortal,
		ComboboxViewport,
		ComboboxContent,
		ComboboxItem,
	} from "@admin/components/ui/combobox";
	import type { SchemaField } from "@riducms/protocol";
	import { fieldControlARIA } from "@riducms/ui";
	import XIcon from "~icons/lucide/x";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldShell from "@admin/fields/field-shell.svelte";
	import {
		removeSelectValue,
		selectOptionLabel,
		selectManyValues,
	} from "@admin/fields/select/select-value";
	import "@admin/components/ui/combobox/combobox.scss";

	let { field, form }: { field: SchemaField; form: FormController } = $props();
	const runtime = getAdminRuntime();
	let anchor = $state<HTMLDivElement | null>(null);
	let open = $state(false);
	let query = $state("");

	const hasMany = $derived(field.select?.hasMany === true);
	const value = $derived(String(form.get(field.path) ?? ""));
	const values = $derived(selectManyValues(form.get(field.path)));
	const options = $derived(field.select?.options ?? []);
	const filtered = $derived(
		options.filter((option) =>
			option.label
				.toLocaleLowerCase(runtime.i18n.language)
				.includes(query.toLocaleLowerCase(runtime.i18n.language))
		)
	);
	const selectedLabel = $derived(options.find((option) => option.value === value)?.label ?? "");
	const placeholder = $derived(
		field.admin.placeholder ?? runtime.i18n.t("fields:select", { label: field.admin.label })
	);
	const issues = $derived(form.issuesFor(field.path));
	const editingBlocked = $derived(field.admin.readOnly === true || form.editingBlocked);
	const controlARIA = $derived(
		fieldControlARIA(field.id, field.admin.description !== undefined, issues.length > 0)
	);
	const selection = $derived(
		hasMany
			? { type: "multiple" as const, value: values, onValueChange: setValues }
			: { type: "single" as const, value, onValueChange: setValue }
	);

	$effect(() => form.register(field.path));

	function setValues(next: string[]) {
		if (!editingBlocked) form.set(field.path, [...next]);
	}

	function setValue(next: string) {
		if (!editingBlocked) form.set(field.path, next);
	}
</script>

<FieldShell {field} {issues}>
	<!-- The combobox must restart when a retained field switches between single and multiple. -->
	{#key hasMany}
		<Combobox
			{...selection}
			allowDeselect={false}
			items={options}
			disabled={editingBlocked}
			bind:open={
				() => open,
				(next) => {
					open = next;
					if (!next) query = "";
				}
			}
			inputValue={open || hasMany ? query : selectedLabel}
		>
			<div
				bind:this={anchor}
				class="ridu-combobox"
				aria-invalid={issues.length > 0}
				data-disabled={editingBlocked}
			>
				<div class="ridu-combobox-value">
					{#if hasMany}
						{#each values as selected (selected)}
							<span class="ridu-combobox-tag">
								{selectOptionLabel(options, selected)}
								<button
									type="button"
									class="ridu-combobox-icon"
									disabled={editingBlocked}
									aria-label={runtime.i18n.t("fields:remove", {
										label: selectOptionLabel(options, selected),
									})}
									onclick={() => setValues(removeSelectValue(values, selected))}
								>
									<XIcon />
								</button>
							</span>
						{/each}
					{/if}
					<ComboboxInput
						id={field.id}
						class="ridu-combobox-input"
						autocomplete="off"
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						{placeholder}
						{...controlARIA}
						aria-label={field.admin.label}
						aria-required={field.required}
						clearOnDeselect
						onpointerdown={() => (open = true)}
						oninput={(event) => {
							query = event.currentTarget.value;
							open = true;
						}}
					/>
				</div>
				{#if !hasMany && value}
					<button
						type="button"
						class="ridu-combobox-icon"
						disabled={editingBlocked}
						aria-label={runtime.i18n.t("general:clearSelection")}
						onclick={() => setValue("")}
					>
						<XIcon />
					</button>
				{/if}
				<ComboboxTrigger
					type="button"
					class="ridu-combobox-icon ridu-combobox-trigger"
					aria-label={placeholder}
				>
					<ChevronDownIcon />
				</ComboboxTrigger>
			</div>
			<ComboboxPortal>
				<ComboboxContent
					customAnchor={anchor}
					sideOffset={0}
					align="start"
					class="ridu-combobox-popup"
				>
					<ComboboxViewport>
						{#each filtered as option (option.value)}
							<ComboboxItem value={option.value} label={option.label} class="ridu-combobox-option">
								{option.label}
							</ComboboxItem>
						{:else}
							<p class="ridu-combobox-message">
								{runtime.i18n.t("fields:noMatch", { label: field.admin.label })}
							</p>
						{/each}
					</ComboboxViewport>
				</ComboboxContent>
			</ComboboxPortal>
		</Combobox>
	{/key}
</FieldShell>
