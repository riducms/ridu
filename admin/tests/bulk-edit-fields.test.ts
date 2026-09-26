import { describe, expect, test } from "bun:test";
import type { SchemaField } from "@riducms/protocol";

import { submissionFormValues } from "@admin/core/forms/form-schema";
import { initialBulkEditValues } from "@admin/features/bulk-edit/bulk-edit-fields";

const field = (
	name: string,
	type: SchemaField["type"],
	overrides: Partial<SchemaField> = {}
): SchemaField => ({
	id: name,
	name,
	path: name,
	type,
	category: "scalar",
	required: false,
	unique: false,
	admin: { label: name },
	...overrides,
});

describe("bulk edit field values", () => {
	test("serializes selected empty scalar values as an explicit patch", () => {
		const fields = [
			field("title", "text"),
			field("publishedAt", "date"),
			field("featured", "checkbox"),
			field("readingMinutes", "number"),
			field("audience", "select", { select: { options: [], hasMany: true } }),
		] as const;

		const values = initialBulkEditValues(fields);
		expect(values).toEqual({
			title: "",
			publishedAt: "",
			featured: false,
			readingMinutes: null,
			audience: [],
		});
		expect(submissionFormValues(fields, values)).toEqual(values);
	});

	test("keeps defaults and previously entered values when a field is reselected", () => {
		const title = field("title", "text");
		const status = field("status", "select", { default: "draft", select: { options: [] } });
		const first = initialBulkEditValues([title, status]);
		first.title = "Retained title";
		const deselected = initialBulkEditValues([status], first);

		expect(initialBulkEditValues([title, status], deselected)).toEqual({
			title: "Retained title",
			status: "draft",
		});
	});
});
