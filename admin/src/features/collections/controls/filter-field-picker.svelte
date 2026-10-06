<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import ChevronDownIcon from "@admin/components/icons/chevron.svelte";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import type { ListFilterFields } from "@admin/features/collections/list-filter-fields";
	import "@admin/components/ui/select/select.scss";
	import "@admin/features/collections/controls/list-controls.scss";
	import "@admin/features/collections/controls/filter-field-picker.scss";

	let {
		fields,
		value,
		onValueChange,
	}: {
		fields: ListFilterFields;
		value: string;
		onValueChange: (path: string) => void;
	} = $props();

	const i18n = getAdminI18n();

	let open = $state(false);
	// The panel's navigation, search and styles load when the picker first opens, keeping them
	// out of the collection list's initial bundle. Pointing at or focusing the trigger starts
	// the request early.
	let panel =
		$state.raw<
			Promise<
				typeof import("@admin/features/collections/controls/filter-field-picker-panel.svelte")
			>
		>();

	const selected = $derived(fields.resolve(value));

	function loadPanel() {
		panel ??= import("@admin/features/collections/controls/filter-field-picker-panel.svelte");
	}

	function changeOpen(next: boolean) {
		if (next) loadPanel();
		open = next;
	}

	function select(path: string) {
		open = false;
		if (path !== value) onValueChange(path);
	}
</script>

<Popover bind:open={() => open, changeOpen}>
	<PopoverTrigger
		class="ridu-select__trigger"
		data-size="field"
		aria-label={i18n.t("collections:filterField")}
		title={selected?.trail.join(" > ")}
		onpointerenter={loadPanel}
		onfocus={loadPanel}
		onkeydown={(event) => {
			// Open like the neighbouring Select triggers.
			if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
			event.preventDefault();
			changeOpen(true);
		}}
	>
		<span class="ridu-filter-field-picker__name">
			{#if selected === undefined}
				{i18n.t("collections:chooseField")}
			{:else}
				<!-- A long trail yields its space first so the chosen field stays readable. -->
				{#if selected.trail.length > 1}
					<span class="ridu-filter-field-picker__trail">
						{selected.trail.slice(0, -1).join(" > ")} &gt;
					</span>
					{" "}
				{/if}
				<span class="ridu-filter-field-picker__label">{selected.trail.at(-1)}</span>
			{/if}
		</span>
		<ChevronDownIcon class="ridu-select__chevron" />
	</PopoverTrigger>
	<PopoverContent
		class="ridu-filter-field-picker"
		align="start"
		dir={i18n.direction}
		onOpenAutoFocus={(event) => {
			// The panel focuses its search input once it has loaded.
			event.preventDefault();
		}}
	>
		{#await panel}
			<p class="ridu-filter-field-picker__status" role="status">{i18n.t("general:loading")}</p>
		{:then module}
			{#if module}
				<module.default {fields} {value} onSelect={select} />
			{/if}
		{:catch}
			<div class="ridu-filter-field-picker__status" role="alert">
				<p>{i18n.t("collections:filterFieldsLoadFailed")}</p>
				<Button variant="outline" size="sm" onclick={() => window.location.reload()}>
					{i18n.t("general:reloadPage")}
				</Button>
			</div>
		{/await}
	</PopoverContent>
</Popover>
