import { describe, expect, it } from "bun:test";
import { createEditor } from "lexical";

import { initialEditorState } from "../src/lib/field/rich-text-document";
import { decodeRichTextDocument } from "../src/lib/field/rich-text-value";

describe("rich-text document hydration", () => {
	it("supplies Lexical defaults without mutating stored documents", () => {
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
						children: [
							{
								type: "text",
								text: "Portable rich text",
								format: 0,
								detail: 0,
								mode: "normal",
								style: "",
							},
						],
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
			children: [
				{
					type: "paragraph",
					children: [],
					direction: null,
					format: "",
					indent: 0,
				},
			],
			direction: "rtl",
			format: "center",
			indent: 2,
		});
		const editor = createEditor({ namespace: "empty-rich-text-hydration" });
		expect(() => editor.setEditorState(editor.parseEditorState(state!))).not.toThrow();
		expect(initialEditorState({ version: 2, root: {} })).toBeNull();
		expect(initialEditorState(null)).toBeNull();
	});
	it("keeps unsupported historical properties out of the editor", () => {
		expect(
			initialEditorState({
				version: 1,
				root: {
					type: "root",
					children: [{ type: "text", text: "Keep", extension: "historical" }],
				},
			})
		).toBeNull();
	});

	it("decodes admitted documents and reports the unsupported path", () => {
		const value: unknown = { version: 1, root: { type: "root", children: [] } };
		expect<unknown>(decodeRichTextDocument(value)).toBe(value);
		expect(() =>
			decodeRichTextDocument({ version: 2, root: { type: "root", children: [] } })
		).toThrow("Invalid rich-text document at version.");
	});
});
