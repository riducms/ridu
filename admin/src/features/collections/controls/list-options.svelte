<script lang="ts">
	import { Button, Input, buttonVariants } from "@riducms/ui";
	import ListTreeIcon from "~icons/lucide/list-tree";
	import XIcon from "@admin/components/icons/x.svelte";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import type { AdminDocument } from "@admin/core/api/admin-client";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { Link } from "@hvniel/svelte-router";
	import { getCollectionList } from "@admin/features/collections/collection-list.svelte";

	const list = getCollectionList();
	const runtime = getAdminRuntime();
	const { preferences, query: listQuery } = list;

	const { view, folderCollection, contentLocale, showFolder, showHierarchy } = $derived(list);
	const { presets, presetsPending } = $derived(preferences);
	const sessionActive = $derived(runtime.session !== undefined);

	let moreOpen = $state(false);
	let folders = $state.raw<AdminDocument[]>([]);

	$effect(() => {
		if (!moreOpen) return;

		const target = folderCollection;
		const revision = runtime.documentRevision;
		if (target === undefined || revision < 0) {
			folders = [];
			return;
		}

		const request = new AbortController();
		runtime.client
			.list(target.slug, { limit: 100, locale: contentLocale, signal: request.signal })
			.then((page) => {
				if (!request.signal.aborted) folders = page.docs;
			})
			.catch(() => {
				if (!request.signal.aborted) folders = [];
			});

		return () => request.abort();
	});

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

	const selectedFolder = $derived(folders.find((folder) => folder.id === view.folder));
	const selectedFolderLabel = $derived(
		view.folder === ""
			? runtime.i18n.t("collections:allFolders")
			: selectedFolder
				? folderLabel(selectedFolder)
				: view.folder
	);

	function folderLabel(folder: AdminDocument) {
		return String(folder.name ?? folder.title ?? folder.id);
	}

	async function savePreset() {
		if (await list.saveView(presetName)) presetName = "";
	}
</script>

<Popover bind:open={moreOpen}>
	<PopoverTrigger
		class="ridu-list__pill ridu-list__more"
		aria-label={runtime.i18n.t("general:more")}
	>
		···
	</PopoverTrigger>

	<PopoverContent align="end" class="ridu-list__options">
		{#if showFolder}
			<div class="grid gap-2">
				<label class="text-[11px] font-medium text-foreground-muted" for="folder-filter">
					{runtime.i18n.t("collections:folder")}
				</label>
				<Select type="single" value={view.folder} onValueChange={listQuery.selectFolder}>
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
				{#if list.foldersHref !== undefined}
					<Link
						class={buttonVariants({ variant: "outline", class: "h-[31px] gap-1.75 px-3" })}
						to={list.foldersHref}
					>
						{runtime.i18n.t("collections:manageFolders")}
					</Link>
				{/if}
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
						aria-pressed={view.view === "list"}
						onclick={() => listQuery.selectView("list")}
					>
						{runtime.i18n.t("collections:listView")}
					</Button>
					<Button
						variant="ghost"
						size="sm"
						class="h-7 flex-1"
						aria-pressed={view.view === "hierarchy"}
						onclick={() => listQuery.selectView("hierarchy")}
					>
						<ListTreeIcon class="size-3" />
						{runtime.i18n.t("collections:hierarchy")}
					</Button>
				</div>
			</div>
		{/if}

		{#if sessionActive}
			<p>{runtime.i18n.t("collections:savedViews")}</p>
			<form
				class="flex gap-1"
				onsubmit={(event) => {
					event.preventDefault();
					savePreset();
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
						onclick={() => list.applySavedView(preset)}
					>
						{preset.name}
					</button>
					<Button
						variant="ghost"
						size="icon-xs"
						aria-label={runtime.i18n.t("collections:deleteSavedView", {
							name: preset.name,
						})}
						onclick={() => preferences.deletePreset(preset.name)}
					>
						<XIcon class="size-3" />
					</Button>
				</div>
			{/each}
		{/if}
	</PopoverContent>
</Popover>
