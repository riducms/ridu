import type { CollectionListPreset } from "@admin/features/collections/collection-list-preferences-controller.svelte";
import type { AdminCollectionListDataV1, SchemaCollection, SchemaField } from "@riducms/protocol";
import {
	listColumnFields,
	parseListFilters,
	parseListPageSize,
	sortableField,
	normalizeColumnSelection,
	encodeColumnSelection,
	type ListColumnSelection,
	type ListFilterFieldLookup,
	type ListFilterGroup,
	type ListPageSize,
} from "@admin/features/collections/list-workspace";

/**
 * Whether a page was read for every value column and extension field a list read query names.
 * The server resolves a navigation's columns from the saved layout, which can differ from the
 * mounted list's own, for example after another tab saved a different one.
 */
export function listPageCovers(
	query: string,
	read: AdminCollectionListDataV1["query"],
	collection: SchemaCollection | undefined
) {
	const params = new URLSearchParams(query);
	const names = (key: string) => (params.get(key) ?? "").split(",").filter(Boolean);
	// Metadata columns are always returned, so only value columns need reading.
	const valueColumns = new Set(listColumnFields(collection).map((field) => field.path));
	const columns = new Set(read.columns);
	const fields = new Set(read.fields);
	return (
		names("columns").every((path) => !valueColumns.has(path) || columns.has(path)) &&
		names("fields").every((name) => fields.has(name))
	);
}

/** Reads and updates the collection URL. The router remains the source of truth. */
export class CollectionListQuery {
	constructor(
		private readonly params: { readonly current: URLSearchParams },
		private readonly setParams: (
			next: URLSearchParams,
			options: { replace: boolean; preventScrollReset: boolean }
		) => void | Promise<void>,
		private readonly pendingSearch: () => string | undefined
	) {}

	get locale() {
		return this.params.current.get("locale");
	}

	readQuery(options: {
		limit: ListPageSize;
		columns: string[];
		cells: string[];
		fields: string[];
		locale: string | undefined;
		trash: boolean;
	}) {
		const query = new URLSearchParams(this.params.current);
		query.set("limit", String(options.limit));
		query.set("columns", options.columns.join(","));
		for (const key of ["cells", "fields"] as const) {
			if (options[key].length === 0) query.delete(key);
			else query.set(key, options[key].join(","));
		}
		if (options.locale === undefined) query.delete("locale");
		else query.set("locale", options.locale);
		query.set("trash", String(options.trash));
		query.sort();
		return query.toString();
	}

	get search() {
		return this.params.current.get("q") ?? "";
	}

	get page() {
		return parsePage(this.params.current.get("page"));
	}

	get status() {
		return this.params.current.get("status") ?? "";
	}

	get folder() {
		return this.params.current.get("folder") ?? "";
	}

	get hierarchy() {
		return this.params.current.get("view") === "hierarchy";
	}

	get sort() {
		return this.params.current.get("sort") ?? "";
	}

	get sortField() {
		return this.sort.replace(/^-/, "");
	}

	get sortDescending() {
		return this.sort.startsWith("-");
	}

	sortFor(fields: readonly SchemaField[]) {
		if (["id", "createdAt", "updatedAt"].includes(this.sortField)) return this.sort;
		const field = fields.find((candidate) => candidate.path === this.sortField);
		return field !== undefined && sortableField(field) ? this.sort : "";
	}

	filters(fields: ListFilterFieldLookup) {
		return parseListFilters(this.params.current.get("filters"), fields);
	}

	pageSize(fallback: ListPageSize) {
		return parseListPageSize(this.params.current.get("limit"), fallback);
	}

	columnSelection(fields: readonly { path: string }[], fallback: readonly ListColumnSelection[]) {
		return parseColumns(this.params.current.get("columns"), fields, fallback);
	}

