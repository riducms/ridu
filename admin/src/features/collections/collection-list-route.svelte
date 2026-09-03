<script lang="ts">
	import { Link, useLocation, useNavigate, useParams } from "@hvniel/svelte-router";
	import type { SchemaCollection, SchemaField } from "@riducms/protocol";
	import FolderIcon from "~icons/lucide/folder";
	import RotateCcwIcon from "~icons/lucide/rotate-ccw";
	import Trash2Icon from "~icons/lucide/trash-2";
	import UploadCloudIcon from "~icons/lucide/upload-cloud";

	import { Banner } from "@admin/components/ui/banner";
	import { Button, buttonVariants } from "@admin/components/ui/button";
	import { ConfirmationDialog } from "@admin/components/ui/confirmation-dialog";
	import {
		collectionPath,
		collectionTrashPath,
		collectionUploadPath,
		createDocumentPath,
		documentPath,
	} from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import type { AdminDocument } from "@admin/core/api/admin-client";
	import { preferenceOwnerID } from "@admin/core/preferences/preference-write-queue";

	import { CollectionListController } from "@admin/features/collections/collection-list-controller.svelte";
	import CollectionListBulkActions from "@admin/features/collections/collection-list-bulk-actions.svelte";
	import CollectionListBulkEditor from "@admin/features/collections/collection-list-bulk-editor.svelte";
	import {
		CollectionListPreferencesController,
		type CollectionListPreset,
	} from "@admin/features/collections/collection-list-preferences-controller.svelte";
	import CollectionListResults from "@admin/features/collections/collection-list-results.svelte";
	import CollectionListWorkspaceToolbar from "@admin/features/collections/collection-list-workspace-toolbar.svelte";
	import {
		buildListFilterWhere,
		defaultListColumns,
		filterableFields,
		listColumnFields,
		listPageSizes,
		parseListFilters,
		parseListPageSize,
		visibleColumnNames,
		type ListFilter,
		type ListPageSize,
	} from "@admin/features/collections/list-workspace";
	import { formatDateDisplay } from "@admin/fields/scalar/date-value";

	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const navigate = useNavigate();
	const params = $derived(useParams<"collection">());
	const location = $derived(useLocation());
	const slug = $derived(params.collection ?? "");
	const trashOnly = $derived(location.pathname.endsWith("/trash"));
	const collection = $derived(runtime.manifest?.collections.find((item) => item.slug === slug));
	const titleField = $derived(
		collection?.fields.find((field) => field.name === collection.admin.useAsTitle) ??
			collection?.fields.find((field) => field.type === "text") ??
			collection?.fields.find((field) => field.type === "select")
	);
	const searchLabel = $derived(
		titleField === undefined
			? runtime.i18n.t("collections:searchDocuments")
			: runtime.i18n.t("collections:searchBy", { label: titleField.admin.label })
	);
	const statusField = $derived(
		collection?.fields.find((field) => field.type === "select" && field.name === "status")
	);
	const statusChoices = $derived(statusField?.select?.choices ?? []);
	const searchParams = $derived(new URLSearchParams(location.search));
	const localization = $derived(runtime.manifest?.application.localization);
	const requestedLocale = $derived(searchParams.get("locale"));
	const contentLocale = $derived(
		localization?.locales.some((locale) => locale.code === requestedLocale)
			? (requestedLocale ?? localization?.defaultLocale)
			: (runtime.contentLocale ?? localization?.defaultLocale)
	);
	const query = $derived(searchParams.get("q") ?? "");
	const requestedPage = $derived(parsePage(searchParams.get("page")));
	const requestedStatus = $derived(searchParams.get("status") ?? "");
	const requestedFolder = $derived(searchParams.get("folder") ?? "");
	const hierarchy = $derived(searchParams.get("view") === "hierarchy");
	const requestedSort = $derived(searchParams.get("sort") ?? "");
	const folderField = $derived(
		collection?.fields.find((field) => field.name === collection.admin.folderField)
	);
	const parentField = $derived(
		collection?.fields.find((field) => field.name === collection.admin.parentField)
	);
	const folderCollection = $derived(
		runtime.manifest?.collections.find(
			(candidate) => candidate.slug === folderField?.relationship?.collectionSlug
		)
	);
	let folders = $state.raw<AdminDocument[]>([]);
	let referenceLabels = $state.raw<Record<string, string>>({});
	let searchInput = $state<HTMLInputElement | null>(null);
	const activeStatus = $derived(
		statusChoices.some((choice) => choice.value === requestedStatus) ? requestedStatus : ""
	);
	const bulkEditableFields = $derived(
		collection?.fields.filter(
			(field) =>
				field.category === "scalar" &&
				field.localized !== true &&
				field.admin.readOnly !== true &&
				["text", "textarea", "email", "date", "number", "checkbox", "select", "radio"].includes(
					field.type
				)
		) ?? []
	);
	const customListCells = $derived(
		runtime.listCells.flatMap((cell) => {
			if (cell.collection !== slug) return [];
			const field = collection?.fields.find(
				(candidate) => candidate.path === cell.field || candidate.name === cell.field
			);
			return field === undefined ? [] : [{ ...cell, field }];
		})
	);
	const availableColumnFields = $derived(
		listColumnFields(collection, titleField?.name).filter(
			(field) =>
				field.path !== statusField?.path &&
				!customListCells.some((cell) => cell.field.path === field.path)
		)
	);
	const availableFilterFields = $derived(filterableFields(collection?.fields ?? []));
	const defaultColumns = $derived(defaultListColumns(collection, availableColumnFields));
	const preferences = new CollectionListPreferencesController(
		runtime.client,
		notifications,
		() => runtime.session?.id ?? "",
		runtime.i18n
	);
	const { workspace, presets, presetsPending } = $derived(preferences);
	const visibleColumns = $derived(
		visibleColumnNames(searchParams.get("columns"), workspace.columns).flatMap((name) => {
			const field = availableColumnFields.find((candidate) => candidate.path === name);
			return field === undefined ? [] : [field];
		})
	);
	const pageSize = $derived(parseListPageSize(searchParams.get("limit"), workspace.limit));
	const showID = $derived(workspace.showID);
	const showCreated = $derived(workspace.showCreated);
	const listFilters = $derived(
		parseListFilters(searchParams.get("filters"), availableFilterFields)
	);
	const filterWhere = $derived(buildListFilterWhere(listFilters, availableFilterFields));
	const sortField = $derived(requestedSort.replace(/^-/, ""));
	const sortDescending = $derived(requestedSort.startsWith("-"));
	let bulkEditOpen = $state(false);
	type ConfirmationAction =
		| { kind: "delete-document"; id: string }
		| { kind: "delete-selected" }
		| { kind: "restore-selected" }
		| { kind: "delete-selected-permanently" }
		| { kind: "empty-trash" };
	let confirmationOpen = $state(false);
	let confirmationAction = $state.raw<ConfirmationAction>();
	const confirmationCopy = $derived(confirmationText(confirmationAction));
	const controller = new CollectionListController({
		client: runtime.client,
		notifications,
		i18n: runtime.i18n,
		get slug() {
			return slug;
		},
		get locale() {
			return contentLocale;
		},
		get page() {
			return requestedPage;
		},
		get search() {
			return query;
		},
		get titleField() {
			return titleField?.name;
		},
		get status() {
			return activeStatus;
		},
		get statusField() {
			return statusField?.name;
		},
		get statusChoices() {
			return statusChoices;
		},
		get versioned() {
			return collection?.capabilities.versions === true;
		},
		get trashOnly() {
			return trashOnly;
		},
		get folderField() {
			return folderField?.name;
		},
		get folderID() {
			return requestedFolder;
		},
		get hierarchy() {
			return hierarchy;
		},
		get limit() {
			return pageSize;
		},
		get sort() {
			return requestedSort === "" ? [] : [requestedSort];
		},
		get filterWhere() {
			return filterWhere;
		},
	});
	const {
		allDocuments,
		canCreate,
		docs,
		error,
		pagination,
		selectedIDs,
		bulkPending,
		showStatus,
		showUpdated,
		statusColumnAvailable,
	} = $derived(controller);
	const {
		choiceCount,
		canReadField,
		deletePermanent,
		bulkDelete,
		bulkDeletePermanent,
		bulkRestore,
		emptyTrash,
		bulkUpdate,
		retry,
		setShowStatus,
		setShowUpdated,
		title,
	} = controller;

	$effect(() => {
		if (contentLocale === undefined || requestedLocale === contentLocale) return;
		const next = new URLSearchParams(location.search);
		next.set("locale", contentLocale);
		updateSearch(next, true);
	});

	$effect(() =>
		runtime.registerContentLocaleBlocker(
			() =>
				controller.bulkPending || controller.selectionPending || controller.mutationPending.size > 0
		)
	);

	$effect(() => {
		preferences.enter({
			slug,
			sessionID: runtime.session?.id ?? "",
			preferenceOwnerID: preferenceOwnerID(runtime.session),
			columnFields: availableColumnFields,
			filterFields: availableFilterFields,
			defaultColumns,
		});
	});

	$effect(() => {
		setShowStatus(workspace.showStatus);
		setShowUpdated(workspace.showUpdated);
	});

	$effect(() => () => preferences.dispose());

	$effect(() => {
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

	$effect(() => {
		const documents = docs;
		const fields = visibleColumns.filter(referenceField);
		const collections = runtime.manifest?.collections ?? [];
		if (documents.length === 0 || fields.length === 0) {
			referenceLabels = {};
			return;
		}
		const lookups = new Map<string, { collection: SchemaCollection; id: string }>();
		for (const document of documents) {
			for (const field of fields) {
				for (const reference of fieldReferences(field, documentPathValue(document, field.path))) {
					const target = collections.find((candidate) => candidate.slug === reference.collection);
					if (target !== undefined) {
						lookups.set(referenceKey(reference.collection, reference.id), {
							collection: target,
							id: reference.id,
						});
					}
				}
			}
		}
		const request = new AbortController();
		Promise.all(
			[...lookups].map(async ([key, lookup]) => {
				try {
					const document = await runtime.client.find(lookup.collection.slug, lookup.id, {
						signal: request.signal,
						locale: contentLocale,
					});
					return [key, referenceTitle(lookup.collection, document)] as const;
				} catch {
					return [key, lookup.id] as const;
				}
			})
		).then((entries) => {
			if (!request.signal.aborted) referenceLabels = Object.fromEntries(entries);
		});
		return () => request.abort();
	});

	function parsePage(value: string | null) {
		const page = Number(value);
		return Number.isInteger(page) && page > 0 ? page : 1;
	}

	function contentPath(path: string) {
		if (contentLocale === undefined) return path;
		return `${path}?${new URLSearchParams({ locale: contentLocale }).toString()}`;
	}

	function updateSearch(next: URLSearchParams, replace: boolean) {
		navigate(
			{ pathname: location.pathname, search: next.size === 0 ? "" : `?${next}` },
			{ replace }
		);
	}

	function handleSearch(value: string) {
		const next = new URLSearchParams(searchParams);
		if (value.trim().length > 0) next.set("q", value);
		else next.delete("q");
		next.delete("page");
		updateSearch(next, true);
	}

	function clearSearch() {
		const next = new URLSearchParams(searchParams);
		next.delete("q");
		next.delete("page");
		updateSearch(next, true);
	}

	function clearFilters() {
		const next = new URLSearchParams(searchParams);
		next.delete("q");
		next.delete("status");
		next.delete("page");
		next.delete("folder");
		next.delete("filters");
		updateSearch(next, true);
	}

	function clearFiltersAndFocusSearch() {
		clearFilters();
		searchInput?.focus();
	}

	function updateListFilters(filters: readonly ListFilter[]) {
		const next = new URLSearchParams(searchParams);
		if (filters.length === 0) next.delete("filters");
		else next.set("filters", JSON.stringify(filters));
		next.delete("page");
		updateSearch(next, false);
	}

	function toggleSort(name: string) {
		const next = new URLSearchParams(searchParams);
		if (sortField !== name) next.set("sort", name);
		else if (!sortDescending) next.set("sort", `-${name}`);
		else next.delete("sort");
		next.delete("page");
		updateSearch(next, false);
	}

	function selectPageSize(limit: ListPageSize) {
		const next = new URLSearchParams(searchParams);
		next.set("limit", String(limit));
		next.delete("page");
		updateSearch(next, false);
		void preferences.persistWorkspace({ ...workspace, limit });
	}

	function toggleColumn(name: string, show: boolean) {
		// Workspace state is updated synchronously by persistWorkspace. Using it as
		// the source prevents two quick checkbox changes from racing a router update.
		const nextNames = new Set(workspace.columns);
		if (show) nextNames.add(name);
		else nextNames.delete(name);
		const columns = availableColumnFields
			.map((field) => field.path)
			.filter((candidate) => nextNames.has(candidate));
		const next = new URLSearchParams(searchParams);
		if (columns.length === 0) next.set("columns", "");
		else next.set("columns", columns.join(","));
		updateSearch(next, true);
		void preferences.persistWorkspace({ ...workspace, columns });
	}

	function toggleStatusColumn(show: boolean) {
		void preferences.persistWorkspace({ ...workspace, showStatus: show });
	}

	function toggleUpdatedColumn(show: boolean) {
		void preferences.persistWorkspace({ ...workspace, showUpdated: show });
	}

	function toggleIDColumn(show: boolean) {
		void preferences.persistWorkspace({ ...workspace, showID: show });
	}

	function toggleCreatedColumn(show: boolean) {
		void preferences.persistWorkspace({ ...workspace, showCreated: show });
	}

	function selectFolder(folder: string) {
		const next = new URLSearchParams(searchParams);
		if (folder === "") next.delete("folder");
		else next.set("folder", folder);
		next.delete("page");
		updateSearch(next, false);
	}

	function selectView(view: "list" | "hierarchy") {
		const next = new URLSearchParams(searchParams);
		if (view === "list") next.delete("view");
		else next.set("view", view);
		next.delete("page");
		updateSearch(next, false);
	}

	function savePreset(name: string) {
		return preferences.savePreset(name, {
			q: query,
			status: activeStatus,
			folder: requestedFolder,
			view: hierarchy ? "hierarchy" : "list",
			filters: listFilters,
			sort: requestedSort,
			columns: visibleColumns.map((field) => field.path),
			limit: pageSize,
			showStatus,
			showID,
			showCreated,
			showUpdated,
		});
	}

	function applyPreset(preset: CollectionListPreset) {
		const next = new URLSearchParams();
		if (contentLocale !== undefined) next.set("locale", contentLocale);
		if (preset.q !== "") next.set("q", preset.q);
		if (preset.status !== "") next.set("status", preset.status);
		if (preset.folder !== "") next.set("folder", preset.folder);
		if (preset.view === "hierarchy") next.set("view", "hierarchy");
		if (preset.filters.length > 0) next.set("filters", JSON.stringify(preset.filters));
		if (preset.sort !== "") next.set("sort", preset.sort);
		if (preset.columns.length > 0) next.set("columns", preset.columns.join(","));
		next.set("limit", String(preset.limit));
		void preferences.persistWorkspace({
			columns: preset.columns,
			limit: preset.limit,
			showStatus: preset.showStatus,
			showID: preset.showID,
			showCreated: preset.showCreated,
			showUpdated: preset.showUpdated,
		});
		updateSearch(next, false);
	}

	function selectStatus(status: string) {
		const next = new URLSearchParams(searchParams);
		if (status === "") next.delete("status");
		else next.set("status", status);
		next.delete("page");
		updateSearch(next, false);
	}

	function selectPage(page: number) {
		const next = new URLSearchParams(searchParams);
		if (page <= 1) next.delete("page");
		else next.set("page", String(page));
		updateSearch(next, false);
	}

	function columnValue(document: AdminDocument, field: SchemaField) {
		if (!canReadField(field.path, document.id)) return "—";
		const value = documentPathValue(document, field.path);
		if (value === undefined || value === null || value === "") return "—";
		if (field.type === "checkbox") {
			return value === true ? runtime.i18n.t("general:yes") : runtime.i18n.t("general:no");
		}
		if (field.type === "select" || field.type === "radio") {
			return field.select?.choices.find((choice) => choice.value === value)?.label ?? String(value);
		}
		if (field.type === "array") return summarizeRows(value, "row");
		if (field.type === "blocks") return summarizeRows(value, "block");
		if (field.type === "json" || field.plugin !== undefined) return summarizeStructured(value);
		if (field.type === "date")
			return formatDateDisplay(value, field.date?.pickerAppearance, runtime.i18n);
		if (referenceField(field)) {
			return runtime.i18n.formatList(
				fieldReferences(field, value).map(
					(reference) =>
						referenceLabels[referenceKey(reference.collection, reference.id)] ?? reference.id
				)
			);
		}
		if (Array.isArray(value)) return runtime.i18n.formatList(value.map(displayReference));
		if (typeof value === "object") return displayReference(value);
		return String(value);
	}

	function referenceField(field: SchemaField) {
		return (
			field.relationship !== undefined || field.upload !== undefined || field.join !== undefined
		);
	}

	function fieldReferences(field: SchemaField, value: unknown) {
		const values = Array.isArray(value) ? value : [value];
		return values.flatMap((candidate): { collection: string; id: string }[] => {
			if (typeof candidate === "string") {
				const target =
					field.relationship?.collectionSlug ??
					field.upload?.collectionSlug ??
					field.join?.collectionSlug;
				return target === undefined ? [] : [{ collection: target, id: candidate }];
			}
			if (typeof candidate !== "object" || candidate === null) return [];
			const record = candidate as Record<string, unknown>;
			const id = typeof record.id === "string" ? record.id : undefined;
			const target =
				typeof record.relationTo === "string"
					? record.relationTo
					: (field.relationship?.collectionSlug ??
						field.upload?.collectionSlug ??
						field.join?.collectionSlug);
			return id === undefined || target === undefined ? [] : [{ collection: target, id }];
		});
	}

	function referenceKey(collection: string, id: string) {
		return `${collection}:${id}`;
	}

	function referenceTitle(collection: SchemaCollection, document: AdminDocument) {
		const titleField =
			collection.fields.find((field) => field.name === collection.admin.useAsTitle) ??
			collection.fields.find((field) => ["text", "email", "select", "radio"].includes(field.type));
		const value =
			titleField === undefined ? undefined : documentPathValue(document, titleField.path);
		return value === undefined || value === null || value === "" ? document.id : String(value);
	}

	function displayReference(value: unknown) {
		if (typeof value === "string") return value;
		if (typeof value !== "object" || value === null) return String(value ?? "—");
		const record = value as Record<string, unknown>;
		return String(
			record.title ??
				record.name ??
				record.email ??
				record.id ??
				runtime.i18n.t("collections:relatedDocument")
		);
	}

	function documentPathValue(document: AdminDocument, path: string) {
		let value: unknown = document;
		for (const segment of path.split(".")) {
			if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
			value = Reflect.get(value, segment);
		}
		return value;
	}

	function summarizeRows(value: unknown, singular: "row" | "block") {
		if (!Array.isArray(value) || value.length === 0) return "—";
		const previews = value.slice(0, 2).map((row) => {
			if (typeof row !== "object" || row === null) return String(row);
			const record = row as Record<string, unknown>;
			return String(
				record.label ??
					record.title ??
					record.heading ??
					record.name ??
					record.blockType ??
					runtime.i18n.t(singular === "row" ? "collections:rowNumber" : "collections:blockNumber", {
						number: runtime.i18n.formatNumber(value.indexOf(row) + 1),
					})
			);
		});
		const summary = runtime.i18n.formatList(previews);
		return value.length > previews.length
			? runtime.i18n.t("collections:moreItems", {
					summary,
					count: runtime.i18n.formatNumber(value.length - previews.length),
				})
			: summary;
	}

	function summarizeStructured(value: unknown) {
		if (value === undefined || value === null) return "—";
		if (typeof value !== "object") return String(value);
		const text = JSON.stringify(value);
		return text.length > 72 ? `${text.slice(0, 69)}…` : text;
	}

	function requestConfirmation(action: ConfirmationAction) {
		confirmationAction = action;
		confirmationOpen = true;
	}

	function openBulkEdit() {
		bulkEditOpen = true;
	}

	function applyBulkEdit(field: SchemaField, value: unknown) {
		return bulkUpdate({ [field.name]: value });
	}

	async function performConfirmation() {
		const action = confirmationAction;
		if (action === undefined) return;
		if (action.kind === "delete-document") await deletePermanent(action.id);
		else if (action.kind === "delete-selected") await bulkDelete();
		else if (action.kind === "restore-selected") await bulkRestore();
		else if (action.kind === "delete-selected-permanently") await bulkDeletePermanent();
		else await emptyTrash();
	}

	function confirmationText(action: ConfirmationAction | undefined) {
		const count = selectedIDs.size;
		if (action?.kind === "delete-document") {
			return {
				title: runtime.i18n.t("collections:confirmDeleteDocumentPermanently"),
				description: runtime.i18n.t("collections:cannotUndo"),
				confirmLabel: runtime.i18n.t("collections:deletePermanently"),
				destructive: true,
			};
		}
		if (action?.kind === "restore-selected") {
			return {
				title: runtime.i18n.t("collections:confirmRestoreSelected", {
					count,
					formattedCount: runtime.i18n.formatNumber(count),
				}),
				description: runtime.i18n.t("collections:restoreSelectedDescription"),
				confirmLabel: runtime.i18n.t("documents:restore"),
				destructive: false,
			};
		}
		if (action?.kind === "delete-selected-permanently") {
			return {
				title: runtime.i18n.t("collections:confirmDeleteSelectedPermanently", {
					count,
					formattedCount: runtime.i18n.formatNumber(count),
				}),
				description: runtime.i18n.t("collections:cannotUndo"),
				confirmLabel: runtime.i18n.t("collections:deletePermanently"),
				destructive: true,
			};
		}
		if (action?.kind === "empty-trash") {
			const trashCount = pagination.totalDocs;
			return {
				title: runtime.i18n.t("collections:confirmEmptyTrash", {
					label:
						collection?.labels.plural.toLocaleLowerCase(runtime.i18n.language) ??
						runtime.i18n.t("collections:documents"),
				}),
				description: runtime.i18n.t("collections:emptyTrashDescription", {
					count: trashCount,
					formattedCount: runtime.i18n.formatNumber(trashCount),
				}),
				confirmLabel: runtime.i18n.t("collections:emptyTrash"),
				destructive: true,
			};
		}
		return {
			title: runtime.i18n.t("collections:confirmDeleteSelected", {
				count,
				formattedCount: runtime.i18n.formatNumber(count),
			}),
			description: runtime.i18n.t("collections:deleteSelectedDescription"),
			confirmLabel: runtime.i18n.t("general:delete"),
			destructive: true,
		};
	}
</script>

<section class="w-full px-5 py-7 sm:px-8 sm:py-8 lg:px-10 lg:pb-20">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<div>
			<div class="flex flex-wrap items-center gap-3">
				<h1
					class="text-[28px] font-semibold leading-tight tracking-[-0.02em] text-foreground-strong"
				>
					{collection?.labels.plural ?? slug}
				</h1>
				{#if !trashOnly && canCreate}
					<Link class={buttonVariants()} to={contentPath(createDocumentPath(slug))}>
						{runtime.i18n.t("general:create")}
					</Link>
				{/if}
			</div>
			<p class="mt-1.5 text-[12.5px] text-foreground-muted">
				{runtime.i18n.t("collections:documentCount", {
					count: allDocuments,
					formattedCount: runtime.i18n.formatNumber(allDocuments),
				})}
				{#if collection?.capabilities.versions === true}
					<span aria-hidden="true">·</span>
					{runtime.i18n.t("collections:draftsAndVersions")}
				{/if}
			</p>
		</div>
		<div class="flex items-center gap-2">
			{#if !trashOnly && canCreate && collection?.capabilities.upload === true}
				<Link
					class={buttonVariants({ variant: "outline" })}
					to={contentPath(collectionUploadPath(slug))}
				>
					<UploadCloudIcon class="size-3.5" />
					{runtime.i18n.t("collections:bulkUpload")}
				</Link>
			{/if}
			{#if collection?.capabilities.trash === true}
				{#if trashOnly}
					<Button
						variant="destructive"
						disabled={pagination.totalDocs === 0 || bulkPending}
						onclick={() => requestConfirmation({ kind: "empty-trash" })}
					>
						{runtime.i18n.t("collections:emptyTrash")}
					</Button>
				{/if}
				<Link
					class={buttonVariants({ variant: "outline" })}
					to={contentPath(trashOnly ? collectionPath(slug) : collectionTrashPath(slug))}
				>
					{#if trashOnly}<RotateCcwIcon class="size-3.5" />
						{runtime.i18n.t("collections:backToCollection")}{:else}<Trash2Icon class="size-3.5" />
						{runtime.i18n.t("collections:trash")}{/if}
				</Link>
			{/if}
		</div>
	</header>

	{#if statusChoices.length > 0 && !trashOnly}
		<div
			class="mt-6 flex h-9 w-full items-center justify-start gap-5 border-b border-control-border"
			role="group"
			aria-label={runtime.i18n.t("collections:statusFilters")}
		>
			<button
				type="button"
				class="relative h-9 flex-none cursor-pointer px-px pb-2.5 text-[13px] text-foreground-muted outline-none after:absolute after:inset-x-0 after:bottom-[-1px] after:h-0.5 after:bg-primary after:opacity-0 after:transition-opacity hover:text-foreground-strong focus-visible:outline-2 focus-visible:outline-primary/60 focus-visible:outline-offset-2 aria-pressed:text-foreground-strong aria-pressed:after:opacity-100"
				aria-pressed={activeStatus === ""}
				onclick={() => selectStatus("")}
			>
				{runtime.i18n.t("collections:all")}
				<span class="font-mono ms-1 text-[9.5px] text-foreground-faint">
					{runtime.i18n.formatNumber(allDocuments)}
				</span>
			</button>
			{#each statusChoices as choice (choice.value)}
				{const statusCount = $derived(choiceCount(choice))}
				<button
					type="button"
					class="relative h-9 flex-none cursor-pointer px-px pb-2.5 text-[13px] text-foreground-muted outline-none after:absolute after:inset-x-0 after:bottom-[-1px] after:h-0.5 after:bg-primary after:opacity-0 after:transition-opacity hover:text-foreground-strong focus-visible:outline-2 focus-visible:outline-primary/60 focus-visible:outline-offset-2 aria-pressed:text-foreground-strong aria-pressed:after:opacity-100"
					aria-pressed={activeStatus === choice.value}
					onclick={() => selectStatus(choice.value)}
				>
					{choice.label}
					{#if statusCount !== undefined}
						<span class="font-mono ms-1 text-[9.5px] text-foreground-faint">
							{runtime.i18n.formatNumber(statusCount)}
						</span>
					{/if}
				</button>
			{/each}
		</div>
	{/if}

	{#if error !== undefined}
		<Banner class="mt-6" tone="destructive">
			<span class="size-2 shrink-0 rounded-full bg-destructive" aria-hidden="true"></span>
			<span class="min-w-0 flex-1">{error}</span>
			<Button variant="outline" size="sm" onclick={retry}>
				{runtime.i18n.t("general:retry")}
			</Button>
		</Banner>
	{/if}

	{#snippet manageFoldersLink()}
		{#if folderCollection !== undefined}
			<Link
				class={buttonVariants({ variant: "outline", class: "h-[31px] gap-1.75 px-3" })}
				to={contentPath(collectionPath(folderCollection.slug))}
			>
				<FolderIcon class="size-3" />
				{runtime.i18n.t("collections:manageFolders")}
			</Link>
		{/if}
	{/snippet}

	<CollectionListWorkspaceToolbar
		{slug}
		{query}
		{searchLabel}
		bind:searchInput
		filterFields={availableFilterFields}
		filters={listFilters}
		{canReadField}
		{folders}
		{requestedFolder}
		manageFoldersLink={folderCollection === undefined ? undefined : manageFoldersLink}
		showFolder={folderField !== undefined && !trashOnly}
		showHierarchy={parentField !== undefined && !trashOnly}
		{hierarchy}
		sessionActive={runtime.session !== undefined && !trashOnly}
		{presets}
		{presetsPending}
		{statusColumnAvailable}
		{availableColumnFields}
		{visibleColumns}
		{showStatus}
		{showID}
		{showCreated}
		{showUpdated}
		{pageSize}
		onSearch={handleSearch}
		onClearSearch={clearSearch}
		onFiltersChange={updateListFilters}
		onSelectFolder={selectFolder}
		onSelectView={selectView}
		onSavePreset={savePreset}
		onApplyPreset={applyPreset}
		onDeletePreset={preferences.deletePreset}
		onToggleColumn={toggleColumn}
		onToggleStatus={toggleStatusColumn}
		onToggleID={toggleIDColumn}
		onToggleCreated={toggleCreatedColumn}
		onToggleUpdated={toggleUpdatedColumn}
		onSelectPageSize={selectPageSize}
	/>

	<CollectionListResults
		i18n={runtime.i18n}
		{controller}
		resourceKey={slug}
		{collection}
		{titleField}
		{customListCells}
		{visibleColumns}
		{showID}
		{showCreated}
		{showUpdated}
		parentFieldName={parentField?.name}
		{hierarchy}
		{trashOnly}
		filtered={query !== "" ||
			activeStatus !== "" ||
			requestedFolder !== "" ||
			listFilters.length > 0}
		{sortField}
		{sortDescending}
		{columnValue}
		onClearFilters={clearFiltersAndFocusSearch}
		onToggleSort={toggleSort}
		onSelectPage={selectPage}
		onRequestDeletePermanent={(id) => requestConfirmation({ kind: "delete-document", id })}
	>
		{#snippet documentLink(document)}
			<Link class="block max-w-[600px]" to={contentPath(documentPath(slug, document.id))}>
				<span
					class="block truncate text-[14px] font-medium text-foreground-strong transition-colors group-hover:text-primary"
				>
					{title(document)}
				</span>
			</Link>
		{/snippet}
		{#snippet createAction()}
			<Link
				class={buttonVariants({ variant: "outline" })}
				to={contentPath(createDocumentPath(slug))}
			>
				{runtime.i18n.t("collections:createNew", {
					label:
						collection?.labels.singular.toLocaleLowerCase(runtime.i18n.language) ??
						runtime.i18n.t("collections:document"),
				})}
			</Link>
		{/snippet}
	</CollectionListResults>
</section>

<CollectionListBulkActions
	i18n={runtime.i18n}
	{controller}
	{trashOnly}
	versioned={collection?.capabilities.versions === true}
	hasBulkEditableFields={bulkEditableFields.length > 0}
	onEdit={openBulkEdit}
	onRequestDelete={() => requestConfirmation({ kind: "delete-selected" })}
	onRequestRestore={() => requestConfirmation({ kind: "restore-selected" })}
	onRequestDeletePermanent={() => requestConfirmation({ kind: "delete-selected-permanently" })}
	onOpenDocument={(id) => navigate(contentPath(documentPath(slug, id)))}
/>

<ConfirmationDialog
	bind:open={confirmationOpen}
	title={confirmationCopy.title}
	description={confirmationCopy.description}
	confirmLabel={confirmationCopy.confirmLabel}
	cancelLabel={runtime.i18n.t("general:cancel")}
	destructive={confirmationCopy.destructive}
	disabled={bulkPending}
	onconfirm={performConfirmation}
/>

<CollectionListBulkEditor
	bind:open={bulkEditOpen}
	selectedCount={selectedIDs.size}
	fields={bulkEditableFields}
	pending={bulkPending}
	onApply={applyBulkEdit}
/>
