import { describe, expect, it } from "bun:test";
import { equalRichTextValues } from "../src/field/rich-text-value";

describe("rich-text wire value equality", () => {
	it("accepts recursively reordered server properties and absent optional values", () => {
		const editor = {
			version: 1,
			root: {
				type: "root",
				children: [{ type: "block", fields: { title: "Callout", _key: "one", absent: undefined } }],
			},
		};
		const server = {
			root: {
				children: [{ fields: { _key: "one", title: "Callout" }, type: "block" }],
				type: "root",
			},
			version: 1,
		};
		expect(equalRichTextValues(editor, server)).toBe(true);
		expect(equalRichTextValues(server, editor)).toBe(true);
	});

	it("retains meaningful differences in values, properties, and node order", () => {
		expect(equalRichTextValues({ title: "Before" }, { title: "After" })).toBe(false);
		expect(equalRichTextValues({ title: "One" }, { title: "One", caption: "Extra" })).toBe(false);
		expect(
			equalRichTextValues([{ _key: "one" }, { _key: "two" }], [{ _key: "two" }, { _key: "one" }])
		).toBe(false);
		expect(equalRichTextValues([], {})).toBe(false);
		expect(equalRichTextValues({ title: null }, {})).toBe(false);
		expect(equalRichTextValues(null, null)).toBe(true);
	});
});
