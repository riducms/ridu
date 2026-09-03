import type { Pagination } from "@riducms/protocol";
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
	#request?: AbortController;
	#requestGeneration = 0;

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
		const generation = ++this.#requestGeneration;
		this.#request = activeRequest;
		this.#status = "loading";
		this.#error = undefined;

		try {
			const page = await this.client.list(request.slug, {
				page: request.page,
				limit: request.limit,
				where: buildRelationshipWhere(request.searchField, request.search, request.filter),
				signal: activeRequest.signal,
				locale: request.locale,
			});
			if (activeRequest.signal.aborted || generation !== this.#requestGeneration) return;
			this.#docs = page.docs;
			this.#known = {
				...this.#known,
				...Object.fromEntries(page.docs.map((document) => [document.id, document])),
			};
			this.#pagination = page.pagination;
			this.#status = "ready";
		} catch (cause) {
			if (activeRequest.signal.aborted || generation !== this.#requestGeneration) return;
			this.#error =
				cause instanceof Error ? cause.message : this.i18n.t("errors:relationshipDocumentsLoad");
			this.#status = "failed";
		} finally {
			if (this.#request === activeRequest) this.#request = undefined;
		}
	};

	loadDocument = async (slug: string, id: string, locale?: string, signal?: AbortSignal) => {
		const known = this.#known[id];
		if (known !== undefined) return known;
		try {
			const document = await this.client.find(slug, id, { signal, locale });
			if (signal?.aborted) return undefined;
			this.remember(document);
			return document;
		} catch (cause) {
			if (signal?.aborted) return undefined;
			this.#error =
				cause instanceof Error ? cause.message : this.i18n.t("errors:relatedDocumentLoad");
			return undefined;
		}
	};

	dispose() {
		this.#requestGeneration += 1;
		this.#request?.abort();
		this.#request = undefined;
	}
}
