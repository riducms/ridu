import { describe, expect, it } from "bun:test";

import { initialEditorState } from "../src/field/rich-text-document";

describe("rich-text document hydration", () => {
	it("supplies Lexical element defaults without mutating stored documents", () => {
		const stored = {
			version: 1,
			root: {
				type: "root",
				children: [
					{
						type: "paragraph",
						children: [{ type: "text", text: "Portable rich text" }],
					},
				],
			},
		};

		expect(JSON.parse(initialEditorState(stored) ?? "null")).toEqual({
			root: {
				type: "root",
				children: [
					{
						type: "paragraph",
						children: [{ type: "text", text: "Portable rich text" }],
						direction: null,
						format: "",
						indent: 0,
					},
				],
				direction: null,
				format: "",
				indent: 0,
			},
		});
		expect(stored.root).not.toHaveProperty("indent");
	});

	it("preserves explicit element formatting and rejects unsupported envelopes", () => {
		const state = initialEditorState({
			version: 1,
			root: {
				type: "root",
				children: [],
				direction: "rtl",
				format: "center",
				indent: 2,
			},
		});

		expect(JSON.parse(state ?? "null").root).toMatchObject({
			direction: "rtl",
			format: "center",
			indent: 2,
		});
		expect(initialEditorState({ version: 2, root: {} })).toBeNull();
		expect(initialEditorState(null)).toBeNull();
	});
});
