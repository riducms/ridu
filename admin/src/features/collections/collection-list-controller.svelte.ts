import { documentLabel } from "@admin/features/documents/document-title";
import { untrack } from "svelte";
import { RiduError } from "@riducms/sdk";
import type {
	AccessCapabilitiesEnvelope,
	AdminCollectionListDataV1,
	CollectionPageEnvelope,
	Pagination,
	SchemaCollection,
} from "@riducms/protocol";
import type { AdminI18n } from "@riducms/translations";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";

interface CollectionListControllerOptions {
	client: AdminClient;
	notifications: NotificationCenter;
	i18n: AdminI18n;
	get slug(): string;
	get locale(): string | undefined;
	get query(): string;
	get ready(): boolean;
	get prepared(): AdminCollectionListDataV1 | undefined;
	get collection(): SchemaCollection | undefined;
	get trashOnly(): boolean;
}

interface CollectionListRequest {
	slug: string;
	locale: string | undefined;
	query: string;
	trashOnly: boolean;
}

interface CollectionListMutation {
	request: CollectionListRequest;
	collection: SchemaCollection | undefined;
}

export interface CollectionBulkUpdateTarget {
	readonly ids: readonly string[];
	readonly slug: string;
	readonly locale: string | undefined;
	readonly collection: SchemaCollection | undefined;
	readonly accessIdentity: Readonly<Record<string, AccessCapabilitiesEnvelope>>;
}

type CollectionListStatus = "initial" | "loading" | "ready" | "failed";

const emptyPagination: Pagination = {
	page: 1,
	limit: 10,
	totalDocs: 0,
	totalPages: 0,
	hasNextPage: false,
	hasPrevPage: false,
};

const maxAtomicSelection = 100;

export class CollectionListController {
	selectedIDs = $state.raw<Set<string>>(new Set());
	#status = $state<CollectionListStatus>("initial");
	#docs = $state.raw<AdminDocument[]>([]);
	#pagination = $state.raw<Pagination>(emptyPagination);
	#collectionAccess = $state.raw<AccessCapabilitiesEnvelope>();
	#documentAccess = $state.raw<Record<string, AccessCapabilitiesEnvelope>>({});
	#pageError = $state<string>();
	#request?: AbortController;
	#selectionRequest?: AbortController;
	#requestGeneration = 0;
	#mutation?: CollectionListMutation;
	#disposed = false;
	#current?: CollectionListRequest;
	#selectionWhere?: Record<string, unknown>;
	#collection?: SchemaCollection;
	#prepared?: AdminCollectionListDataV1;
	#desiredRequestKey = $state("");
	#loadedRequestKey = $state<string>();
	#loadedSelectionQueryKey?: string;
	bulkPending = $state(false);
	selectionPending = $state(false);

