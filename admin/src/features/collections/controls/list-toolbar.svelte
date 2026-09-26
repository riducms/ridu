<script lang="ts">
	import "@admin/features/collections/controls/list-controls.scss";
	import { getAdminI18n } from "@riducms/plugin";
	import Chevron from "@admin/components/icons/chevron.svelte";
	import SearchIcon from "@admin/components/icons/search.svelte";
	import XIcon from "@admin/components/icons/x.svelte";
	import { CollapsiblePanel } from "@admin/components/ui/collapsible-panel";
	import ColumnPicker from "@admin/features/collections/controls/column-picker.svelte";
	import FilterBuilder from "@admin/features/collections/controls/filter-builder.svelte";
	import ListOptions from "@admin/features/collections/controls/list-options.svelte";
	import { getCollectionList } from "@admin/features/collections/collection-list.svelte";

	let {
		searchInput = $bindable<HTMLInputElement | null>(null),
	}: { searchInput?: HTMLInputElement | null } = $props();

	const list = getCollectionList();
	const i18n = getAdminI18n();

	const { view, searchLabel, filterFields, availableColumnFields, label, navigationIdle } =
		$derived(list);
	const { query: listQuery } = list;

	let panel = $state<"columns" | "filters">();

	const readableFields = $derived(
		filterFields.filter((field) => list.controller.canReadField(field.path))
	);

	const statusLabel = $derived(
		filterFields
			.find((field) => field.path === "status")
			?.select?.options.find((option) => option.value === view.status)?.label ?? view.status
	);

	function clearSearch() {
		listQuery.clearSearch();
		searchInput?.focus();
	}
</script>

<div class="ridu-list-controls" data-slot="collection-list-workspace-toolbar">
	{#if view.status !== ""}
		<div class="ridu-list-controls__context">
			<button
				type="button"
				class="ridu-list__pill"
				onclick={() => listQuery.selectStatus("")}
				aria-label={i18n.t("collections:clearStatus")}
			>
				{i18n.t("collections:status")}: {statusLabel}<XIcon />
			</button>
		</div>
	{/if}

	<div class="ridu-list-search">
		<div class="ridu-list-search__input">
			<SearchIcon />
			<label class="sr-only" for="collection-search">{searchLabel}</label>
			<input
				id="collection-search"
				bind:this={searchInput}
				type="search"
				value={view.q}
				oninput={(event) => listQuery.handleSearch(event.currentTarget.value)}
				placeholder={i18n.t("collections:searchPlaceholder", { label: searchLabel })}
			/>
			{#if view.q !== ""}
				<button
					type="button"
					class="ridu-list-search__clear"
					aria-label={i18n.t("collections:clearSearch")}
					onclick={clearSearch}
				>
					<XIcon />
				</button>
			{/if}
		</div>

		<div class="ridu-list-search__actions">
			<button
				type="button"
				class="ridu-list__pill"
				aria-expanded={panel === "columns"}
				aria-controls="collection-columns"
				onclick={() => (panel = panel === "columns" ? undefined : "columns")}
			>
				{i18n.t("collections:columns")}<Chevron
					class={panel === "columns" ? "ridu-list__chevron-up" : undefined}
				/>
			</button>
			{#if filterFields.length > 0}
				<button
					type="button"
					class="ridu-list__pill"
					aria-expanded={panel === "filters"}
					aria-controls="collection-filters"
					onclick={() => (panel = panel === "filters" ? undefined : "filters")}
				>
					{i18n.t("collections:filters")}
					{#if view.filters.length > 0}
						<span>
							({view.filters.flat().length})
						</span>
					{/if}
					<Chevron class={panel === "filters" ? "ridu-list__chevron-up" : undefined} />
				</button>
			{/if}
			<ListOptions />
		</div>
	</div>

	<CollapsiblePanel id="collection-columns" open={panel === "columns"}>
		<ColumnPicker
			columns={list.columnChoices}
			availableColumns={availableColumnFields}
			canReadField={list.controller.canReadField}
			onChange={list.setColumns}
			{navigationIdle}
		/>
	</CollapsiblePanel>

	<CollapsiblePanel id="collection-filters" open={panel === "filters"}>
		<FilterBuilder
			fields={readableFields}
			filters={view.filters}
			{label}
			onChange={listQuery.updateListFilters}
			{navigationIdle}
		/>
	</CollapsiblePanel>
</div>
