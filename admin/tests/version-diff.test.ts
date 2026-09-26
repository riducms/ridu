import { describe, expect, test } from "bun:test";
import type { SchemaCollection } from "@riducms/protocol";
import { createAdminI18n, en } from "@riducms/translations";

import type { AdminVersion } from "@admin/core/api/admin-client";
import {
	formatVersionValue,
	versionDiffRows,
	versionTextDiff,
	sameValue,
} from "@admin/features/versions/version-diff";

const i18n = createAdminI18n({ languages: [en], language: "en" });

const collection = {
	fields: [
		{ name: "title", path: "title", type: "text", admin: { label: "Title" } },
		{
			name: "seo",
			path: "seo",
			type: "group",
			admin: { label: "SEO" },
			nested: {
				fields: [
					{
						name: "description",
						path: "seo.description",
						type: "textarea",
						admin: { label: "SEO description" },
					},
				],
			},
		},
		{
			name: "computed",
			path: "computed",
			type: "virtual",
			admin: { label: "Computed" },
			virtual: { valueType: "string" },
		},
	],
} as SchemaCollection;

function version(revision: number, title: string, description: string): AdminVersion {
	return {
		ID: `version_${revision}`,
		DocumentID: "post_1",
		Revision: revision,
		Status: revision === 2 ? "published" : "draft",
		Snapshot: { id: "post_1", title, seo: { description } },
		CreatedAt: "2030-01-01T00:00:00Z",
	};
}

describe("version comparison", () => {
	test("compares readable schema fields and nested paths", () => {
		const rows = versionDiffRows(
			collection,
			version(2, "Current", "Same"),
			version(1, "Old", "Same"),
			i18n
		);
		expect(rows.map((row) => row.path)).toEqual(["title", "seo", "_status"]);
		expect(rows.filter((row) => row.changed).map((row) => row.path)).toEqual(["title", "_status"]);
	});

	test("formats scalar and structured values for the comparison table", () => {
		expect(formatVersionValue(undefined, i18n)).toBe("");
		expect(formatVersionValue(false, i18n)).toBe("No");
		expect(formatVersionValue({ enabled: true }, i18n)).toContain('"enabled": true');

		const timeCollection = {
			...collection,
			fields: [
				{
					name: "startsAt",
					path: "startsAt",
					type: "date",
					admin: { label: "Starts at" },
					date: { format: "time" },
				},
			],
		} as SchemaCollection;
		const current = {
			...version(2, "", ""),
			Snapshot: { id: "post_1", startsAt: "08:35:00" },
		};
		const row = versionDiffRows(timeCollection, current, undefined, i18n)[0]!;
		expect(formatVersionValue(row.after, i18n, row)).toBe("08:35");
	});
});

describe("version comparison semantics", () => {
	test("compares objects by value rather than serialization order", () => {
		expect(sameValue({ a: 1, b: 2 }, { b: 2, a: 1 })).toBe(true);
		expect(sameValue([1, 2], [2, 1])).toBe(false);
	});

	test("keeps localized values independent, including absent translations", () => {
		const localized = { ...collection, fields: [{ ...collection.fields[0]!, localized: true }] };
		const current = {
			...version(2, "unused", ""),
			Snapshot: { id: "post_1", title: { en: "Hello", fr: "Bonjour" } },
		};
		const before = { ...current, Revision: 1, Snapshot: { id: "post_1", title: { en: "Hello" } } };
		const row = versionDiffRows(localized, current, before, i18n, ["en", "fr"])[0]!;
		expect(
			row.children?.map(({ locale, before, after, changed }) => ({
				locale,
				before,
				after,
				changed,
			}))
		).toEqual([
			{ locale: "en", before: "Hello", after: "Hello", changed: false },
			{ locale: "fr", before: undefined, after: "Bonjour", changed: true },
		]);
		expect(versionDiffRows(localized, current, before, i18n, ["en"])[0]!.changed).toBe(false);
	});

	test("aligns nested array rows by persisted identity and keeps reorders visible", () => {
		const array = {
			...collection,
			fields: [
				{
					name: "links",
					path: "links",
					type: "array",
					admin: { label: "Links" },
					nested: { fields: [collection.fields[0]!] },
				},
			],
		} as SchemaCollection;
		const a = { _key: "a", title: "A" },
			b = { _key: "b", title: "B" };
		const before = { ...version(1, "", ""), Snapshot: { id: "post_1", links: [a, b] } };
		const after = { ...version(2, "", ""), Snapshot: { id: "post_1", links: [b, a] } };
		const row = versionDiffRows(array, after, before, i18n)[0]!;
		expect(row.changed).toBe(true);
		expect(row.children?.every((child) => child.changed)).toBe(true);
		expect(row.children?.map((child) => child.children?.[0]?.before)).toEqual(["B", "A"]);
		expect(row.children?.map((child) => child.children?.filter((field) => field.changed))).toEqual([
			[expect.objectContaining({ label: "Position", before: 2, after: 1 })],
			[expect.objectContaining({ label: "Position", before: 1, after: 2 })],
		]);
	});

	test("retains removed fields when a block changes type without changing identity", () => {
		const blocks = {
			...collection,
			fields: [
				{
					name: "layout",
					type: "blocks",
					admin: { label: "Layout" },
					blocks: {
						types: [
							{ slug: "text", fields: [collection.fields[0]!] },
							{ slug: "link", fields: [{ name: "url", type: "text", admin: { label: "URL" } }] },
						],
					},
				},
			],
		} as SchemaCollection;
		const before = {
			...version(1, "", ""),
			Snapshot: {
				id: "post_1",
				layout: [{ _key: "same", blockType: "text", title: "Removed title" }],
			},
		};
		const after = {
			...version(2, "", ""),
			Snapshot: {
				id: "post_1",
				layout: [{ _key: "same", blockType: "link", url: "https://example.test" }],
			},
		};
		const children = versionDiffRows(blocks, after, before, i18n)[0]!.children![0]!.children!;
		expect(children).toEqual(
			expect.arrayContaining([
				expect.objectContaining({
					label: "Block type",
					before: "text",
					after: "link",
					changed: true,
				}),
				expect.objectContaining({
					label: "Title",
					before: "Removed title",
					after: undefined,
					changed: true,
				}),
				expect.objectContaining({
					label: "URL",
					before: undefined,
					after: "https://example.test",
					changed: true,
				}),
			])
		);
	});

	test("highlights separate changed phrases without treating HTML as markup", () => {
		const old = "Hello <script>old</script> world and old name";
		const next = "Hello <script>new</script> world and new name";
		const diff = versionTextDiff(old, next);
		expect(diff.before.map((part) => part.text).join("")).toBe(old);
		expect(diff.after.map((part) => part.text).join("")).toBe(next);
		expect(diff.before.filter((part) => part.changed).length).toBe(2);
		expect(
			diff.after
				.filter((part) => !part.changed)
				.map((part) => part.text)
				.join("")
		).toContain(" world and ");
	});

	test("bounds work for large unrelated text and preserves exact whitespace", () => {
		const before = "old\tvalue\n".repeat(400);
		const after = "new  content\n".repeat(400);
		const diff = versionTextDiff(before, after);
		expect(diff.before.map((part) => part.text).join("")).toBe(before);
		expect(diff.after.map((part) => part.text).join("")).toBe(after);
	});
});