	constructor(readonly options: CollectionListControllerOptions) {
		$effect.pre(() => {
			if (this.#disposed) return;
			const request = this.#requestFromOptions();
			const collection = this.options.collection;
			// Revoke an outgoing mutation even while the next route's preferences are loading.
			if (
				this.#mutation !== undefined &&
				(requestKey(this.#mutation.request) !== requestKey(request) ||
					this.#mutation.collection !== collection)
			)
				this.#invalidateMutation();
			const previous = this.#current;
			const schemaChanged = collection !== this.#collection;
			if (
				previous !== undefined &&
				(schemaChanged || requestKey(previous) !== requestKey(request))
			) {
				this.#requestGeneration += 1;
				this.#request?.abort();
				this.#request = undefined;
				this.#cancelSelectionRequest();
			}
			if (!this.options.ready || request.slug === "") return;
			this.#collection = collection;
			if (schemaChanged) {
				this.clearSelection();
				this.#loadedRequestKey = undefined;
			}
			this.#current = request;
			this.#desiredRequestKey = requestKey(request);
			const prepared = this.options.prepared;
			if (prepared !== undefined && prepared !== this.#prepared) {
				this.#prepared = prepared;
				// Merging selected-row access must not turn selection into a route dependency.
				untrack(() => this.#adoptPrepared(request, prepared));
				return;
			}
			const search = new URLSearchParams(request.query).get("q") ?? "";
			const searchChanged =
				previous !== undefined && new URLSearchParams(previous.query).get("q") !== search;
			const delay = searchChanged && search.trim().length > 0 ? 180 : 0;
			const timer = window.setTimeout(async () => {
				if (this.#loadedRequestKey !== requestKey(request)) await this.#open(request);
			}, delay);
			return () => window.clearTimeout(timer);
		});
		$effect(() => () => this.dispose());
	}

	#requestFromOptions(): CollectionListRequest {
		return {
			slug: this.options.slug,
			locale: this.options.locale,
			query: this.options.query,
			trashOnly: this.options.trashOnly,
		};
	}

	#adoptPrepared(request: CollectionListRequest, data: AdminCollectionListDataV1) {
		this.#pageError = undefined;
		this.#applyListPage(request, data);
	}

	#applyListPage(request: CollectionListRequest, data: AdminCollectionListDataV1) {
		const key = selectionQueryKey(request, data.query);
		if (this.#loadedSelectionQueryKey !== key) this.clearSelection();
		const page = data.page;
		if (page === undefined) throw new Error("Collection list response omitted its page");
		if (page.error !== undefined) {
			this.#failPage(new RiduError(page.error));
		} else {
			this.#selectionWhere = data.query.where;
			this.#applyPage(request, page.value);
			this.#loadedSelectionQueryKey = key;
		}
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

	get canCreate() {
		return this.#collectionAccess?.operations.create === true;
	}

	get canBulkUpdate() {
		return this.#selectedAllow("update");
	}

	canBulkUpdateField = (path: string) =>
		this.canBulkUpdate &&
		[...this.selectedIDs].every((id) => this.#documentAccess[id]?.fields[path]?.update !== false);

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

	canReadField = (path: string, id?: string) => {
		const access = id === undefined ? this.#collectionAccess : this.#documentAccess[id];
		if (access?.operations.read !== true) return false;
		return access.fields[path]?.read !== false;
	};

	get error() {
		return this.#pageError;
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
			this.selectedIDs.size > 0 &&
			this.selectedIDs.size < this.pagination.totalDocs
		);
	}

	get showSelectAllMatches() {
		return (
			this.selectionControlsReady &&
			this.#collectionAccess?.operations.selectAll === true &&
			this.selectedIDs.size > 0 &&
			(this.pagination.totalDocs > this.selectedIDs.size ||
				this.selectedIDs.size > this.docs.length)
		);
	}

	get selectionControlsReady() {
		return this.#desiredRequestKey !== "" && this.#loadedRequestKey === this.#desiredRequestKey;
	}

	get selectionLimitExceeded() {
		return this.pagination.totalDocs > maxAtomicSelection;
	}

	get selectionLimit() {
		return maxAtomicSelection;
	}

	retry = () => (this.#current === undefined ? Promise.resolve() : this.#open(this.#current));

	title = (document: AdminDocument) => documentLabel(this.options.collection, document);

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
		const selectionKey = this.#loadedSelectionQueryKey;
		this.#selectionRequest?.abort();
		const activeRequest = new AbortController();
		this.#selectionRequest = activeRequest;
		this.selectionPending = true;
		try {
			const selection = await this.options.client.resolveFilteredSelection(request.slug, {
				where: this.#selectionWhere,
				trash: request.trashOnly,
				locale: request.locale,
				signal: activeRequest.signal,
			});
			if (
				activeRequest.signal.aborted ||
				this.#current === undefined ||
				selectionKey !== this.#loadedSelectionQueryKey
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

	captureBulkUpdateTarget = (
		ids: readonly string[] = [...this.selectedIDs]
	): CollectionBulkUpdateTarget => ({
		ids: [...ids],
		slug: this.options.slug,
		locale: this.options.locale,
		collection: this.options.collection,
		accessIdentity: this.#documentAccess,
	});

	bulkUpdate = async (target: CollectionBulkUpdateTarget, data: Record<string, unknown>) => {
		if (
			target.slug !== this.options.slug ||
			target.locale !== this.options.locale ||
			target.collection !== this.options.collection ||
			target.accessIdentity !== this.#documentAccess
		)
			return false;
		return this.#bulk("update", data, { ids: target.ids, surfaceIssues: true });
	};
	bulkPublish = () => this.#bulk("publish");
	bulkUnpublish = () => this.#bulk("unpublish");
	bulkDelete = () => this.#bulk("delete");
	bulkRestore = () => this.#bulk("restoreDeleted");
	bulkDeletePermanent = () => this.#bulk("deletePermanent");

	emptyTrash = async () => {
		const mutation = this.#beginMutation();
		if (mutation === undefined) return;
		const { request } = mutation;
		try {
			const documents = await this.options.client.emptyTrash(request.slug, {
				locale: request.locale,
			});
			if (!this.#ownsMutation(mutation)) return;
			this.clearSelection();
			this.options.notifications.success({
				title: this.options.i18n.t("collections:documentsPermanentlyDeleted", {
					count: documents.length,
					formattedCount: this.options.i18n.formatNumber(documents.length),
				}),
			});
			await this.#open(request);
		} catch (cause) {
			if (!this.#ownsMutation(mutation)) return;
			this.options.notifications.error({
				title: this.options.i18n.t("collections:trashNotEmptied"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.i18n.t("collections:noDocumentsChanged"),
			});
		} finally {
			if (this.#mutation === mutation) this.#invalidateMutation();
		}
	};

	dispose() {
		this.#disposed = true;
		this.#invalidateMutation();
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
		if (this.#disposed) return;
		this.#current = request;
		this.#request?.abort();
		const activeRequest = new AbortController();
		const generation = ++this.#requestGeneration;
		this.#request = activeRequest;
		this.#status = "loading";
		this.#pageError = undefined;

		try {
			const data = await this.options.client.adminCollectionList(
				request.slug,
				request.query,
				"page",
				{ signal: activeRequest.signal }
			);
			if (activeRequest.signal.aborted || generation !== this.#requestGeneration) return;
			this.#applyListPage(request, data);
		} catch (cause) {
			if (activeRequest.signal.aborted || generation !== this.#requestGeneration) return;
			this.#failPage(cause);
		} finally {
			if (this.#request === activeRequest) this.#request = undefined;
		}
	};

	#applyPage(request: CollectionListRequest, page: CollectionPageEnvelope<AdminDocument>) {
		this.#docs = page.docs;
		this.#pagination = page.pagination;
		this.#collectionAccess = page.access.collection;
		const visibleIDs = new Set(page.docs.map((document) => document.id));
		this.#documentAccess = {
			...Object.fromEntries(
				Object.entries(this.#documentAccess).filter(
					([id]) => this.selectedIDs.has(id) && !visibleIDs.has(id)
				)
			),
			...page.access.documents,
		};
		this.#loadedRequestKey = requestKey(request);
		this.#status = "ready";
	}

	#failPage(cause: unknown) {
		const visibleIDs = new Set(this.#docs.map((document) => document.id));
		this.#collectionAccess = undefined;
		this.#documentAccess = Object.fromEntries(
			Object.entries(this.#documentAccess).filter(
				([id]) => this.selectedIDs.has(id) && !visibleIDs.has(id)
			)
		);
		this.#pageError =
			cause instanceof Error
				? cause.message
				: this.options.i18n.t("collections:documentsLoadFailed");
		this.#status = "failed";
	}

	#beginMutation() {
		if (this.#disposed || this.bulkPending || !this.options.ready) return;
		const request = this.#requestFromOptions();
		const collection = this.options.collection;
		if (
			this.#current === undefined ||
			requestKey(this.#current) !== requestKey(request) ||
			this.#collection !== collection
		)
			return;
		const mutation = { request, collection };
		this.#mutation = mutation;
		this.bulkPending = true;
		return mutation;
	}

	#ownsMutation(mutation: CollectionListMutation) {
		return (
			!this.#disposed &&
			this.#mutation === mutation &&
			requestKey(mutation.request) === requestKey(this.#requestFromOptions()) &&
			mutation.collection === this.options.collection
		);
	}

	#invalidateMutation() {
		this.#mutation = undefined;
		this.bulkPending = false;
	}

	#selectedAllow(
		operation: "update" | "delete" | "publish" | "unpublish" | "restoreDeleted" | "deletePermanent"
	) {
		return this.#idsAllow([...this.selectedIDs], operation);
	}

	#idsAllow(
		ids: readonly string[],
		operation: "update" | "delete" | "publish" | "unpublish" | "restoreDeleted" | "deletePermanent"
	) {
		return (
			ids.length > 0 && ids.every((id) => this.#documentAccess[id]?.operations[operation] === true)
		);
	}

	async #bulk(
		action: "update" | "publish" | "unpublish" | "delete" | "restoreDeleted" | "deletePermanent",
		data?: Record<string, unknown>,
		options: { ids?: readonly string[]; surfaceIssues?: boolean } = {}
	) {
		const ids = [...(options.ids ?? this.selectedIDs)];
		if (!this.selectionControlsReady || !this.#idsAllow(ids, action)) return false;
		if (
			action === "update" &&
			Object.keys(data ?? {}).some((path) =>
				ids.some((id) => this.#documentAccess[id]?.fields[path]?.update === false)
			)
		)
			return false;
		const mutation = this.#beginMutation();
		if (mutation === undefined) return false;
		const { request } = mutation;
		try {
			if (action === "update")
				await this.options.client.bulkUpdate(request.slug, ids, data ?? {}, {
					locale: request.locale,
				});
			else if (action === "publish")
				await this.options.client.bulkPublish(request.slug, ids, {
					locale: request.locale,
				});
			else if (action === "unpublish")
				await this.options.client.bulkUnpublish(request.slug, ids, {
					locale: request.locale,
				});
			else if (action === "delete")
				await this.options.client.bulkDelete(request.slug, ids, {
					locale: request.locale,
				});
			else if (action === "restoreDeleted")
				await this.options.client.bulkRestoreDeleted(request.slug, ids, {
					locale: request.locale,
				});
			else
				await this.options.client.bulkDeletePermanent(request.slug, ids, {
					locale: request.locale,
				});
			if (!this.#ownsMutation(mutation)) return false;
			this.clearSelection();
			this.options.notifications.success({
				title: this.options.i18n.t(bulkTranslationKey(action), {
					count: ids.length,
					formattedCount: this.options.i18n.formatNumber(ids.length),
				}),
			});
			await this.#open(request);
			return this.#ownsMutation(mutation);
		} catch (cause) {
			if (!this.#ownsMutation(mutation)) return false;
			this.options.notifications.error({
				title: this.options.i18n.t("collections:bulkOperationFailed"),
				message:
					cause instanceof Error
						? cause.message
						: this.options.i18n.t("collections:noDocumentsChanged"),
			});
			if (options.surfaceIssues && cause instanceof RiduError && cause.issues.length > 0)
				throw cause;
			return false;
		} finally {
			if (this.#mutation === mutation) this.#invalidateMutation();
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

function selectionQueryKey(
	request: CollectionListRequest,
	query: AdminCollectionListDataV1["query"]
) {
	return JSON.stringify([request.slug, query.locale, query.trash, query.where]);
}

function requestKey(request: CollectionListRequest) {
	return JSON.stringify(request);
}
