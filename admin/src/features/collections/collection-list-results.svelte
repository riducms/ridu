<script lang="ts">
	import type { AdminI18n, AdminListCell, FieldDocument } from "@riducms/plugin";
	import type { SchemaCollection, SchemaField } from "@riducms/protocol";
	import ArrowDownIcon from "~icons/lucide/arrow-down";
	import ArrowUpIcon from "~icons/lucide/arrow-up";
	import ArrowUpDownIcon from "~icons/lucide/arrow-up-down";
	import type { Snippet } from "svelte";

	import { Button } from "@admin/components/ui/button";
	import { Checkbox } from "@admin/components/ui/checkbox";
	import { EmptyState } from "@admin/components/ui/empty-state";
	import { Pagination } from "@admin/components/ui/pagination";
	import { StatusIndicator } from "@admin/components/ui/status-indicator";
	import {
		Table,
		TableBody,
		TableCell,
		TableHead,
		TableHeader,
		TableRow,
	} from "@admin/components/ui/table";
	import type { CollectionListController } from "@admin/features/collections/collection-list-controller.svelte";
	import CollectionTableSkeleton from "@admin/features/collections/collection-table-skeleton.svelte";
	import { sortableField } from "@admin/features/collections/list-workspace";

	type ResolvedListCell = Omit<AdminListCell, "field"> & { field: SchemaField };

	let {
		i18n,
		controller,
		resourceKey,
		collection,
		titleField,
		customListCells,
		visibleColumns,
		showID,
		showCreated,
		showUpdated,
		parentFieldName,
		hierarchy,
		trashOnly,
		filtered,
		sortField,
		sortDescending,
		columnValue,
		onClearFilters,
		onToggleSort,
		onSelectPage,
		onRequestDeletePermanent,
		documentLink,
		createAction,
	}: {
		i18n: AdminI18n;
		controller: CollectionListController;
		resourceKey: string;
		collection: SchemaCollection | undefined;
		titleField: SchemaField | undefined;
		customListCells: readonly ResolvedListCell[];
		visibleColumns: readonly SchemaField[];
		showID: boolean;
		showCreated: boolean;
		showUpdated: boolean;
		parentFieldName: string | undefined;
		hierarchy: boolean;
		trashOnly: boolean;
		filtered: boolean;
		sortField: string;
		sortDescending: boolean;
		columnValue: (document: FieldDocument, field: SchemaField) => string;
		onClearFilters: () => void;
		onToggleSort: (path: string) => void;
		onSelectPage: (page: number) => void;
		onRequestDeletePermanent: (id: string) => void;
		documentLink: Snippet<[document: FieldDocument]>;
		createAction?: Snippet;
	} = $props();
	// Capture the identity represented by the currently rendered rows. A resource switch must pass
	// through loading before a new successful result can claim that identity.
	// svelte-ignore state_referenced_locally
	let renderedResourceKey = $state(resourceKey);
	let observedResourceLoading = false;

	const {
		allOnPageSelected,
		bulkPending,
		canCreate,
		docs,
		mutationPending,
		pageSelectionIndeterminate,
		pagination,
		rangeEnd,
		rangeStart,
		selectedIDs,
		selectionControlsReady,
		status,
		statusVisible,
	} = $derived(controller);
	const hierarchyRows = $derived(buildHierarchyRows(docs, parentFieldName));
	const displayedRows = $derived(
		hierarchy && !trashOnly ? hierarchyRows : docs.map((document) => ({ document, depth: 0 }))
	);
	const resourceChanging = $derived(resourceKey !== renderedResourceKey);
	const pluralLabel = $derived(
		collection?.labels.plural.toLocaleLowerCase(i18n.language) ?? i18n.t("collections:documents")
	);

	$effect(() => {
		const nextResourceKey = resourceKey;
		const nextStatus = status;
		if (nextResourceKey === renderedResourceKey) {
			observedResourceLoading = false;
			return;
		}
		if (nextStatus === "initial" || nextStatus === "loading") {
			observedResourceLoading = true;
			return;
		}
		if (observedResourceLoading && (nextStatus === "ready" || nextStatus === "failed")) {
			renderedResourceKey = nextResourceKey;
			observedResourceLoading = false;
		}
	});

	function ariaSort(path: string) {
		if (sortField !== path) return undefined;
		return sortDescending ? ("descending" as const) : ("ascending" as const);
	}

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
</script>

