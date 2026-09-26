<script lang="ts">
	import type { SchemaAdminTimeZone } from "@riducms/protocol";
	import { timeZoneLabel } from "@admin/core/i18n/time-zone-label";
	import { getAdminI18n } from "@riducms/plugin";
	import {
		Combobox,
		ComboboxInput,
		ComboboxTrigger,
		ComboboxPortal,
		ComboboxContent,
		ComboboxViewport,
		ComboboxItem,
	} from "@admin/components/ui/combobox";
	import XIcon from "~icons/lucide/x";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import "@admin/components/ui/combobox/combobox.scss";

	let {
		id,
		value,
		options,
		at,
		disabled = false,
		onValueChange,
	}: {
		id: string;
		value: string;
		options: readonly SchemaAdminTimeZone[];
		at: Date;
		disabled?: boolean;
		onValueChange: (value: string) => void;
	} = $props();
	const i18n = getAdminI18n();
	let anchor = $state<HTMLDivElement | null>(null);
	let open = $state(false);
	let query = $state("");
	// Only the clear handler reads this binding; it does not drive rendering.
	// svelte-ignore non_reactive_update
	let input: HTMLInputElement;
	const label = $derived(i18n.t("fields:timeZone"));
	const placeholder = $derived(i18n.t("fields:select", { label }));
	const items = $derived(
		options.map((option) => ({
			value: option.id,
			label: timeZoneLabel(
				option.id,
				i18n.text(option.label, option.labelTranslations),
				i18n.language,
				at
			),
		}))
	);
	const selectedLabel = $derived(items.find((option) => option.value === value)?.label ?? value);
	const filtered = $derived(
		items.filter((option) =>
			`${option.label} ${option.value}`
				.toLocaleLowerCase(i18n.language)
				.includes(query.toLocaleLowerCase(i18n.language))
		)
	);

	function select(next: string) {
		if (!disabled) onValueChange(next);
	}

	function clear() {
		if (disabled) return;
		input.focus();
		onValueChange("");
	}
</script>

<Combobox
	type="single"
	{value}
	{items}
	onValueChange={select}
	allowDeselect={false}
	{disabled}
	bind:open={
		() => open,
		(next) => {
			open = next;
			if (!next) query = "";
		}
	}
	inputValue={open ? query : selectedLabel}
>
	<div bind:this={anchor} class="ridu-combobox" data-disabled={disabled}>
		<div class="ridu-combobox-value">
			<ComboboxInput
				{id}
				class="ridu-combobox-input"
				{placeholder}
				aria-label={label}
				autocomplete="off"
				autocorrect="off"
				autocapitalize="none"
				spellcheck={false}
				clearOnDeselect
				onpointerdown={() => {
					if (!disabled) open = true;
				}}
				oninput={(event) => {
					query = event.currentTarget.value;
					open = true;
				}}
			>
				{#snippet child({ props })}
					<input bind:this={input} {...props} value={open ? query : selectedLabel} />
				{/snippet}
			</ComboboxInput>
		</div>
		{#if value}
			<button
				type="button"
				class="ridu-combobox-icon"
				{disabled}
				aria-label={i18n.t("general:clearSelection")}
				onclick={clear}
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
			dir={i18n.direction}
		>
			<ComboboxViewport>
				{#each filtered as option (option.value)}
					<ComboboxItem value={option.value} label={option.label} class="ridu-combobox-option">
						{option.label}
					</ComboboxItem>
				{:else}
					<p class="ridu-combobox-message">{i18n.t("fields:noMatch", { label })}</p>
				{/each}
			</ComboboxViewport>
		</ComboboxContent>
	</ComboboxPortal>
</Combobox>
