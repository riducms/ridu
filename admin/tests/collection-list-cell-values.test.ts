import { describe, expect, test } from "bun:test";
import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import { createAdminI18n, en } from "@riducms/translations";

import { createCollectionListCellFormatter } from "@admin/features/collections/collection-list-cell-values";

const i18n = createAdminI18n({ languages: [en], language: "en" });
const collections = [
	{
		slug: "authors",
		admin: { useAsTitle: "name" },
		fields: [{ name: "name", path: "name", type: "text" }],
	},
	{ slug: "teams", admin: {}, fields: [] },
] as SchemaCollection[];

function formatter(canReadField = () => true) {
	return createCollectionListCellFormatter({ i18n, collections, canReadField });
}

function field(value: Partial<SchemaField>): SchemaField {
	return { name: "value", path: "value", type: "text", ...value } as SchemaField;
}

describe("collection list cell values", () => {
	test("uses populated relationship titles and falls back to IDs", () => {
		const relationship = field({
			name: "author",
			path: "author",
			type: "relationship",
			relationship: {
				collectionId: "collection-authors",
				collectionSlug: "authors",
				onDelete: "nullify",
			},
		});
		expect(formatter()({ id: "post", author: { id: "author_1", name: "Ada" } }, relationship)).toBe(
			"Ada"
		);
		expect(formatter()({ id: "post", author: { id: "author_1" } }, relationship)).toBe("author_1");
		expect(formatter()({ id: "post", author: "author_1" }, relationship)).toBe("author_1");
	});

	test("formats polymorphic populated and unpopulated references together", () => {
		const relationship = field({
			name: "subjects",
			path: "subjects",
			type: "relationship",
			relationship: {
				hasMany: true,
				polymorphic: true,
				targets: [
					{ collectionId: "collection-authors", collectionSlug: "authors" },
					{ collectionId: "collection-teams", collectionSlug: "teams" },
				],
				onDelete: "nullify",
			},
		});
		const value = [
			{ relationTo: "authors", id: { id: "author_1", name: "Ada" } },
			{ relationTo: "teams", id: "team_1" },
		];
		expect(formatter()({ id: "post", subjects: value }, relationship)).toBe("Ada and team_1");
	});

	test("translates every plural select value", () => {
		const select = field({
			name: "colors",
			path: "colors",
			type: "select",
			select: {
				hasMany: true,
				options: [
					{ value: "red", label: "Red", labelTranslations: { en: "Rouge" } },
					{ value: "blue", label: "Blue", labelTranslations: { en: "Bleu" } },
				],
			},
		});
		expect(formatter()({ id: "post", colors: ["red", "blue"] }, select)).toBe("Rouge and Bleu");
	});

	test("numbers repeated row objects by their mapping position", () => {
		const array = field({ name: "items", path: "items", type: "array" });
		const row = {};
		expect(formatter()({ id: "post", items: [row, row] }, array)).toBe("Row 1 and Row 2");
	});

	test("does not expose a value when field read access is denied", () => {
		const title = field({ name: "title", path: "title" });
		expect(formatter(() => false)({ id: "post", title: "Private" }, title)).toBe("—");
	});
});
