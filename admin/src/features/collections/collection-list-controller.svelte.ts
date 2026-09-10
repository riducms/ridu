import type { AccessCapabilitiesEnvelope, Pagination, SchemaSelectOption } from "@riducms/protocol";
import type { AdminI18n } from "@riducms/translations";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";

interface CollectionListControllerOptions {
	client: AdminClient;
	notifications: NotificationCenter;
	i18n: AdminI18n;
	get slug(): string;
	get locale(): string | undefined;
	get page(): number;
	get search(): string;
	get titleField(): string | undefined;
	get status(): string;
	get statusField(): string | undefined;
	get statusOptions(): readonly SchemaSelectOption[];
	get versioned(): boolean;
	get trashOnly(): boolean;
	get folderField(): string | undefined;
	get folderID(): string;
	get hierarchy(): boolean;
	get limit(): number;
	get sort(): readonly string[];
	get filterWhere(): readonly Where[];
}

interface CollectionListRequest {
	slug: string;
	locale: string | undefined;
	page: number;
	limit: number;
	search: string;
	titleField: string | undefined;
	status: string;
	statusField: string | undefined;
	statusValues: readonly string[];
	trashOnly: boolean;
	folderField: string | undefined;
	folderID: string;
	hierarchy: boolean;
	sort: readonly string[];
	filterWhere: readonly Where[];
}

type CollectionListStatus = "initial" | "loading" | "ready" | "failed";
type Where = Record<string, unknown>;

const emptyPagination: Pagination = {
	page: 1,
	limit: 25,
	totalDocs: 0,
	totalPages: 0,
	hasNextPage: false,
	hasPrevPage: false,
};

const maxAtomicSelection = 100;

export class CollectionListController {
	showStatus = $state(true);
	showUpdated = $state(true);
	selectedIDs = $state.raw<Set<string>>(new Set());
	#status = $state<CollectionListStatus>("initial");
	#docs = $state.raw<AdminDocument[]>([]);
	#pagination = $state.raw<Pagination>(emptyPagination);
	#statusCounts = $state.raw<Record<string, number>>({});
	#collectionAccess = $state.raw<AccessCapabilitiesEnvelope>();
	#documentAccess = $state.raw<Record<string, AccessCapabilitiesEnvelope>>({});
	#error = $state<string>();
	#request?: AbortController;
	#selectionRequest?: AbortController;
	#requestGeneration = 0;
	#current?: CollectionListRequest;
	#desiredRequestKey = $state("");
	#desiredSelectionQueryKey = $state("");
	#loadedRequestKey = $state<string>();
	#loadedSelectionQueryKey = $state<string>();
	mutationPending = $state.raw<Set<string>>(new Set());
	bulkPending = $state(false);
	selectionPending = $state(false);

