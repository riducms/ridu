<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import type { SchemaField } from "@riducms/protocol";
	import { flushSync } from "svelte";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import XIcon from "~icons/lucide/x";

	import {
		Combobox,
		ComboboxContent,
		ComboboxInput,
		ComboboxItem,
		ComboboxPortal,
		ComboboxTrigger,
		ComboboxViewport,
	} from "@admin/components/ui/combobox";
	import "@admin/components/ui/combobox/combobox.scss";
	import "@admin/features/bulk-edit/bulk-edit.scss";

	let {
		fields,
		value,
		onValueChange,
		disabled = false,
	}: {
		fields: readonly SchemaField[];
		value: string[];
		onValueChange: (paths: string[]) => void;
		disabled?: boolean;
	} = $props();

	const i18n = getAdminI18n();
	const id = $props.id();
	let anchor = $state<HTMLDivElement | null>(null);
	// Only the remove handler reads this DOM binding.
	// svelte-ignore non_reactive_update
	let input: HTMLInputElement | null = null;
	let open = $state(false);
	let query = $state("");

	const options = $derived(
		fields.map((field) => ({
			value: field.path,
			label: i18n.text(field.admin.label, field.admin.labelTranslations),
		}))
	);
	const available = $derived(
		options.filter(
			(option) =>
				!value.includes(option.value) &&
				option.label
					.toLocaleLowerCase(i18n.language)
					.includes(query.toLocaleLowerCase(i18n.language))
		)
	);
	const selected = $derived(
		value.flatMap((path) => options.find((option) => option.value === path) ?? [])
	);

	$effect(() => {
		if (disabled) changeOpen(false);
	});

	function changeOpen(next: boolean) {
		open = next && !disabled;
		if (!open) query = "";
	}

	function select(next: string[]) {
		if (disabled) return;
		onValueChange(next);
		changeOpen(false);
	}

	function remove(path?: string) {
		if (disabled) return;
		onValueChange(path === undefined ? [] : value.filter((item) => item !== path));
		query = "";
		input?.focus();
	}
</script>

<div class="ridu-bulk-field-picker">
	<label for={id}>{i18n.t("collections:selectFieldsToEdit")}</label>
	<Combobox
		type="multiple"
		{value}
		onValueChange={select}
		inputValue={query}
		items={options}
		bind:open={() => open, changeOpen}
		{disabled}
		allowDeselect={false}
		loop
	>
		<div bind:this={anchor} class="ridu-combobox" data-disabled={disabled}>
			<div class="ridu-combobox-value">
				{#each selected as option (option.value)}
					<span class="ridu-combobox-tag">
						{option.label}
						<button
							type="button"
							class="ridu-combobox-icon"
							{disabled}
							aria-label={i18n.t("fields:remove", { label: option.label })}
							onclick={() => remove(option.value)}
						>
							<XIcon />
						</button>
					</span>
				{/each}
				<ComboboxInput
					{id}
					bind:ref={input}
					class="ridu-combobox-input"
					autocomplete="off"
					autocorrect="off"
					autocapitalize="none"
					spellcheck={false}
					placeholder={value.length === 0 ? i18n.t("collections:selectFieldsPlaceholder") : ""}
					onpointerdown={() => changeOpen(true)}
					oninput={(event) => {
						const value = event.currentTarget.value;
						// Bits highlights immediately after this callback. Filter first so it
						// cannot retain a removed option as its keyboard navigation origin.
						flushSync(() => {
							query = value;
							changeOpen(true);
						});
					}}
				>
					{#snippet child({ props })}
						<!-- Bits tracks picked labels internally; this input only displays the search. -->
						<input {...props} value={query} />
					{/snippet}
				</ComboboxInput>
			</div>
			{#if value.length > 0}
				<button
					type="button"
					class="ridu-combobox-icon"
					{disabled}
					aria-label={i18n.t("general:clearSelection")}
					onclick={() => remove()}
				>
					<XIcon />
				</button>
			{/if}
			<ComboboxTrigger
				type="button"
				class="ridu-combobox-icon ridu-combobox-trigger"
				aria-label={i18n.t("collections:selectFieldsToEdit")}
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
					{#each available as option (option.value)}
						<ComboboxItem value={option.value} label={option.label} class="ridu-combobox-option">
							{option.label}
						</ComboboxItem>
					{:else}
						<p class="ridu-combobox-message">{i18n.t("collections:noFieldsFound")}</p>
					{/each}
				</ComboboxViewport>
			</ComboboxContent>
		</ComboboxPortal>
	</Combobox>
</div>
