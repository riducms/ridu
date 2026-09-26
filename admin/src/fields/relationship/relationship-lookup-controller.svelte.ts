import type { CollectionPageAccess, Pagination } from "@riducms/protocol";
import type { AdminI18n } from "@riducms/plugin";
import { createAdminI18n } from "@riducms/translations";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";

import {
	buildRelationshipWhere,
	type RelationshipFilter,
} from "@admin/fields/relationship/relationship-query";

export interface RelationshipLookupRequest {
	slug: string;
	page: number;
	limit: number;
	search: string;
	searchField: string | undefined;
	filter: RelationshipFilter;
	locale?: string;
	sort?: string;
	where?: Record<string, unknown>;
	includeAccess?: boolean;
	depth?: number;
	/** Fill quick-picker suggestions without counting already selected documents. */
	excludeIDs?: readonly string[];
}

type RelationshipLookupStatus = "initial" | "loading" | "ready" | "failed";

const emptyPagination: Pagination = {
	page: 1,
	limit: 10,
	totalDocs: 0,
	totalPages: 0,
	hasNextPage: false,
	hasPrevPage: false,
};

export class RelationshipLookupController {
	#status = $state<RelationshipLookupStatus>("initial");
	#docs = $state.raw<AdminDocument[]>([]);
	#known = $state.raw<Record<string, AdminDocument>>({});
	#pagination = $state.raw<Pagination>(emptyPagination);
	#error = $state<string>();
	#access = $state.raw<CollectionPageAccess>();
	#request?: AbortController;

	constructor(
		readonly client: AdminClient,
		readonly i18n: AdminI18n = createAdminI18n()
	) {}

	get status() {
		return this.#status;
	}

	get docs() {
		return this.#docs;
	}

	get pagination() {
		return this.#pagination;
	}

	get error() {
		return this.#error;
	}

	canReadField = (path: string, id?: string) => {
		const access = id === undefined ? this.#access?.collection : this.#access?.documents[id];
		return access?.operations.read === true && access.fields[path]?.read !== false;
	};

	document(id: string) {
		return this.#known[id];
	}

	remember(document: AdminDocument) {
		this.#known = { ...this.#known, [document.id]: document };
		this.#docs = this.#docs.map((candidate) =>
			candidate.id === document.id ? document : candidate
		);
	}

	search = async (request: RelationshipLookupRequest) => {
		this.#request?.abort();
		const activeRequest = new AbortController();
		this.#request = activeRequest;
		this.#status = "loading";
		this.#error = undefined;

		try {
			const referenceWhere = buildRelationshipWhere(
				request.searchField,
				request.search,
				request.filter
			);
			const predicates = [referenceWhere, request.where].filter((where) => where !== undefined);
			const excluded = new Set(request.excludeIDs);
			const options = {
				limit: excluded.size > 0 ? Math.min(100, request.limit + excluded.size) : request.limit,
				where: predicates.length > 1 ? { and: predicates } : predicates[0],
				sort: request.sort ? [request.sort] : undefined,
				depth: request.depth,
				includeAccess: request.includeAccess,
				signal: activeRequest.signal,
				locale: request.locale,
			};
			let nextPage = request.page;
			let page;
			const docs: AdminDocument[] = [];

			// Page through bounded requests instead of sending an unbounded exclusion query.
			do {
				page = await this.client.list(request.slug, { ...options, page: nextPage++ });
				if (activeRequest.signal.aborted) return;
				docs.push(...page.docs.filter((document) => !excluded.has(document.id)));
			} while (excluded.size > 0 && docs.length < request.limit && page.pagination.hasNextPage);

			this.#docs = excluded.size > 0 ? docs.slice(0, request.limit) : docs;
			this.#known = {
				...this.#known,
				...Object.fromEntries(this.#docs.map((document) => [document.id, document])),
			};
			this.#pagination = page.pagination;
			this.#access = "access" in page ? page.access : undefined;
			this.#status = "ready";
		} catch (cause) {
			if (activeRequest.signal.aborted) return;
			this.#error =
				cause instanceof Error ? cause.message : this.i18n.t("errors:relationshipDocumentsLoad");
			this.#status = "failed";
			this.#access = undefined;
		} finally {
			if (this.#request === activeRequest) this.#request = undefined;
		}
	};

	cancelSearch() {
		this.#request?.abort();
		this.#request = undefined;
	}

	reset() {
		this.cancelSearch();
		this.#status = "initial";
		this.#docs = [];
		this.#known = {};
		this.#pagination = emptyPagination;
		this.#access = undefined;
		this.#error = undefined;
	}

	dispose() {
		this.cancelSearch();
	}
}
