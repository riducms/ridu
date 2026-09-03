import { describe, expect, it } from "bun:test";
import type { SchemaField } from "@riducms/protocol";

import { partitionDocumentFields } from "@admin/features/documents/document-field-partition";

describe("document field partition", () => {
	it("preserves order inside the main and sidebar regions while omitting static hidden fields", () => {
		const title = textField("title");
		const status = textField("status", { sidebar: true });
		const internal = textField("internal", { hidden: true, sidebar: true });
		const owner = textField("owner", { sidebar: true });
		const body = textField("body");
		const fields = [title, status, internal, owner, body];

		const partition = partitionDocumentFields(fields);

		expect(partition.content.map((field) => field.name)).toEqual(["title", "body"]);
		expect(partition.sidebar.map((field) => field.name)).toEqual(["status", "owner"]);
		expect(fields.map((field) => field.name)).toEqual([
			"title",
			"status",
			"internal",
			"owner",
			"body",
		]);
	});
});

function textField(name: string, admin: Partial<SchemaField["admin"]> = {}): SchemaField {
	return {
		id: `posts-${name}`,
		name,
		path: name,
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: name, ...admin },
		text: {},
	};
}
