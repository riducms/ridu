<script lang="ts">
	import SortHeading from "@admin/features/collections/controls/sort-heading.svelte";
	import { resolveListCells, resolveListColumns } from "@admin/features/collections/list-columns";
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import Chevron from "@admin/components/icons/chevron.svelte";
	import SearchIcon from "@admin/components/icons/search.svelte";
	import XIcon from "@admin/components/icons/x.svelte";
	import { Banner } from "@admin/components/ui/banner";
	import { Checkbox } from "@riducms/ui";
	import { CollapsiblePanel } from "@admin/components/ui/collapsible-panel";
	import ListPagination from "@admin/features/collections/controls/list-pagination.svelte";
	import PencilIcon from "~icons/lucide/pencil";
	import ColumnPicker from "@admin/features/collections/controls/column-picker.svelte";
	import FilterBuilder from "@admin/features/collections/controls/filter-builder.svelte";
	import { createCollectionListCellFormatter } from "@admin/features/collections/collection-list-cell-values";
	import type { ListColumn } from "@admin/features/collections/list-workspace";
	import type { AdminDocument } from "@admin/core/api/admin-client";
	import type { ReferenceBrowserWorkflow } from "@admin/features/reference-browser/reference-browser-workflow.svelte";
	import "@admin/features/collections/collection-list.scss";
	import "@admin/components/data/list-table.scss";
	import "@admin/features/collections/controls/list-controls.scss";
	import "@admin/features/reference-browser/reference-browser-list.scss";

	let {
		controller,
		searchElement = $bindable<HTMLInputElement | null>(null),
	}: {
		controller: ReferenceBrowserWorkflow;
		searchElement?: HTMLInputElement | null;
	} = $props();

	const i18n = getAdminI18n();
	// A browser session mounts one stable workflow; its schema and runtime are not replaced.
	// svelte-ignore state_referenced_locally
	const { runtime, field, hasMany } = controller.options;
	const {
		collection,
		docs,
		pagination,
		status,
		error,
		query,
		workingSelection,
		committing,
		columns,
		listFilters,
		sort,
		pageSize,
	} = $derived(controller);
	const pluralLabel = $derived(
		i18n.text(collection.labels.plural, collection.labels.pluralTranslations)
	);
	const singularLabel = $derived(
		i18n.text(collection.labels.singular, collection.labels.singularTranslations)
	);
	const searchLabel = $derived(
		controller.searchField === undefined
			? pluralLabel
			: i18n.text(
					controller.searchField.admin.label,
					controller.searchField.admin.labelTranslations
				)
	);
	const customListCells = $derived(
		resolveListCells(collection, runtime.config.extensions.listCellRenderers)
	);
	const availableColumns = $derived(resolveListColumns(collection, i18n, customListCells));
	const visibleColumns = $derived.by(() => {
		const selected = columns.flatMap((selection) => {
			const column = availableColumns.find((candidate) => candidate.path === selection.path);
			return selection.active && column && controller.canReadField(column.path) ? [column] : [];
		});
		return selected.length > 0
			? selected
			: availableColumns.filter((column) => column.path === "id" && controller.canReadField("id"));
	});
	const filterFields = $derived(controller.filterFields.readable(controller.canReadField));
	const linkedColumn = $derived(
		visibleColumns.find(
			(column) => !customListCells.some((cell) => cell.field.path === column.path)
		)
	);
	const selectedOnPage = $derived(
		docs.filter((document) => workingSelection.includes(document.id)).length
	);
	const allOnPageSelected = $derived(docs.length > 0 && selectedOnPage === docs.length);
	const pageSelectionIndeterminate = $derived(selectedOnPage > 0 && !allOnPageSelected);
	const busy = $derived(status === "loading" || committing);
	// The formatter is stable for the mounted workflow and reads live access at call time.
	// svelte-ignore state_referenced_locally
	const formatCellValue = createCollectionListCellFormatter({
		i18n,
		get collections() {
			return runtime.manifest?.collections ?? [];
		},
		canReadField: controller.canReadField,
	});
	let panel = $state<"columns" | "filters">();
	function cellLabel(document: AdminDocument, column: ListColumn) {
		if (column.path === "filename" && collection.capabilities.upload)
			return controller.documentLabel(document);
		if (column.field !== undefined) return formatCellValue(document, column.field);
		return document.id;
	}

	function togglePage(checked: boolean) {
		if (busy || controller.options.readOnly) return;
		for (const document of docs) {
			if (controller.workingSelection.includes(document.id) !== checked)
				controller.toggleSelection(document.id);
		}
	}

	function clearSearch() {
		controller.query = "";
		controller.page = 1;
		searchElement?.focus();
	}
</script>

