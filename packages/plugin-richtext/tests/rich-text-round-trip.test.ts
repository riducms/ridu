import { describe, expect, it } from "bun:test";
import { CodeNode } from "@lexical/code";
import { LinkNode } from "@lexical/link";
import { ListItemNode, ListNode } from "@lexical/list";
import { HeadingNode, QuoteNode } from "@lexical/rich-text";
import { documentRecoveryIssue } from "@riducms/sdk/richtext";
import { $createParagraphNode, $createTextNode, $getRoot, createEditor } from "lexical";

import { BlockNode } from "../src/block/rich-text-block-node";
import { initialEditorState } from "../src/field/rich-text-document";

// Svelte-backed upload, relationship and horizontal-rule nodes need the admin build; the
// rich-text editing Playwright test round-trips them through the real editor.
const text = { type: "text", text: "Text" };
const item = { type: "listitem", children: [text] };
const sparsest: Record<string, Record<string, unknown>> = {
	"root-level text": text,
	"root-level line break": { type: "linebreak" },
	paragraph: { type: "paragraph", children: [text] },
	heading: { type: "heading", tag: "h2", children: [text] },
	quote: { type: "quote", children: [text] },
	link: {
		type: "paragraph",
		children: [{ type: "link", url: "https://example.com", children: [text] }],
	},
	linebreak: { type: "paragraph", children: [text, { type: "linebreak" }, text] },
	"bullet list": { type: "list", listType: "bullet", children: [item, item] },
	"number list": { type: "list", listType: "number", children: [item, item] },
	"check list": {
		type: "list",
		listType: "check",
		children: [
			{ ...item, checked: true },
			{ ...item, checked: false },
		],
	},
	"nested list": {
		type: "list",
		listType: "number",
		children: [
			item,
			{ type: "listitem", children: [{ type: "list", listType: "number", children: [item] }] },
			item,
		],
	},
	code: { type: "code", children: [text] },
	block: { type: "block", version: 1, fields: { blockType: "callout" } },
};

function editorFor(document: unknown) {
	const editor = createEditor({
		namespace: "rich-text-round-trip",
		nodes: [HeadingNode, QuoteNode, ListNode, ListItemNode, LinkNode, CodeNode, BlockNode],
		onError: (error) => {
			throw error;
		},
	});
	editor.setEditorState(editor.parseEditorState(initialEditorState(document)!));
	return editor;
}

// The editor's change listener validates this wire value before saving it.
function exported(editor: ReturnType<typeof createEditor>) {
	return JSON.parse(
		JSON.stringify({ version: 1, root: editor.getEditorState().toJSON().root })
	) as { root: { children: Record<string, unknown>[] } };
}

describe("rich-text editor round trip", () => {
	for (const [name, node] of Object.entries(sparsest)) {
		it(`exports a sparse ${name} the server accepts`, () => {
			const document = { version: 1, root: { type: "root", children: [node] } };
			expect(documentRecoveryIssue(document)).toBeUndefined();
			const editor = editorFor(document);
			expect(documentRecoveryIssue(exported(editor))).toBeUndefined();
			editor.update(
				() => {
					$getRoot().append($createParagraphNode().append($createTextNode("Edited")));
				},
				{ discrete: true }
			);
			expect(documentRecoveryIssue(exported(editor))).toBeUndefined();
		});
	}

	it("numbers sparse list items from the default start", () => {
		const editor = editorFor({
			version: 1,
			root: { type: "root", children: [sparsest["nested list"], sparsest["check list"]] },
		});
		const [numbered, checked] = exported(editor).root.children as {
			start: number;
			children: { value: number }[];
		}[];
		expect(numbered.start).toBe(1);
		expect(numbered.children.map((child) => child.value)).toEqual([1, 2, 2]);
		expect(checked.start).toBe(1);
		expect(checked.children.map((child) => child.value)).toEqual([1, 2]);
	});
});
