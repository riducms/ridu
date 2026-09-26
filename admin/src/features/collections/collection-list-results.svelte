<script lang="ts">
	import { getAdminI18n, type AdminCollectionList, type FieldDocument } from "@riducms/plugin";
	import { Link } from "@hvniel/svelte-router";
	import { buttonVariants } from "@riducms/ui";
	import { Checkbox } from "@riducms/ui";
	import { getCollectionList } from "@admin/features/collections/collection-list.svelte";
	import { type ListColumn } from "@admin/features/collections/list-workspace";
	import SortHeading from "@admin/features/collections/controls/sort-heading.svelte";
	import ListPagination from "@admin/features/collections/controls/list-pagination.svelte";

	let { onClearFilters }: { onClearFilters: () => void } = $props();

	const list = getCollectionList();
	const i18n = getAdminI18n();
	const { controller, documentHref, formatCellValue: columnValue } = list;

	const {
		slug: resourceKey,
		collection,
		titleField,
		customListCells,
		customResults,
		visibleColumns,
		parentFieldName,
		trashOnly,
		filtered,
	} = $derived(list);

	const hierarchy = $derived(list.view.view === "hierarchy");
	// Capture the identity represented by the currently rendered rows. Prepared data can claim the
	// destination before render; fallback data waits until the controller has loaded that request.
	// svelte-ignore state_referenced_locally
	let renderedResourceKey = $state.raw(resourceKey);

	const {
		allOnPageSelected,
		bulkPending,
		canCreate,
		docs,
		pageSelectionIndeterminate,
		selectedIDs,
		selectionControlsReady,
		status,
	} = $derived(controller);

	const pluginList: AdminCollectionList = {
		documentHref,
		get documents() {
			return controller.docs;
		},
		get selectedIDs() {
			return controller.selectedIDs;
		},
		get selectionEnabled() {
			return controller.selectionControlsReady && !controller.bulkPending;
		},
		get allOnPageSelected() {
			return controller.allOnPageSelected;
		},
		get pageSelectionIndeterminate() {
			return controller.pageSelectionIndeterminate;
		},
		toggleDocument: (id, checked) => controller.toggleDocument(id, checked),
		togglePage: (checked) => controller.togglePage(checked),
		title: (document) => controller.title(document),
	};

	const linkedColumn = $derived(
		visibleColumns.find(
			(column) =>
				controller.canReadField(column.path) &&
				!customListCells.some((cell) => cell.field.path === column.path)
		)
	);

	const hierarchyRows = $derived(buildHierarchyRows(docs, parentFieldName));
	const displayedRows = $derived(
		hierarchy && !trashOnly ? hierarchyRows : docs.map((document) => ({ document, depth: 0 }))
	);

	const resourceChanging = $derived(resourceKey !== renderedResourceKey);
	const resourcePending = $derived(resourceChanging && !controller.selectionControlsReady);

	const pluralLabel = $derived(
		collection?.labels.plural.toLocaleLowerCase(i18n.language) ?? i18n.t("collections:documents")
	);

	$effect.pre(() => {
		const nextResourceKey = resourceKey;
		const nextStatus = status;
		const nextReady = controller.selectionControlsReady;
		if (
			nextResourceKey !== renderedResourceKey &&
			nextReady &&
			(nextStatus === "ready" || nextStatus === "failed")
		) {
			renderedResourceKey = nextResourceKey;
		}
	});

	function buildHierarchyRows(documents: readonly FieldDocument[], parentName: string | undefined) {
		if (parentName === undefined) return documents.map((document) => ({ document, depth: 0 }));

		const byParent = new Map<string, FieldDocument[]>();
		const ids = new Set(documents.map((document) => document.id));

		for (const document of documents) {
			const raw = document[parentName];
			const parent = typeof raw === "string" ? raw : undefined;
			const key = parent !== undefined && ids.has(parent) ? parent : "";
			byParent.set(key, [...(byParent.get(key) ?? []), document]);
		}

		const rows: { document: FieldDocument; depth: number }[] = [];
		const visited = new Set<string>();
		const visit = (parent: string, depth: number) => {
			for (const document of byParent.get(parent) ?? []) {
				if (visited.has(document.id)) continue;

				visited.add(document.id);
				rows.push({ document, depth });
				visit(document.id, depth + 1);
			}
		};
		visit("", 0);

		for (const document of documents) {
			if (!visited.has(document.id)) rows.push({ document, depth: 0 });
		}

		return rows;
	}

	function cellLabel(document: FieldDocument, column: ListColumn) {
		if (column.path === titleField?.path) return controller.title(document);
		if (column.field && !["id", "createdAt", "updatedAt"].includes(column.path))
			return columnValue(document, column.field);
		if (column.path === "id") return document.id;

		const value = document[column.path];

		return typeof value === "string"
			? i18n.formatDate(value, {
					month: "long",
					day: "numeric",
					year: "numeric",
					hour: "numeric",
					minute: "2-digit",
				})
			: "—";
	}
