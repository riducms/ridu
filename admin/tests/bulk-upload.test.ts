import { describe, expect, test } from "bun:test";
import type { SchemaField } from "@riducms/protocol";

import {
	bulkUploadMetadataFields,
	initialBulkUploadValues,
	uploadData,
} from "@admin/features/uploads/bulk-upload";

const fields = [
	{
		id: "alt",
		name: "alt",
		type: "text",
		category: "scalar",
		required: true,
		admin: { label: "Alt" },
	},
	{
		id: "caption",
		name: "caption",
		type: "textarea",
		category: "scalar",
		required: false,
		admin: { label: "Caption" },
	},
	{
		id: "kind",
		name: "kind",
		type: "select",
		category: "scalar",
		required: false,
		default: "image",
		admin: { label: "Kind" },
	},
	{
		id: "sizes",
		name: "sizes",
		type: "json",
		category: "upload",
		required: false,
		admin: { label: "Sizes", readOnly: true },
	},
	{
		id: "roles",
		name: "roles",
		type: "select",
		category: "scalar",
		required: false,
		admin: { label: "Roles" },
		select: { hasMany: true, defaultValues: ["editor"] },
	},
	{
		id: "tags",
		name: "tags",
		type: "array",
		category: "nested",
		required: false,
		admin: { label: "Tags" },
	},
] as SchemaField[];

describe("bulk upload metadata", () => {
	test("offers editable scalar metadata and derives accessible alt text from filenames", () => {
		const editable = bulkUploadMetadataFields(fields);
		expect(editable.map((field) => field.name)).toEqual(["alt", "caption", "kind"]);
		expect(initialBulkUploadValues("launch-diagram_final.png", editable, "filename")).toEqual({
			alt: "launch diagram final",
			caption: "",
			kind: "image",
		});
	});

	test("omits blank optional metadata from upload requests", () => {
		expect(uploadData({ alt: "Diagram", caption: "  ", kind: "image" })).toEqual({
			alt: "Diagram",
			kind: "image",
		});
	});
});