	columnsForPicker(fields: readonly { path: string }[], fallback: readonly ListColumnSelection[]) {
		return parseColumns(this.#nextParams().get("columns"), fields, fallback);
	}

	handleSearch = (value: string) => {
		const next = this.#nextParams();
		if (value.trim().length > 0) next.set("q", value);
		else next.delete("q");
		next.delete("page");
		this.#updateSearch(next, true);
	};

	clearSearch = () => this.handleSearch("");

	clearFilters = () => {
		const next = this.#nextParams();
		next.delete("q");
		next.delete("status");
		next.delete("page");
		next.delete("folder");
		next.delete("filters");
		return this.#updateSearch(next, true);
	};

	updateListFilters = (filters: readonly ListFilterGroup[]) => {
		const next = this.#nextParams();
		if (filters.length === 0) next.delete("filters");
		else next.set("filters", JSON.stringify(filters));
		next.delete("page");
		return this.#updateSearch(next, false);
	};

	setSort = (name: string, descending: boolean) => {
		const next = this.#nextParams();
		next.set("sort", descending ? `-${name}` : name);
		next.delete("page");
		return this.#updateSearch(next, false);
	};

	selectFolder = (folder: string) => {
		const next = this.#nextParams();
		if (folder === "") next.delete("folder");
		else next.set("folder", folder);
		next.delete("page");
		this.#updateSearch(next, false);
	};

	selectView = (view: "list" | "hierarchy") => {
		const next = this.#nextParams();
		if (view === "list") next.delete("view");
		else next.set("view", view);
		next.delete("page");
		this.#updateSearch(next, false);
	};

	selectStatus = (status: string) => {
		const next = this.#nextParams();
		if (status === "") next.delete("status");
		else next.set("status", status);
		next.delete("page");
		this.#updateSearch(next, false);
	};

	selectPage = (page: number) => {
		const next = this.#nextParams();
		if (page <= 1) next.delete("page");
		else next.set("page", String(page));
		this.#updateSearch(next, false);
	};

	selectPageSize = (limit: ListPageSize) => {
		const next = this.#nextParams();
		next.set("limit", String(limit));
		next.delete("page");
		this.#updateSearch(next, false);
	};

	setColumns = (columns: readonly ListColumnSelection[]) => {
		const next = this.#nextParams();
		next.set("columns", encodeColumnSelection(columns));
		this.#updateSearch(next, true);
	};

	applyPreset = (preset: CollectionListPreset, locale: string | undefined) => {
		const next = new URLSearchParams();
		if (locale !== undefined) next.set("locale", locale);
		if (preset.q !== "") next.set("q", preset.q);
		if (preset.status !== "") next.set("status", preset.status);
		if (preset.folder !== "") next.set("folder", preset.folder);
		if (preset.view === "hierarchy") next.set("view", "hierarchy");
		if (preset.filters.length > 0) next.set("filters", JSON.stringify(preset.filters));
		if (preset.sort !== "") next.set("sort", preset.sort);
		next.set("columns", encodeColumnSelection(preset.columns));
		next.set("limit", String(preset.limit));
		this.#updateSearch(next, false);
	};

	#updateSearch(next: URLSearchParams, replace: boolean) {
		return this.setParams(next, { replace, preventScrollReset: replace });
	}

	#nextParams() {
		// Compose rapid edits with the router's pending destination until it commits.
		return new URLSearchParams(this.pendingSearch() ?? this.params.current);
	}
}

function parsePage(value: string | null) {
	const page = Number(value);
	return Number.isInteger(page) && page > 0 ? page : 1;
}

function parseColumns(
	encoded: string | null,
	fields: readonly { path: string }[],
	fallback: readonly ListColumnSelection[]
) {
	const value =
		encoded === null
			? fallback
			: encoded
					.split(",")
					.filter(Boolean)
					.map((name) => ({
						path: name.replace(/^-/, ""),
						active: !name.startsWith("-"),
					}));
	return normalizeColumnSelection(value, fields, fallback);
}