<div class="ridu-reference-list" data-slot="reference-browser-list">
	<div class="ridu-list-controls">
		<div class="ridu-list-search">
			<div class="ridu-list-search__input">
				<SearchIcon />
				<label class="sr-only" for="{field.id}-relationship-search">
					{i18n.t("reference:search", { label: pluralLabel })}
				</label>
				<input
					id="{field.id}-relationship-search"
					bind:this={searchElement}
					type="search"
					value={query}
					disabled={committing || controller.searchField === undefined}
					oninput={controller.handleSearch}
					placeholder={i18n.t("collections:searchBy", { label: searchLabel })}
				/>
				{#if query !== ""}
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
					aria-controls="{field.id}-reference-columns"
					onclick={() => (panel = panel === "columns" ? undefined : "columns")}
				>
					{i18n.t("collections:columns")}
					<Chevron class={panel === "columns" ? "ridu-list__chevron-up" : undefined} />
				</button>
				{#if !filterFields.empty}
					<button
						type="button"
						class="ridu-list__pill"
						aria-expanded={panel === "filters"}
						aria-controls="{field.id}-reference-filters"
						onclick={() => (panel = panel === "filters" ? undefined : "filters")}
					>
						{i18n.t("collections:filters")}
						{#if listFilters.length > 0}
							<span>({listFilters.flat().length})</span>
						{/if}
						<Chevron class={panel === "filters" ? "ridu-list__chevron-up" : undefined} />
					</button>
				{/if}
			</div>
		</div>
		<CollapsiblePanel id="{field.id}-reference-columns" open={panel === "columns"}>
			<ColumnPicker
				{columns}
				{availableColumns}
				canReadField={controller.canReadField}
				onChange={controller.setColumns}
			/>
		</CollapsiblePanel>
		<CollapsiblePanel id="{field.id}-reference-filters" open={panel === "filters"}>
			<FilterBuilder
				fields={filterFields}
				filters={listFilters}
				label={pluralLabel}
				onChange={controller.setListFilters}
			/>
		</CollapsiblePanel>
	</div>

	<div class="ridu-reference-list__results" aria-busy={busy}>
		{#if error !== undefined}
			<Banner tone="destructive">{error}</Banner>
		{/if}
		{#if (status === "initial" || status === "loading") && docs.length === 0}
			<div class="ridu-list__loading" role="status" data-ridu-loading-surface="reference-results">
				{i18n.t("reference:loadingRelated")}
			</div>
		{:else if docs.length === 0}
			<div class="ridu-list__empty">
				<h2>{i18n.t("reference:nothingMatches")}</h2>
				<p>
					{i18n.t("reference:searchOrCreate", {
						label: singularLabel.toLocaleLowerCase(i18n.language),
					})}
				</p>
			</div>
		{:else}
			<div class="ridu-list-table__scroll">
				<table class="ridu-list-table" aria-label={i18n.t("reference:results")}>
					<thead>
						<tr>
							{#if hasMany}
								<th class="ridu-list-table__selection" scope="col">
									<Checkbox
										checked={allOnPageSelected}
										indeterminate={pageSelectionIndeterminate}
										disabled={busy || controller.options.readOnly || status === "failed"}
										onCheckedChange={togglePage}
										aria-label={i18n.t("collections:selectAllRows")}
									/>
								</th>
							{/if}
							{#each visibleColumns as column (column.path)}
								<SortHeading
									{column}
									{sort}
									onSort={controller.setSort}
									ready={!busy}
									disabled={committing}
								/>
							{/each}
						</tr>
					</thead>
					<tbody>
						{#each docs as document (document.id)}
							<tr data-selected={workingSelection.includes(document.id)}>
								{#if hasMany}
									<td class="ridu-list-table__selection">
										<Checkbox
											checked={workingSelection.includes(document.id)}
											disabled={busy || controller.options.readOnly || status === "failed"}
											onCheckedChange={(checked) => {
												if (workingSelection.includes(document.id) !== checked)
													controller.toggleSelection(document.id);
											}}
											aria-label={i18n.t("collections:selectDocument", {
												title: controller.documentLabel(document),
											})}
										/>
									</td>
								{/if}
								{#each visibleColumns as column, index (column.path)}
									{const cell = customListCells.find(
										(candidate) => candidate.field.path === column.path
									)}
									<td>
										{#if cell && controller.canReadField(column.path, document.id)}
											<cell.component
												{collection}
												field={cell.field}
												{document}
												value={document[cell.field.name]}
												{i18n}
											/>
										{:else if column.path === linkedColumn?.path}
											<button
												type="button"
												class="ridu-reference-list__document-button"
												disabled={busy || controller.options.readOnly || status === "failed"}
												onclick={() =>
													hasMany
														? controller.toggleSelection(document.id)
														: controller.selectDocument(document.id)}
											>
												{#if collection.capabilities.upload && column.path === "filename" && controller.isImage(document) && controller.mediaURL(document)}
													<img
														class="ridu-reference-list__thumbnail"
														src={controller.mediaURL(document)}
														alt=""
													/>
												{/if}
												{cellLabel(document, column)}
											</button>
										{:else}
											{cellLabel(document, column)}
										{/if}
										{#if index === 0 && linkedColumn === undefined}
											<button
												type="button"
												class="ridu-list-table__document-link"
												disabled={busy || controller.options.readOnly || status === "failed"}
												onclick={() =>
													hasMany
														? controller.toggleSelection(document.id)
														: controller.selectDocument(document.id)}
											>
												{controller.documentLabel(document)}
											</button>
										{/if}
										{#if index === 0}
											<button
												type="button"
												class="ridu-reference-list__edit-button"
												disabled={busy || status === "failed"}
												aria-label={i18n.t("reference:editLabel", {
													label: controller.documentLabel(document),
												})}
												onclick={() => controller.openKnownDocument(document)}
											>
												<PencilIcon aria-hidden="true" />
											</button>
										{/if}
									</td>
								{/each}
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</div>

	<footer class="ridu-reference-list__footer">
		<ListPagination
			{pagination}
			{pageSize}
			{busy}
			onPageChange={controller.setPage}
			onPageSizeChange={controller.setPageSize}
		/>
		{#if hasMany}
			<div class="ridu-reference-list__selection-actions">
				<Button
					variant="outline"
					disabled={committing}
					onclick={() => controller.requestOpenChange(false)}
				>
					{i18n.t("reference:cancel")}
				</Button>
				{#if !controller.options.readOnly}
					<Button
						disabled={!controller.canCommitSelection || committing}
						onclick={() => controller.commitSelection()}
					>
						{committing
							? i18n.t("reference:applying")
							: i18n.t("reference:addSelected", {
									count: i18n.formatNumber(workingSelection.length),
								})}
					</Button>
				{/if}
			</div>
		{/if}
	</footer>
</div>
