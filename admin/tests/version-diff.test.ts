import { describe, expect, test } from "bun:test";
import type { SchemaCollection } from "@riducms/protocol";
import { createAdminI18n, en } from "@riducms/translations";

import type { AdminVersion } from "@admin/core/api/admin-client";
import { formatVersionValue, versionDiffRows } from "@admin/features/versions/version-diff";

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
		expect(rows.map((row) => row.path)).toEqual(["_status", "title", "seo.description"]);
		expect(rows.filter((row) => row.changed).map((row) => row.path)).toEqual(["_status", "title"]);
	});

	test("formats scalar and structured values for the comparison table", () => {
		expect(formatVersionValue(undefined, i18n)).toBe("—");
		expect(formatVersionValue(false, i18n)).toBe("No");
		expect(formatVersionValue({ enabled: true }, i18n)).toContain('"enabled": true');
	});
});
