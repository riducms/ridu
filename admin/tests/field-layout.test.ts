import { describe, expect, it } from "bun:test";
import type { SchemaField } from "@riducms/protocol";

import { createFieldLayout } from "../src/fields/field-layout";

describe("field layout", () => {
	it("turns shared row metadata into one explicit layout group", () => {
		const fields = [
			textField("posts-summary", "summary"),
			textField("posts-author", "author", "posts-author"),
			textField("posts-status", "status", "posts-author"),
			textField("posts-content", "content"),
		];

		const layout = createFieldLayout(fields);

		expect(layout.map((group) => group.fields.map((field) => field.name))).toEqual([
			["summary"],
			["author", "status"],
			["content"],
		]);
		expect(layout[1]?.row?.id).toBe("posts-author");
	});

	it("does not merge non-row fields that happen to be adjacent", () => {
		const layout = createFieldLayout([
			textField("posts-author", "author"),
			textField("posts-status", "status"),
		]);

		expect(layout).toHaveLength(2);
	});

	it("groups consecutive fields in a collapsible without changing their paths", () => {
		const first = textField("posts-internal-name", "internalName");
		const second = textField("posts-notes", "notes");
		first.admin.collapsible = { id: "posts-advanced", label: "Advanced", initiallyCollapsed: true };
		second.admin.collapsible = {
			id: "posts-advanced",
			label: "Advanced",
			initiallyCollapsed: true,
		};

		const layout = createFieldLayout([first, second]);

		expect(layout).toHaveLength(1);
		expect(layout[0]?.collapsible?.label).toBe("Advanced");
		expect(layout[0]?.fields.map((field) => field.path)).toEqual(["internalName", "notes"]);
	});

	it("keeps separately authored tab groups local and in field order", () => {
		const content = textField("posts-summary", "summary");
		const seo = textField("posts-seo", "seo");
		seo.admin.tab = "SEO";
		seo.admin.tabGroup = { id: "posts-seo-tabs" };
		const related = textField("posts-links", "links");
		related.admin.tab = "Related";
		related.admin.tabGroup = { id: "posts-related-tabs" };
		const layout = textField("posts-layout", "layout");
		layout.admin.tab = "Layout";
		layout.admin.tabGroup = { id: "posts-related-tabs" };

		const groups = createFieldLayout([content, seo, related, layout]);

		expect(groups.map((group) => group.key)).toEqual([
			"field:posts-summary",
			"tabs:posts-seo-tabs:posts-seo",
			"tabs:posts-related-tabs:posts-links",
		]);
		expect(groups[1]?.fields.map((field) => field.name)).toEqual(["seo"]);
		expect(groups[2]?.fields.map((field) => field.name)).toEqual(["links", "layout"]);
	});

	it("restores row grouping inside the tab group that owns the fields", () => {
		const title = textField("posts-seo-title", "title", "posts-seo-row");
		const canonical = textField("posts-seo-canonical", "canonical", "posts-seo-row");
		for (const field of [title, canonical]) {
			field.admin.tab = "SEO";
			field.admin.tabGroup = { id: "posts-seo-tabs" };
		}

		const outer = createFieldLayout([title, canonical]);
		const inner = createFieldLayout(outer[0]!.fields, "posts-seo-tabs");

		expect(outer).toHaveLength(1);
		expect(inner).toHaveLength(1);
		expect(inner[0]?.row?.id).toBe("posts-seo-row");
	});

	it("gives non-contiguous tab occurrences distinct render keys", () => {
		const score = textField("posts-score", "score");
		const published = textField("posts-published", "published");
		for (const field of [score, published]) {
			field.admin.tab = "Scoring";
			field.admin.tabGroup = { id: "posts-scoring-tabs" };
		}

		const groups = createFieldLayout([score, textField("posts-summary", "summary"), published]);

		expect(groups.map((group) => group.key)).toEqual([
			"tabs:posts-scoring-tabs:posts-score",
			"field:posts-summary",
			"tabs:posts-scoring-tabs:posts-published",
		]);
	});
});

function textField(id: string, name: string, row?: string): SchemaField {
	return {
		id,
		name,
		path: name,
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: name, ...(row === undefined ? {} : { row: { id: row } }) },
		text: {},
	};
}