<div
	data-slot="collection-list-results"
	class="mt-2 min-w-0"
	aria-busy={resourceChanging || status === "loading"}
>
	{#if resourceChanging || ((status === "initial" || status === "loading") && docs.length === 0)}
		<CollectionTableSkeleton showStatus={statusVisible} {showID} {showCreated} {showUpdated} />
	{:else if docs.length === 0}
		{#if filtered}
			<EmptyState
				title={i18n.t("collections:nothingMatches")}
				description={i18n.t("collections:noMatchingDocuments")}
			>
				<Button variant="outline" onclick={onClearFilters}>
					{i18n.t("collections:clearFilters")}
				</Button>
			</EmptyState>
		{:else}
			<EmptyState
				title={trashOnly
					? i18n.t("collections:trashEmpty")
					: i18n.t("collections:empty", { label: pluralLabel })}
				description={trashOnly
					? i18n.t("collections:trashEmptyDescription")
					: i18n.t("collections:emptyDescription")}
			>
				{#if !trashOnly && canCreate}{@render createAction?.()}{/if}
			</EmptyState>
		{/if}
	{:else}
		<Table
			class="min-w-[640px] border-collapse text-start"
			aria-label={i18n.t("collections:collectionDocuments", { label: pluralLabel })}
		>
			<TableHeader>
				<TableRow>
					<TableHead class="w-7 px-2.5">
						<Checkbox
							checked={allOnPageSelected}
							indeterminate={pageSelectionIndeterminate}
							disabled={!selectionControlsReady || bulkPending}
							onCheckedChange={controller.togglePage}
							aria-label={i18n.t("collections:selectAllRows")}
						/>
					</TableHead>
					<TableHead
						aria-sort={titleField !== undefined && sortableField(titleField)
							? ariaSort(titleField.name)
							: undefined}
					>
						{#if titleField !== undefined && sortableField(titleField)}
							<Button
								variant="ghost"
								size="sm"
								class="-ms-2 h-7 gap-1 px-2"
								onclick={() => onToggleSort(titleField.name)}
								aria-label={i18n.t("collections:sortBy", {
									label: titleField.admin.label,
								})}
							>
								{titleField.admin.label}
								{#if sortField === titleField.name}
									{#if sortDescending}<ArrowDownIcon class="size-3" />{:else}<ArrowUpIcon
											class="size-3"
										/>{/if}
								{:else}<ArrowUpDownIcon class="size-3 text-foreground-faint" />{/if}
							</Button>
						{:else}
							{titleField?.admin.label ?? i18n.t("collections:title")}
						{/if}
					</TableHead>
					{#if statusVisible}
						<TableHead class="w-36">{i18n.t("collections:status")}</TableHead>
					{/if}
					{#each customListCells as cell (cell.key)}
						<TableHead>
							{cell.labelKey === undefined ? cell.label : i18n.t(cell.labelKey)}
						</TableHead>
					{/each}
					{#each visibleColumns as field (field.id)}
						<TableHead aria-sort={sortableField(field) ? ariaSort(field.path) : undefined}>
							{#if sortableField(field)}
								<Button
									variant="ghost"
									size="sm"
									class="-ms-2 h-7 gap-1 px-2"
									onclick={() => onToggleSort(field.path)}
									aria-label={i18n.t("collections:sortBy", {
										label: field.admin.label,
									})}
								>
									{field.admin.label}
									{#if sortField === field.path}
										{#if sortDescending}<ArrowDownIcon class="size-3" />{:else}<ArrowUpIcon
												class="size-3"
											/>{/if}
									{:else}<ArrowUpDownIcon class="size-3 text-foreground-faint" />{/if}
								</Button>
							{:else}
								{field.admin.label}
							{/if}
						</TableHead>
					{/each}
					{#if showID}<TableHead>ID</TableHead>{/if}
					{#if showCreated}<TableHead class="w-24">
							{i18n.t("collections:created")}
						</TableHead>{/if}
					{#if showUpdated}<TableHead class="w-24 text-end">
							{i18n.t("collections:updated")}
						</TableHead>{/if}
					{#if trashOnly}<TableHead class="w-52 text-end">
							{i18n.t("collections:actions")}
						</TableHead>{/if}
				</TableRow>
			</TableHeader>
			<TableBody class={{ "opacity-55": status === "loading" }}>
				{#each displayedRows as row (row.document.id)}
					{const document = row.document}
					<TableRow
						class="group"
						data-state={selectedIDs.has(document.id) ? "selected" : undefined}
					>
						<TableCell class="w-7 px-2.5">
							<Checkbox
								checked={selectedIDs.has(document.id)}
								disabled={!selectionControlsReady || bulkPending}
								onCheckedChange={(checked) => controller.toggleDocument(document.id, checked)}
								aria-label={i18n.t("collections:selectDocument", {
									title: controller.title(document),
								})}
							/>
						</TableCell>
						<TableCell
							style={hierarchy && !trashOnly
								? `padding-inline-start: ${14 + row.depth * 24}px`
								: undefined}
						>
							{#if trashOnly}
								<div class="block max-w-[600px]">
									<span class="block truncate text-[14px] font-medium text-foreground">
										{controller.title(document)}
									</span>
								</div>
							{:else}
								{@render documentLink(document)}
							{/if}
						</TableCell>
						{#if statusVisible}
							{const documentStatus = $derived(controller.documentStatus(document))}
							<TableCell>
								{#if documentStatus !== undefined}
									<StatusIndicator
										tone={controller.statusTone(documentStatus)}
										pulse={documentStatus.toLocaleLowerCase(i18n.language) === "published"}
									>
										{controller.statusLabel(documentStatus)}
									</StatusIndicator>
								{:else}
									<span class="text-[12px] text-foreground-faint">—</span>
								{/if}
							</TableCell>
						{/if}
						{#each customListCells as cell (cell.key)}
							<TableCell>
								<cell.component
									collection={collection!}
									field={cell.field}
									{document}
									value={document[cell.field.name]}
									{i18n}
								/>
							</TableCell>
						{/each}
						{#each visibleColumns as field (field.id)}
							<TableCell class="max-w-52 truncate text-[12px] text-foreground-muted">
								{columnValue(document, field)}
							</TableCell>
						{/each}
						{#if showID}
							<TableCell class="font-mono max-w-52 truncate text-[11px] text-foreground-sub">
								{document.id}
							</TableCell>
						{/if}
						{#if showCreated}
							<TableCell class="font-mono text-[11px] text-foreground-sub">
								{controller.created(document)}
							</TableCell>
						{/if}
						{#if showUpdated}
							<TableCell class="font-mono text-end text-[11px] text-foreground-sub">
								{controller.updated(document)}
							</TableCell>
						{/if}
						{#if trashOnly}
							<TableCell class="text-end">
								<div class="flex justify-end gap-1.5">
									{#if controller.canRestore(document.id)}<Button
											variant="outline"
											size="sm"
											disabled={mutationPending.has(document.id)}
											onclick={() => controller.restore(document.id)}
											aria-label={i18n.t("collections:restoreDocument", {
												title: controller.title(document),
											})}
										>
											{i18n.t("documents:restore")}
										</Button>{/if}
									{#if controller.canDeletePermanent(document.id)}<Button
											variant="destructive"
											size="sm"
											disabled={mutationPending.has(document.id)}
											onclick={() => onRequestDeletePermanent(document.id)}
											aria-label={i18n.t("collections:deleteDocumentPermanently", {
												title: controller.title(document),
											})}
										>
											{i18n.t("collections:deletePermanently")}
										</Button>{/if}
								</div>
							</TableCell>
						{/if}
					</TableRow>
				{/each}
			</TableBody>
		</Table>
	{/if}
</div>

{#if (!hierarchy || trashOnly) && !resourceChanging}
	<Pagination
		start={rangeStart}
		end={rangeEnd}
		total={pagination.totalDocs}
		page={pagination.page}
		totalPages={pagination.totalPages}
		hasPrevious={pagination.hasPrevPage}
		hasNext={pagination.hasNextPage}
		busy={status === "loading"}
		label={i18n.t("collections:pagination")}
		summary={i18n.t("collections:rangeSummary", {
			start: i18n.formatNumber(rangeStart),
			end: i18n.formatNumber(rangeEnd),
			total: i18n.formatNumber(pagination.totalDocs),
		})}
		pageSummary={i18n.t("collections:pageSummary", {
			page: i18n.formatNumber(pagination.page),
			pages: i18n.formatNumber(Math.max(pagination.totalPages, 1)),
		})}
		previousLabel={i18n.t("collections:previousPage")}
		nextLabel={i18n.t("collections:nextPage")}
		onPrevious={() => onSelectPage(pagination.page - 1)}
		onNext={() => onSelectPage(pagination.page + 1)}
	/>
{/if}
