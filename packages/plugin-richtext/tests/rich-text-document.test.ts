import { describe, expect, it } from "bun:test";

import { initialEditorState } from "../src/field/rich-text-document";
import { documentRecoveryIssue } from "../src/document-validation";

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
	it("keeps unsupported historical properties out of the editor and leaves payloads opaque", () => {
		const value = {
			version: 1,
			root: { type: "root", children: [{ type: "text", text: "Keep", extension: "historical" }] },
		};
		expect(documentRecoveryIssue(value)).toBe("root.children.0.extension");
		expect(initialEditorState(value)).toBeNull();
		expect(documentRecoveryIssue({ ...value, extra: true })).toBe("extra");
		expect(
			documentRecoveryIssue({ version: 1, root: { type: "root", version: 2, children: [] } })
		).toBe("root.version");
		expect(
			documentRecoveryIssue({
				version: 1,
				root: { type: "root", children: [{ type: "text", text: "Keep", caption: "wrong node" }] },
			})
		).toBe("root.children.0.caption");
		expect(
			documentRecoveryIssue({
				version: 1,
				root: {
					type: "root",
					children: [
						{
							type: "block",
							version: 1,
							fields: {
								_key: "historical",
								blockType: "removed",
								arbitrary: { type: "nonsense", children: true },
							},
						},
					],
				},
			})
		).toBeUndefined();
	});
	it("rejects modes and alignment values that Lexical would silently normalize", () => {
		for (const [node, path] of [
			[{ type: "text", text: "Keep", mode: "historical-mode" }, "mode"],
			[{ type: "paragraph", children: [], format: "historical-alignment" }, "format"],
			[{ type: "paragraph", children: [], direction: "historical-direction" }, "direction"],
		] as const) {
			expect(documentRecoveryIssue({ version: 1, root: { type: "root", children: [node] } })).toBe(
				`root.children.0.${path}`
			);
		}
	});
});