</script>

<div
	data-slot="collection-list-results"
	class="ridu-list-results"
	inert={!list.navigationIdle}
	aria-busy={!list.navigationIdle || resourcePending || status === "loading"}
>
	{#if resourcePending || ((status === "initial" || status === "loading") && docs.length === 0)}
		<div class="ridu-list__loading" role="status">{i18n.t("collections:loadingDocuments")}</div>
	{:else if docs.length === 0}
		<div class={["ridu-list__empty", trashOnly && "ridu-list__empty--trash"]}>
			{#if trashOnly}
				<p>
					{i18n.t("collections:noTrashResults", {
						label: collection?.labels.plural ?? pluralLabel,
					})}
				</p>
			{:else}
				<h2>
					{filtered
						? i18n.t("collections:nothingMatches")
						: i18n.t("collections:empty", { label: pluralLabel })}
				</h2>
				<p>
					{filtered
						? i18n.t("collections:noMatchingDocuments")
						: i18n.t("collections:emptyDescription")}
				</p>
				{#if filtered}
					<button type="button" class="ridu-list__pill" onclick={onClearFilters}>
						{i18n.t("collections:clearFilters")}
					</button>
				{:else if canCreate}
					<Link class={buttonVariants({ variant: "outline" })} to={list.createHref}>
						{i18n.t("collections:createNew", {
							label:
								collection?.labels.singular.toLocaleLowerCase(i18n.language) ??
								i18n.t("collections:document"),
						})}
					</Link>
				{/if}
			{/if}
		</div>
	{:else if customResults !== undefined && collection !== undefined && !trashOnly}
		<customResults.component {collection} list={pluginList} {i18n} />
	{:else}
		<div class="ridu-list-table__scroll">
			<table
				class="ridu-list-table"
				aria-label={i18n.t("collections:collectionDocuments", { label: pluralLabel })}
			>
				<thead>
					<tr>
						<th class="ridu-list-table__selection">
							<Checkbox
								checked={allOnPageSelected}
								indeterminate={pageSelectionIndeterminate}
								disabled={!selectionControlsReady || bulkPending}
								onCheckedChange={controller.togglePage}
								aria-label={i18n.t("collections:selectAllRows")}
							/>
						</th>
						{#each visibleColumns as column (column.path)}
							<SortHeading
								{column}
								sort={list.view.sort}
								onSort={list.query.setSort}
								ready={list.navigationIdle}
							/>
						{/each}
					</tr>
				</thead>

				<tbody>
					{#each displayedRows as row (row.document.id)}
						{const document = row.document}
						<tr data-selected={selectedIDs.has(document.id)}>
							<td class="ridu-list-table__selection">
								<Checkbox
									checked={selectedIDs.has(document.id)}
									disabled={!selectionControlsReady || bulkPending}
									onCheckedChange={(checked) => controller.toggleDocument(document.id, checked)}
									aria-label={i18n.t("collections:selectDocument", {
										title: controller.title(document),
									})}
								/>
							</td>
							{#each visibleColumns as column, index (column.path)}
								{const cell = customListCells.find((cell) => cell.field.path === column.path)}
								<td
									style:padding-inline-start={hierarchy && index === 0
										? `${16 + row.depth * 24}px`
										: undefined}
								>
									{#if cell && collection && controller.canReadField(column.path)}
										<cell.component
											{collection}
											field={cell.field}
											{document}
											value={document[cell.field.name]}
											{i18n}
										/>
									{:else if column.path === linkedColumn?.path && !trashOnly}
										<Link to={documentHref(document)}>
											{cellLabel(document, column)}
										</Link>
									{:else}
										{cellLabel(document, column)}
									{/if}
									{#if !trashOnly && index === 0 && linkedColumn === undefined}
										<Link class="ridu-list-table__document-link" to={documentHref(document)}>
											{controller.title(document)}
										</Link>
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

{#if docs.length > 0 && (!hierarchy || trashOnly) && !resourceChanging}
	<ListPagination
		pagination={controller.pagination}
		pageSize={list.view.limit}
		busy={status === "loading"}
		inert={!list.navigationIdle}
		onPageChange={list.query.selectPage}
		onPageSizeChange={list.setPageSize}
	/>
{/if}
