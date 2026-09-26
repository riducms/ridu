import { describe, expect, it } from "bun:test";
import type { SchemaField } from "@riducms/protocol";
import { CollectionListQuery } from "@admin/features/collections/collection-list-query";

const titleField = {
	id: "title",
	name: "title",
	path: "title",
	type: "text",
	category: "scalar",
	required: false,
	unique: false,
	admin: { label: "Title" },
} as SchemaField;

function fixture(search: string) {
	let current = new URLSearchParams(search);
	const navigations: { search: string; replace?: boolean; preventScrollReset?: boolean }[] = [];
	const query = new CollectionListQuery(
		{
			get current() {
				return current;
			},
		},
		(next, options) => {
			if (!(next instanceof URLSearchParams)) throw new Error("Expected search parameters");
			current = next;
			navigations.push({ search: next.toString(), ...options });
		},
		() => undefined
	);
	return {
		query,
		navigations,
		get params() {
			return current;
		},
		visit(search: string) {
			current = new URLSearchParams(search);
		},
	};
}

describe("collection list URL behavior", () => {
	it("uses normalized runtime locale for reads, including unlocalized applications", () => {
		const { query } = fixture("locale=removed&q=hello");
		const options = { limit: 10 as const, columns: ["title"], locale: undefined, trash: false };
		expect(new URLSearchParams(query.readQuery(options)).has("locale")).toBe(false);
		expect(new URLSearchParams(query.readQuery({ ...options, locale: "fr" })).get("locale")).toBe(
			"fr"
		);
	});
	it("reads subsequent navigation instead of retaining the initial query", () => {
		const { query, visit } = fixture("q=first&page=2&sort=-title&locale=en");
		expect([query.search, query.page, query.sortField, query.sortDescending, query.locale]).toEqual(
			["first", 2, "title", true, "en"]
		);
		visit("q=second&page=0&folder=folder-2&view=hierarchy&locale=fr");
		expect([query.search, query.page, query.folder, query.hierarchy, query.locale]).toEqual([
			"second",
			1,
			"folder-2",
			true,
			"fr",
		]);
	});

	it("replaces search history while preserving unrelated filters and clearing pagination", () => {
		const state = fixture("locale=fr&status=draft&page=8&columns=title");
		state.query.handleSearch("  news  ");
		expect(Object.fromEntries(state.params)).toEqual({
			locale: "fr",
			status: "draft",
			columns: "title",
			q: "  news  ",
		});
		expect(state.navigations[0]).toMatchObject({ replace: true, preventScrollReset: true });
		state.query.handleSearch("  ");
		expect(state.params.has("q")).toBe(false);
	});

	it("sets directional sort with navigable history and keeps workspace choices when clearing filters", () => {
		const state = fixture(
			"locale=en&q=news&status=draft&folder=one&filters=[]&page=3&view=hierarchy&limit=50"
		);
		for (const sort of ["title", "-title"]) {
			state.query.setSort("title", sort.startsWith("-"));
			expect(state.params.get("sort")).toBe(sort);
			expect(state.params.has("page")).toBe(false);
			expect(state.navigations.at(-1)).toMatchObject({ replace: false, preventScrollReset: false });
		}
		state.query.clearFilters();
		expect(Object.fromEntries(state.params)).toEqual({
			locale: "en",
			view: "hierarchy",
			limit: "50",
			sort: "-title",
		});
	});

	it("admits sorting only for queryable sortable fields in the active schema", () => {
		const state = fixture("sort=-title");
		expect(state.query.sortFor([titleField])).toBe("-title");

		state.visit("sort=privateTitle");
		expect(
			state.query.sortFor([
				{
					...titleField,
					id: "private-title",
					name: "privateTitle",
					path: "privateTitle",
					queryRestricted: true,
				},
			])
		).toBe("");

		state.visit("sort=metadata");
		expect(
			state.query.sortFor([
				{ ...titleField, id: "metadata", name: "metadata", path: "metadata", type: "json" },
			])
		).toBe("");

		state.visit("sort=missing");
		expect(state.query.sortFor([titleField])).toBe("");
	});

	it("keeps an explicit empty column selection distinct from workspace defaults", () => {
		const state = fixture("page=4&locale=fr");
		state.query.setColumns([]);
		expect(state.params.has("columns")).toBe(true);
		expect(state.params.get("columns")).toBe("");
		expect(state.params.get("page")).toBe("4");
		state.query.selectPageSize(100);
		expect(state.query.pageSize(25)).toBe(100);
		expect(state.query.page).toBe(1);
		expect(state.query.locale).toBe("fr");
	});
});
