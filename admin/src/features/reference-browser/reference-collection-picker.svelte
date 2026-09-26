<script lang="ts">
	import { flushSync } from "svelte";
	import { getAdminI18n } from "@riducms/plugin";
	import type { SchemaCollection } from "@riducms/protocol";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import {
		Combobox,
		ComboboxInput,
		ComboboxTrigger,
		ComboboxPortal,
		ComboboxViewport,
		ComboboxContent,
		ComboboxItem,
	} from "@admin/components/ui/combobox";
	import "@admin/components/ui/combobox/combobox.scss";

	let {
		collections,
		value,
		disabled,
		onchange,
	}: {
		collections: readonly SchemaCollection[];
		value: string;
		disabled: boolean;
		onchange: (slug: string) => void;
	} = $props();

	const i18n = getAdminI18n();
	const id = $props.id();
	let anchor = $state.raw<HTMLDivElement | null>(null);
	let open = $state(false);
	let query = $state("");
	const options = $derived(
		collections.map((collection) => ({
			value: collection.slug,
			label: i18n.text(collection.labels.singular, collection.labels.singularTranslations),
		}))
	);
	const selectedLabel = $derived(options.find((option) => option.value === value)?.label ?? "");
	const filtered = $derived(
		options.filter((option) =>
			option.label.toLocaleLowerCase(i18n.language).includes(query.toLocaleLowerCase(i18n.language))
		)
	);

	function search(event: Event) {
		const value = (event.currentTarget as HTMLInputElement).value;
		// Bits reads candidate nodes later in this same input event.
		flushSync(() => {
			query = value;
			open = true;
		});
	}
</script>

<div class="ridu-reference-collection">
	<label for={id}>{i18n.t("reference:selectCollection")}</label>
	<Combobox
		type="single"
		{value}
		{disabled}
		items={options}
		allowDeselect={false}
		onValueChange={onchange}
		inputValue={open ? query : selectedLabel}
		bind:open={
			() => open,
			(next) => {
				open = next;
				if (!next) query = "";
			}
		}
	>
		<div bind:this={anchor} class="ridu-combobox" data-disabled={disabled}>
			<div class="ridu-combobox-value">
				<ComboboxInput
					{id}
					class="ridu-combobox-input"
					placeholder={selectedLabel}
					autocomplete="off"
					autocorrect="off"
					autocapitalize="none"
					spellcheck={false}
					onpointerdown={() => (open = true)}
					oninput={search}
				/>
			</div>
			<ComboboxTrigger
				type="button"
				class="ridu-combobox-icon ridu-combobox-trigger"
				aria-label={i18n.t("reference:selectCollection")}
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
						<p class="ridu-combobox-message">{i18n.t("reference:noCollectionsMatch")}</p>
					{/each}
				</ComboboxViewport>
			</ComboboxContent>
		</ComboboxPortal>
	</Combobox>
</div>
