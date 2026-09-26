import { describe, expect, test } from "bun:test";
import type { SchemaField } from "@riducms/protocol";

import {
	applyBulkUploadValues,
	bulkUploadBulkEditFields,
	bulkUploadDocumentFields,
	bulkUploadRenderFields,
	initialBulkUploadValues,
} from "@admin/features/uploads/bulk-upload";

const field = (
	name: string,
	overrides: Partial<SchemaField> & Pick<SchemaField, "type" | "category">
): SchemaField => ({
	id: name,
	name,
	path: name,
	required: false,
	unique: false,
	admin: { label: name },
	...overrides,
});

const fields = [
	field("alt", { type: "text", category: "scalar", required: true }),
	field("caption", { type: "textarea", category: "scalar" }),
	field("kind", { type: "select", category: "scalar", default: "image" }),
	field("sizes", {
		type: "json",
		category: "upload",
		admin: { label: "Sizes", readOnly: true },
	}),
	field("roles", {
		type: "select",
		category: "scalar",
		select: { hasMany: true, defaultValues: ["editor"], options: [] },
	}),
	field("tags", { type: "array", category: "nested" }),
	field("notice", {
		type: "ui",
		category: "presentation",
		admin: { label: "Notice", readOnly: true },
	}),
	field("externalID", { type: "text", category: "scalar", unique: true }),
	field("internalNote", {
		type: "textarea",
		category: "scalar",
		admin: { label: "Internal note", hidden: true },
	}),
	field("conditionalCaption", {
		type: "text",
		category: "scalar",
		admin: {
			label: "Conditional caption",
			condition: {
				kind: "predicate",
				predicate: {
					scope: "document",
					path: "kind",
					operator: "equals",
					values: [{ type: "string", value: "image" }],
				},
			},
		},
	}),
] as SchemaField[];

describe("bulk upload forms", () => {
	test("keeps the normal document field tree while excluding computed upload metadata", () => {
		expect(bulkUploadDocumentFields(fields).map((candidate) => candidate.name)).toEqual([
			"alt",
			"caption",
			"kind",
			"roles",
			"tags",
			"notice",
			"externalID",
			"internalNote",
			"conditionalCaption",
		]);
	});

	test("derives filename defaults without flattening typed schema defaults", () => {
		expect(
			initialBulkUploadValues(
				"launch-diagram_final.png",
				bulkUploadDocumentFields(fields),
				"filename"
			)
		).toEqual({
			alt: "launch diagram final",
			kind: "image",
			roles: ["editor"],
		});
	});

	test("offers safe root scalar fields for editing every queued draft", () => {
		expect(bulkUploadBulkEditFields(fields).map((candidate) => candidate.name)).toEqual([
			"alt",
			"caption",
			"kind",
			"roles",
		]);
	});

	test("scopes concurrent renderer IDs through nested fields without changing form paths", () => {
		const child = field("group.caption", {
			id: "caption",
			name: "caption",
			path: "group.caption",
			type: "text",
			category: "scalar",
			admin: { label: "Caption", row: { id: "caption-row" } },
		});
		const group = field("group", {
			type: "group",
			category: "nested",
			nested: { fields: [child] },
		});
		const [scoped] = bulkUploadRenderFields([group], "remote");
		expect(scoped).toMatchObject({
			id: "group-remote",
			path: "group",
			nested: {
				fields: [
					{
						id: "caption-remote",
						path: "group.caption",
						admin: { row: { id: "caption-row-remote" } },
					},
				],
			},
		});
	});

	test("applies selected values through form paths and clones mutable values", () => {
		const applied: Record<string, unknown> = {};
		const values = { roles: ["editor", "author"] };
		applyBulkUploadValues(
			{ set: (path, value) => (applied[path] = value) },
			[fields.find((candidate) => candidate.name === "roles")!],
			values
		);
		expect(applied).toEqual(values);
		expect(applied.roles).not.toBe(values.roles);
	});
});
