<script lang="ts">
	import type { SchemaField } from "@riducms/protocol";
	import type { Snippet } from "svelte";
	import Columns3Icon from "~icons/lucide/columns-3";
	import ListTreeIcon from "~icons/lucide/list-tree";
	import SaveIcon from "~icons/lucide/save";
	import SearchIcon from "~icons/lucide/search";
	import SlidersHorizontalIcon from "~icons/lucide/sliders-horizontal";
	import XIcon from "~icons/lucide/x";

	import { Button, buttonVariants } from "@admin/components/ui/button";
	import { Checkbox } from "@admin/components/ui/checkbox";
	import { Input } from "@admin/components/ui/input";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import type { AdminDocument } from "@admin/core/api/admin-client";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import CollectionListFilterPopover from "@admin/features/collections/collection-list-filter-popover.svelte";
	import type { CollectionListPreset } from "@admin/features/collections/collection-list-preferences-controller.svelte";
	import {
		listPageSizes,
		type ListFilter,
		type ListPageSize,
	} from "@admin/features/collections/list-workspace";

	const runtime = getAdminRuntime();

	let {
		slug,
		query,
		searchLabel,
		searchInput = $bindable<HTMLInputElement | null>(null),
		filterFields,
		filters,
		canReadField,
		folders,
		requestedFolder,
		manageFoldersLink,
		showFolder = false,
		showHierarchy = false,
		hierarchy = false,
		sessionActive = false,
		presets,
		presetsPending = false,
		statusColumnAvailable,
		availableColumnFields,
		visibleColumns,
		showStatus,
		showID,
		showCreated,
		showUpdated,
		pageSize,
		onSearch,
		onClearSearch,
		onFiltersChange,
		onSelectFolder,
		onSelectView,
		onSavePreset,
		onApplyPreset,
		onDeletePreset,
		onToggleColumn,
		onToggleStatus,
		onToggleID,
		onToggleCreated,
		onToggleUpdated,
		onSelectPageSize,
	}: {
		slug: string;
		query: string;
		searchLabel: string;
		searchInput?: HTMLInputElement | null;
		filterFields: readonly SchemaField[];
		filters: readonly ListFilter[];
		canReadField: (path: string) => boolean;
		folders: readonly AdminDocument[];
		requestedFolder: string;
		manageFoldersLink?: Snippet;
		showFolder?: boolean;
		showHierarchy?: boolean;
		hierarchy?: boolean;
		sessionActive?: boolean;
		presets: readonly CollectionListPreset[];
		presetsPending?: boolean;
		statusColumnAvailable: boolean;
		availableColumnFields: readonly SchemaField[];
		visibleColumns: readonly SchemaField[];
		showStatus: boolean;
		showID: boolean;
		showCreated: boolean;
		showUpdated: boolean;
		pageSize: ListPageSize;
		onSearch: (value: string) => void;
		onClearSearch: () => void;
		onFiltersChange: (filters: readonly ListFilter[]) => void;
		onSelectFolder: (folder: string) => void;
		onSelectView: (view: "list" | "hierarchy") => void;
		onSavePreset: (name: string) => Promise<boolean>;
		onApplyPreset: (preset: CollectionListPreset) => void;
		onDeletePreset: (name: string) => void;
		onToggleColumn: (path: string, show: boolean) => void;
		onToggleStatus: (show: boolean) => void;
		onToggleID: (show: boolean) => void;
		onToggleCreated: (show: boolean) => void;
		onToggleUpdated: (show: boolean) => void;
		onSelectPageSize: (size: ListPageSize) => void;
	} = $props();

	let presetName = $state("");
	const sortedPresets = $derived(
		[...presets].sort((left, right) =>
			left.name.localeCompare(right.name, runtime.i18n.language, {
				numeric: true,
				sensitivity: "base",
			})
		)
	);
	const sortedFolders = $derived(
		[...folders].sort((left, right) =>
			folderLabel(left).localeCompare(folderLabel(right), runtime.i18n.language, {
				numeric: true,
				sensitivity: "base",
			})
		)
	);
	const selectedFolderLabel = $derived(
		requestedFolder === ""
			? runtime.i18n.t("collections:allFolders")
			: String(
					folders.find((folder) => folder.id === requestedFolder)?.name ??
						folders.find((folder) => folder.id === requestedFolder)?.title ??
						requestedFolder
				)
	);

	function folderLabel(folder: AdminDocument) {
		return String(folder.name ?? folder.title ?? folder.id);
	}

	async function savePreset() {
		if (await onSavePreset(presetName)) presetName = "";
	}
</script>