	constructor(readonly options: CollectionListControllerOptions) {
		$effect(() => {
			const request: CollectionListRequest = {
				slug: this.options.slug,
				locale: this.options.locale,
				page: this.options.hierarchy ? 1 : this.options.page,
				limit: this.options.hierarchy ? 100 : this.options.limit,
				search: this.options.search,
				titleField: this.options.titleField,
				status: this.options.status,
				statusField: this.options.statusField,
				statusValues: this.options.statusOptions.map((option) => option.value),
				trashOnly: this.options.trashOnly,
				folderField: this.options.folderField,
				folderID: this.options.folderID,
				hierarchy: this.options.hierarchy,
				sort: this.options.sort,
				filterWhere: this.options.filterWhere,
			};
			if (request.slug === "") return;
			const previous = this.#current;
			if (previous !== undefined && selectionQueryKey(previous) !== selectionQueryKey(request)) {
				this.clearSelection();
			}
			if (previous !== undefined && requestKey(previous) !== requestKey(request)) {
				this.#requestGeneration += 1;
				this.#request?.abort();
				this.#request = undefined;
			}
			this.#current = request;
			this.#desiredRequestKey = requestKey(request);
			this.#desiredSelectionQueryKey = selectionQueryKey(request);
			const delay = request.search.trim().length > 0 ? 180 : 0;
			const timer = window.setTimeout(() => this.#open(request), delay);
			return () => window.clearTimeout(timer);
		});

		$effect(() => () => this.dispose());
	}

	get status() {
		return this.#status;
	}

	get docs() {
		return this.#docs;
	}

	get pagination() {
		return this.#pagination;
	}

	get statusCounts() {
		return this.#statusCounts;
	}

	get canCreate() {
		return this.#collectionAccess?.operations.create === true;
	}

	get canBulkUpdate() {
		return this.#selectedAllow("update");
	}

	get canBulkPublish() {
		return this.#selectedAllow("publish");
	}

	get canBulkUnpublish() {
		return this.#selectedAllow("unpublish");
	}

	get canBulkDelete() {
		return this.#selectedAllow("delete");
	}

	get canBulkRestore() {
		return this.#selectedAllow("restoreDeleted");
	}

	get canBulkDeletePermanent() {
		return this.#selectedAllow("deletePermanent");
	}

	canRestore = (id: string) => this.#documentAccess[id]?.operations.restoreDeleted === true;

	canDeletePermanent = (id: string) =>
		this.#documentAccess[id]?.operations.deletePermanent === true;

	canReadField = (path: string, id?: string) => {
		const access = id === undefined ? this.#collectionAccess : this.#documentAccess[id];
		if (access?.operations.read !== true) return false;
		return access.fields[path]?.read !== false;
	};

	get error() {
		return this.#error;
	}

	get statusColumnAvailable() {
		return this.options.statusField !== undefined || this.options.versioned;
	}

	get statusVisible() {
		return this.showStatus && this.statusColumnAvailable;
	}

	get allDocuments() {
		return this.options.statusField === undefined
			? this.pagination.totalDocs
			: (this.statusCounts[""] ?? this.pagination.totalDocs);
	}

	get rangeStart() {
		return this.pagination.totalDocs === 0
			? 0
			: (this.pagination.page - 1) * this.pagination.limit + 1;
	}

	get rangeEnd() {
		return Math.min(this.pagination.page * this.pagination.limit, this.pagination.totalDocs);
	}

	get selectedOnPage() {
		return this.docs.reduce(
			(count, document) => count + Number(this.selectedIDs.has(document.id)),
			0
		);
	}

	get allOnPageSelected() {
		return this.docs.length > 0 && this.selectedOnPage === this.docs.length;
	}

	get pageSelectionIndeterminate() {
		return this.selectedOnPage > 0 && this.selectedOnPage < this.docs.length;
	}

	get canSelectAllMatches() {
		return (
			this.selectionControlsReady &&
			this.#collectionAccess?.operations.selectAll === true &&
			this.allOnPageSelected &&
			this.selectedIDs.size < this.pagination.totalDocs
		);
	}

	get showSelectAllMatches() {
		return (
			this.selectionControlsReady &&
			this.#collectionAccess?.operations.selectAll === true &&
			this.allOnPageSelected &&
			this.pagination.totalDocs > this.docs.length
		);
	}

	get selectionControlsReady() {
		return this.#desiredRequestKey !== "" && this.#loadedRequestKey === this.#desiredRequestKey;
	}

	get selectionQueryReady() {
		return (
			this.#desiredSelectionQueryKey !== "" &&
			this.#loadedSelectionQueryKey === this.#desiredSelectionQueryKey
		);
	}

	get selectionLimitExceeded() {
		return this.pagination.totalDocs > maxAtomicSelection;
	}

	retry = async () => {
		if (this.#current !== undefined) await this.#open(this.#current);
	};

	setShowStatus = (show: boolean) => {
		this.showStatus = show;
	};

	setShowUpdated = (show: boolean) => {
		this.showUpdated = show;
	};

	title = (document: AdminDocument) =>
		String(
			(this.options.titleField === undefined ? undefined : document[this.options.titleField]) ??
				document.id
		);

	documentStatus = (document: AdminDocument) => {
		if (typeof document._status === "string") return document._status;
		if (this.options.statusField === undefined) return undefined;
		const value = document[this.options.statusField];
		return typeof value === "string" ? value : undefined;
	};

	statusLabel = (value: string) => {
		const configured = this.options.statusOptions.find((option) => option.value === value)?.label;
		if (configured !== undefined) return configured;
		const normalized = value.toLocaleLowerCase(this.options.i18n.language);
		if (normalized === "published") return this.options.i18n.t("documents:published");
		if (normalized === "draft") return this.options.i18n.t("documents:draft");
		return humanize(value, this.options.i18n.language);
	};

	statusTone = (value: string) => {
		const normalized = value.toLocaleLowerCase(this.options.i18n.language);
		if (normalized === "published" || normalized === "live") return "live";
		if (normalized === "draft") return "muted";
		return "warning";
	};

	updated = (document: AdminDocument) => formatUpdated(document.updatedAt, this.options.i18n);
	created = (document: AdminDocument) => formatUpdated(document.createdAt, this.options.i18n);

	optionCount = (option: SchemaSelectOption) => this.statusCounts[option.value];

	toggleDocument = (id: string, checked: boolean) => {
		if (!this.selectionControlsReady || !this.docs.some((document) => document.id === id)) return;
		this.#cancelSelectionRequest();
		const selected = new Set(this.selectedIDs);
		if (checked) selected.add(id);
		else selected.delete(id);
		this.selectedIDs = selected;
	};

	togglePage = (checked: boolean) => {
		if (!this.selectionControlsReady) return;
		this.#cancelSelectionRequest();
		const selected = new Set(this.selectedIDs);
		for (const document of this.docs) {
			if (checked) selected.add(document.id);
			else selected.delete(document.id);
		}
		this.selectedIDs = selected;
	};

	clearSelection = () => {
		this.#cancelSelectionRequest();
		this.selectedIDs = new Set();
	};

	selectAllMatches = async () => {
		const request = this.#current;
		if (
			request === undefined ||
			!this.canSelectAllMatches ||
			this.selectionLimitExceeded ||
			this.selectionPending
		)
			return;
		const selectionKey = selectionQueryKey(request);
		this.#selectionRequest?.abort();
		const activeRequest = new AbortController();
		this.#selectionRequest = activeRequest;
		this.selectionPending = true;
		try {
			const selection = await this.options.client.resolveFilteredSelection(request.slug, {
				where: buildWhere(request),
				trash: request.trashOnly,
				locale: request.locale,
				signal: activeRequest.signal,
			});
			if (
				activeRequest.signal.aborted ||
				this.#current === undefined ||
				selectionKey !== selectionQueryKey(this.#current)
			)
				return;
			this.selectedIDs = new Set(selection.items.map((item) => item.id));
			this.#documentAccess = Object.fromEntries(
				selection.items.map((item) => [item.id, item.access] as const)
			);
		} catch (cause) {
			if (activeRequest.signal.aborted) return;
			this.options.notifications.error({
				title: this.options.i18n.t("collections:allDocumentsNotSelected"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.i18n.t("collections:filteredResultLoadFailed"),
			});
		} finally {
			if (this.#selectionRequest === activeRequest) {
				this.#selectionRequest = undefined;
				this.selectionPending = false;
			}
		}
	};

	restore = async (id: string) => {
		await this.#mutate(id, "restore");
	};

	deletePermanent = async (id: string) => {
		await this.#mutate(id, "permanent");
	};

	bulkUpdate = async (data: Record<string, unknown>) => {
		return this.#bulk("update", data);
	};

	bulkPublish = async () => {
		await this.#bulk("publish");
	};

	bulkUnpublish = async () => {
		await this.#bulk("unpublish");
	};

	bulkDelete = async () => {
		await this.#bulk("delete");
	};

	bulkRestore = async () => {
		await this.#bulk("restoreDeleted");
	};

	bulkDeletePermanent = async () => {
		await this.#bulk("deletePermanent");
	};

	emptyTrash = async () => {
		if (this.bulkPending) return;
		this.bulkPending = true;
		try {
			const documents = await this.options.client.emptyTrash(this.options.slug);
			this.clearSelection();
			this.options.notifications.success({
				title: this.options.i18n.t("collections:documentsPermanentlyDeleted", {
					count: documents.length,
					formattedCount: this.options.i18n.formatNumber(documents.length),
				}),
			});
			if (this.#current !== undefined) await this.#open(this.#current);
		} catch (cause) {
			this.options.notifications.error({
				title: this.options.i18n.t("collections:trashNotEmptied"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.i18n.t("collections:noDocumentsChanged"),
			});
		} finally {
			this.bulkPending = false;
		}
	};

	dispose() {
		this.#requestGeneration += 1;
		this.#request?.abort();
		this.#request = undefined;
		this.#cancelSelectionRequest();
	}

	#cancelSelectionRequest() {
		this.#selectionRequest?.abort();
		this.#selectionRequest = undefined;
		this.selectionPending = false;
	}

	#open = async (request: CollectionListRequest) => {
		if (
			this.#current !== undefined &&
			selectionQueryKey(this.#current) !== selectionQueryKey(request)
		) {
			this.clearSelection();
		}
		this.#current = request;
		this.#request?.abort();
		const activeRequest = new AbortController();
		const generation = ++this.#requestGeneration;
		this.#request = activeRequest;
		this.#status = "loading";
		this.#error = undefined;

		try {
			const [page, statusCounts, collectionAccess] = await Promise.all([
				this.options.client.list(request.slug, {
					page: request.page,
					limit: request.limit,
					where: buildWhere(request),
					sort: request.sort,
					trash: request.trashOnly,
					locale: request.locale,
					signal: activeRequest.signal,
				}),
				this.#loadStatusCounts(request, activeRequest.signal),
				this.options.client.collectionAccess(request.slug, {
					trash: request.trashOnly,
					locale: request.locale,
					signal: activeRequest.signal,
				}),
			]);
			const documentAccessEntries = await Promise.all(
				page.docs.map(async (document) => {
					try {
						return [
							document.id,
							await this.options.client.collectionAccess(request.slug, {
								id: document.id,
								trash: request.trashOnly,
								locale: request.locale,
								signal: activeRequest.signal,
							}),
						] as const;
					} catch {
						return undefined;
					}
				})
			);
			if (activeRequest.signal.aborted || generation !== this.#requestGeneration) return;
			this.#docs = page.docs;
			this.#pagination = page.pagination;
			this.#statusCounts = statusCounts;
			this.#collectionAccess = collectionAccess;
			const visibleAccess = Object.fromEntries(
				documentAccessEntries.filter((entry) => entry !== undefined)
			);
			this.#documentAccess = {
				...Object.fromEntries(
					Object.entries(this.#documentAccess).filter(([id]) => this.selectedIDs.has(id))
				),
				...visibleAccess,
			};
			this.#loadedRequestKey = requestKey(request);
			this.#loadedSelectionQueryKey = selectionQueryKey(request);
			this.#status = "ready";
		} catch (cause) {
			if (activeRequest.signal.aborted || generation !== this.#requestGeneration) return;
			this.#error =
				cause instanceof Error
					? cause.message
					: this.options.i18n.t("collections:documentsLoadFailed");
			this.#status = "failed";
		} finally {
			if (this.#request === activeRequest) this.#request = undefined;
		}
	};

	async #loadStatusCounts(request: CollectionListRequest, signal: AbortSignal) {
		if (request.statusField === undefined || request.statusValues.length === 0) return {};
		const entries = await Promise.all([
			this.options.client
				.count(request.slug, {
					where: buildWhere(request, ""),
					trash: request.trashOnly,
					locale: request.locale,
					signal,
				})
				.then((result) => ["", result.totalDocs] as const),
			...request.statusValues.map(async (status) => {
				const result = await this.options.client.count(request.slug, {
					where: buildWhere(request, status),
					trash: request.trashOnly,
					locale: request.locale,
					signal,
				});
				return [status, result.totalDocs] as const;
			}),
		]);
		return Object.fromEntries(entries);
	}

	#selectedAllow(
		operation: "update" | "delete" | "publish" | "unpublish" | "restoreDeleted" | "deletePermanent"
	) {
		return (
			this.selectedIDs.size > 0 &&
			[...this.selectedIDs].every((id) => this.#documentAccess[id]?.operations[operation] === true)
		);
	}

	async #mutate(id: string, action: "restore" | "permanent") {
		if (this.mutationPending.has(id)) return;
		this.mutationPending = new Set(this.mutationPending).add(id);
		try {
			if (action === "restore") await this.options.client.restoreDeleted(this.options.slug, id);
			else await this.options.client.deletePermanent(this.options.slug, id);
			this.options.notifications.success({
				title:
					action === "restore"
						? this.options.i18n.t("collections:documentRestored")
						: this.options.i18n.t("collections:documentPermanentlyDeleted"),
			});
			if (this.#current !== undefined) await this.#open(this.#current);
		} catch (cause) {
			this.options.notifications.error({
				title:
					action === "restore"
						? this.options.i18n.t("collections:documentNotRestored")
						: this.options.i18n.t("collections:documentNotDeleted"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.i18n.t("collections:operationFailed"),
			});
		} finally {
			const pending = new Set(this.mutationPending);
			pending.delete(id);
			this.mutationPending = pending;
		}
	}

	async #bulk(
		action: "update" | "publish" | "unpublish" | "delete" | "restoreDeleted" | "deletePermanent",
		data?: Record<string, unknown>
	) {
		const ids = [...this.selectedIDs];
		if (ids.length === 0 || this.bulkPending || !this.selectionQueryReady) return false;
		this.bulkPending = true;
		try {
			if (action === "update")
				await this.options.client.bulkUpdate(this.options.slug, ids, data ?? {}, {
					locale: this.options.locale,
				});
			else if (action === "publish")
				await this.options.client.bulkPublish(this.options.slug, ids, {
					locale: this.options.locale,
				});
			else if (action === "unpublish")
				await this.options.client.bulkUnpublish(this.options.slug, ids, {
					locale: this.options.locale,
				});
			else if (action === "delete")
				await this.options.client.bulkDelete(this.options.slug, ids, {
					locale: this.options.locale,
				});
			else if (action === "restoreDeleted")
				await this.options.client.bulkRestoreDeleted(this.options.slug, ids, {
					locale: this.options.locale,
				});
			else
				await this.options.client.bulkDeletePermanent(this.options.slug, ids, {
					locale: this.options.locale,
				});
			this.clearSelection();
			this.options.notifications.success({
				title: this.options.i18n.t(bulkTranslationKey(action), {
					count: ids.length,
					formattedCount: this.options.i18n.formatNumber(ids.length),
				}),
			});
			if (this.#current !== undefined) await this.#open(this.#current);
			return true;
		} catch (cause) {
			this.options.notifications.error({
				title: this.options.i18n.t("collections:bulkOperationFailed"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.i18n.t("collections:noDocumentsChanged"),
			});
			return false;
		} finally {
			this.bulkPending = false;
		}
	}
}

function bulkTranslationKey(
	action: "update" | "publish" | "unpublish" | "delete" | "restoreDeleted" | "deletePermanent"
) {
	if (action === "update") return "collections:documentsUpdated" as const;
	if (action === "publish") return "collections:documentsPublished" as const;
	if (action === "unpublish") return "collections:documentsUnpublished" as const;
	if (action === "restoreDeleted") return "collections:documentsRestored" as const;
	if (action === "deletePermanent") return "collections:documentsPermanentlyDeleted" as const;
	return "collections:documentsDeleted" as const;
}

function buildWhere(request: CollectionListRequest, status = request.status) {
	const filters: Where[] = [];
	const search = request.search.trim();
	if (search.length > 0 && request.titleField !== undefined) {
		filters.push({ [request.titleField]: { like: search } });
	}
	if (status !== "" && request.statusField !== undefined) {
		filters.push({ [request.statusField]: { equals: status } });
	}
	if (request.folderID !== "" && request.folderField !== undefined) {
		filters.push({ [request.folderField]: { equals: request.folderID } });
	}
	filters.push(...request.filterWhere);
	if (filters.length === 0) return undefined;
	if (filters.length === 1) return filters[0];
	return { and: filters };
}

function selectionQueryKey(request: CollectionListRequest) {
	return JSON.stringify({
		slug: request.slug,
		locale: request.locale,
		search: request.search,
		titleField: request.titleField,
		status: request.status,
		statusField: request.statusField,
		trashOnly: request.trashOnly,
		folderField: request.folderField,
		folderID: request.folderID,
		hierarchy: request.hierarchy,
		sort: request.sort,
		filterWhere: request.filterWhere,
	});
}

function requestKey(request: CollectionListRequest) {
	return JSON.stringify(request);
}

function humanize(value: string, language: string) {
	return value
		.replaceAll(/[_-]+/g, " ")
		.replace(/\b\w/g, (letter) => letter.toLocaleUpperCase(language));
}

function formatUpdated(value: string | undefined, i18n: AdminI18n) {
	if (value === undefined) return "—";
	const date = new Date(value);
	if (Number.isNaN(date.valueOf())) return value;
	const seconds = Math.round((date.valueOf() - Date.now()) / 1_000);
	if (Math.abs(seconds) < 60) return i18n.formatRelativeTime(seconds, "second");
	const minutes = Math.round(seconds / 60);
	if (Math.abs(minutes) < 60) return i18n.formatRelativeTime(minutes, "minute");
	const hours = Math.round(minutes / 60);
	if (Math.abs(hours) < 24) return i18n.formatRelativeTime(hours, "hour");
	const days = Math.round(hours / 24);
	if (Math.abs(days) < 7) return i18n.formatRelativeTime(days, "day");
	return i18n.formatDate(date, {
		day: "2-digit",
		month: "short",
		year: date.getFullYear() === new Date().getFullYear() ? undefined : "numeric",
	});
}
