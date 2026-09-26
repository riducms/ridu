import { describe, expect, test } from "bun:test";
import { joinFormPath, readFormPath } from "../src/core/forms/form-path";
import { readDocumentPath } from "../src/core/schema/read-document-path";

describe("field value paths", () => {
	const values = {
		seo: { title: "Nested title" },
		rows: [{ title: "First row" }, { title: "Second row" }],
		empty: null,
	};

	test("both readers follow object fields and stop at missing or scalar parents", () => {
		for (const read of [readFormPath, readDocumentPath]) {
			expect(read(values, "seo.title")).toBe("Nested title");
			expect(read(values, "missing.title")).toBeUndefined();
			expect(read(values, "empty.title")).toBeUndefined();
			expect(read(values, "seo.title.length")).toBeUndefined();
		}
	});

	test("form paths address array indexes; document paths preserve arrays as whole values", () => {
		expect(readFormPath(values, "rows.1.title")).toBe("Second row");
		expect(readFormPath(values, "rows.2.title")).toBeUndefined();
		expect(readFormPath(values, "rows.length")).toBeUndefined();
		expect(readDocumentPath(values, "rows.1.title")).toBeUndefined();
		expect(readDocumentPath(values, "rows")).toBe(values.rows);
	});

	test("joins root and nested form paths without changing row indexes", () => {
		expect(joinFormPath("", "title")).toBe("title");
		expect(joinFormPath("rows.1", "title")).toBe("rows.1.title");
	});
});