<div
	class="mt-5 flex flex-wrap items-center gap-2 rounded-[3px] bg-control p-2"
	data-slot="collection-list-workspace-toolbar"
>
	<div class="relative min-w-52 flex-1 sm:max-w-[320px]">
		<SearchIcon
			class="pointer-events-none absolute top-1/2 start-3.5 size-3.5 -translate-y-1/2 text-foreground-faint"
			aria-hidden="true"
		/>
		<label class="sr-only" for="collection-search">{searchLabel}</label>
		<Input
			id="collection-search"
			bind:ref={searchInput}
			type="search"
			class="h-[31px] border-control-border bg-background px-8 text-[12.5px]"
			value={query}
			oninput={(event) => onSearch(event.currentTarget.value)}
			placeholder={runtime.i18n.t("collections:searchPlaceholder", { label: searchLabel })}
		/>
		{#if query !== ""}
			<Button
				variant="ghost"
				size="icon-xs"
				class="absolute top-1/2 end-1.5 -translate-y-1/2"
				onclick={onClearSearch}
				aria-label={runtime.i18n.t("collections:clearSearch")}
			>
				<XIcon class="size-3" />
			</Button>
		{/if}
	</div>

	<div class="ms-auto flex flex-wrap items-center justify-end gap-2">
		{#if filterFields.length > 0}
			{#key slug}
				<CollectionListFilterPopover
					i18n={runtime.i18n}
					fields={filterFields}
					{filters}
					{canReadField}
					{onFiltersChange}
				/>
			{/key}
		{/if}

		<Popover>
			<PopoverTrigger
				class={buttonVariants({ variant: "outline", class: "h-[31px] gap-1.75 px-3" })}
			>
				<Columns3Icon class="size-3 text-foreground-faint" />
				{runtime.i18n.t("collections:columns")}
			</PopoverTrigger>
			<PopoverContent align="end" class="w-44 gap-1 p-2">
				<p class="px-2 pt-1 pb-1 text-[11px] font-medium text-foreground-muted">
					{runtime.i18n.t("collections:columns")}
				</p>
				{#if statusColumnAvailable}
					<label
						class="flex h-9 cursor-pointer items-center gap-2.5 rounded-[3px] px-2 text-[13px] text-foreground-muted hover:bg-control hover:text-foreground-strong"
					>
						<Checkbox
							checked={showStatus}
							onCheckedChange={onToggleStatus}
							aria-label={runtime.i18n.t("collections:showStatusColumn")}
						/>
						{runtime.i18n.t("collections:status")}
					</label>
				{/if}
				{#each availableColumnFields as field (field.id)}
					{#if canReadField(field.path)}
						<label
							class="flex h-9 cursor-pointer items-center gap-2.5 rounded-[3px] px-2 text-[13px] text-foreground-muted hover:bg-control hover:text-foreground-strong"
						>
							<Checkbox
								checked={visibleColumns.some((candidate) => candidate.path === field.path)}
								onCheckedChange={(checked) => onToggleColumn(field.path, checked)}
								aria-label={runtime.i18n.t("collections:showColumn", {
									label: field.admin.label,
								})}
							/>
							<span class="truncate">{field.admin.label}</span>
						</label>
					{/if}
				{/each}
				<label
					class="flex h-9 cursor-pointer items-center gap-2.5 rounded-[3px] px-2 text-[13px] text-foreground-muted hover:bg-control hover:text-foreground-strong"
				>
					<Checkbox
						checked={showID}
						onCheckedChange={onToggleID}
						aria-label={runtime.i18n.t("collections:showIDColumn")}
					/>
					ID
				</label>
				<label
					class="flex h-9 cursor-pointer items-center gap-2.5 rounded-[3px] px-2 text-[13px] text-foreground-muted hover:bg-control hover:text-foreground-strong"
				>
					<Checkbox
						checked={showCreated}
						onCheckedChange={onToggleCreated}
						aria-label={runtime.i18n.t("collections:showCreatedColumn")}
					/>
					{runtime.i18n.t("collections:created")}
				</label>
				<label
					class="flex h-9 cursor-pointer items-center gap-2.5 rounded-[3px] px-2 text-[13px] text-foreground-muted hover:bg-control hover:text-foreground-strong"
				>
					<Checkbox
						checked={showUpdated}
						onCheckedChange={onToggleUpdated}
						aria-label={runtime.i18n.t("collections:showUpdatedColumn")}
					/>
					{runtime.i18n.t("collections:updated")}
				</label>
			</PopoverContent>
		</Popover>

		{#if sessionActive}
			<Popover>
				<PopoverTrigger
					class={buttonVariants({ variant: "outline", class: "h-[31px] gap-1.75 px-3" })}
				>
					<SaveIcon class="size-3" />
					{runtime.i18n.t("collections:savedViews")}
				</PopoverTrigger>
				<PopoverContent align="end" class="w-56 gap-1 p-2">
					<form
						class="flex gap-1"
						onsubmit={(event) => {
							event.preventDefault();
							void savePreset();
						}}
					>
						<Input
							bind:value={presetName}
							aria-label={runtime.i18n.t("collections:savedViewName")}
							placeholder={runtime.i18n.t("collections:viewName")}
							class="h-8 min-w-0"
						/>
						<Button
							type="submit"
							size="sm"
							class="h-8"
							disabled={presetName.trim() === "" || presetsPending}
						>
							{runtime.i18n.t("general:save")}
						</Button>
					</form>
					{#each sortedPresets as preset (preset.name)}
						<div class="flex items-center gap-1">
							<button
								class="min-w-0 flex-1 truncate rounded-[3px] px-2 py-1.5 text-start text-[12.5px] hover:bg-control"
								onclick={() => onApplyPreset(preset)}
							>
								{preset.name}
							</button>
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label={runtime.i18n.t("collections:deleteSavedView", {
									name: preset.name,
								})}
								onclick={() => onDeletePreset(preset.name)}
							>
								<XIcon class="size-3" />
							</Button>
						</div>
					{/each}
				</PopoverContent>
			</Popover>
		{/if}

		<Popover>
			<PopoverTrigger
				class={buttonVariants({ variant: "outline", class: "h-[31px] gap-1.75 px-3" })}
			>
				<SlidersHorizontalIcon class="size-3" />
				{runtime.i18n.t("general:more")}
			</PopoverTrigger>
			<PopoverContent align="end" class="w-64 gap-4 p-3">
				{#if showFolder}
					<div class="grid gap-2">
						<label class="text-[11px] font-medium text-foreground-muted" for="folder-filter">
							{runtime.i18n.t("collections:folder")}
						</label>
						<Select type="single" value={requestedFolder} onValueChange={onSelectFolder}>
							<SelectTrigger
								id="folder-filter"
								size="toolbar"
								aria-label={runtime.i18n.t("collections:folder")}
							>
								{selectedFolderLabel}
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="" label={runtime.i18n.t("collections:allFolders")} />
								{#each sortedFolders as folder (folder.id)}
									<SelectItem value={folder.id} label={folderLabel(folder)} />
								{/each}
							</SelectContent>
						</Select>
						{#if manageFoldersLink !== undefined}{@render manageFoldersLink()}{/if}
					</div>
				{/if}
				{#if showHierarchy}
					<div class="grid gap-2">
						<p class="text-[11px] font-medium text-foreground-muted">
							{runtime.i18n.t("collections:view")}
						</p>
						<div
							class="flex rounded-[3px] border border-control-border bg-background p-0.5"
							aria-label={runtime.i18n.t("collections:collectionView")}
						>
							<Button
								variant="ghost"
								size="sm"
								class="h-7 flex-1"
								aria-pressed={!hierarchy}
								onclick={() => onSelectView("list")}
							>
								{runtime.i18n.t("collections:listView")}
							</Button>
							<Button
								variant="ghost"
								size="sm"
								class="h-7 flex-1"
								aria-pressed={hierarchy}
								onclick={() => onSelectView("hierarchy")}
							>
								<ListTreeIcon class="size-3" />
								{runtime.i18n.t("collections:hierarchy")}
							</Button>
						</div>
					</div>
				{/if}
				<div class="grid gap-2">
					<label class="text-[11px] font-medium text-foreground-muted" for="page-size">
						{runtime.i18n.t("collections:pageSize")}
					</label>
					<Select
						type="single"
						value={String(pageSize)}
						onValueChange={(next) => onSelectPageSize(Number(next) as ListPageSize)}
					>
						<SelectTrigger
							id="page-size"
							size="toolbar"
							aria-label={runtime.i18n.t("collections:documentsPerPage")}
						>
							{runtime.i18n.t("collections:perPage", {
								count: runtime.i18n.formatNumber(pageSize),
							})}
						</SelectTrigger>
						<SelectContent>
							{#each listPageSizes as size}<SelectItem
									value={String(size)}
									label={runtime.i18n.t("collections:perPage", {
										count: runtime.i18n.formatNumber(size),
									})}
								/>{/each}
						</SelectContent>
					</Select>
				</div>
			</PopoverContent>
		</Popover>
	</div>
</div>
