import { describe, expect, it } from "bun:test";
import { CodeNode } from "@lexical/code";
import { LinkNode } from "@lexical/link";
import { ListItemNode, ListNode } from "@lexical/list";
import { $convertFromMarkdownString } from "@lexical/markdown";
import { HeadingNode, QuoteNode } from "@lexical/rich-text";
import { createEditor } from "lexical";

import { richTextMarkdownTransformers } from "../src/field/rich-text-markdown";
import { decodeRichTextConfig, type RichTextFeature } from "../src/field/rich-text-config";
import { decodeRichTextDocument } from "../src/field/rich-text-value";

function convert(markdown: string, features: RichTextFeature[] = []) {
	const editor = createEditor({
		namespace: "markdown-contract",
		nodes: [HeadingNode, QuoteNode, ListNode, ListItemNode, LinkNode, CodeNode],
		onError(error) {
			throw error;
		},
	});
	editor.update(
		() => {
			$convertFromMarkdownString(
				markdown,
				richTextMarkdownTransformers(decodeRichTextConfig({ features }))
			);
		},
		{ discrete: true }
	);
	return decodeRichTextDocument(
		JSON.parse(
			JSON.stringify({
				version: 1,
				root: editor.getEditorState().toJSON().root,
			})
		)
	);
}

describe("rich-text markdown shortcuts", () => {
	it("keeps formatting, headings and checklists in the canonical stored node format", () => {
		const document = convert("###### Sixth heading\n\n**Bold** and _italic_\n\n- [x] Complete", [
			"lists",
		]);
		expect(document.root.children[0]).toMatchObject({ type: "heading", tag: "h6" });
		expect(document.root.children[1]).toMatchObject({
			type: "paragraph",
			children: [{ text: "Bold", format: 1 }, { text: " and " }, { text: "italic", format: 2 }],
		});
		expect(document.root.children[2]).toMatchObject({
			type: "list",
			listType: "check",
			children: [{ type: "listitem", checked: true }],
		});
	});

	it("preserves plain text when list, link and code features are disabled", () => {
		for (const markdown of [
			"- plain list",
			"[ ] unchecked",
			"[docs](https://ridu.dev)",
			"`inline`",
		]) {
			expect(convert(markdown).root.children).toMatchObject([
				{
					type: "paragraph",
					children: [{ type: "text", text: markdown, format: 0 }],
				},
			]);
		}
		expect(convert("```js\nconst value = 1;\n```").root.children).toMatchObject([
			{
				type: "paragraph",
				children: [
					{ type: "text", text: "```js", format: 0 },
					{ type: "linebreak" },
					{ type: "text", text: "const value = 1;", format: 0 },
					{ type: "linebreak" },
					{ type: "text", text: "```", format: 0 },
				],
			},
		]);
	});

	it("normalizes allowed links and leaves executable URLs as ordinary text", () => {
		expect(convert("[docs](ridu.dev)", ["links"]).root.children[0]).toMatchObject({
			children: [{ type: "link", url: "https://ridu.dev" }],
		});
		expect(convert("[bad](javascript:alert)", ["links"]).root.children[0]).toMatchObject({
			children: [{ type: "text", text: "[bad](javascript:alert)" }],
		});
	});
	it("validates the final escaped URL without double-decoding author text", () => {
		for (const [markdown, url] of [
			[String.raw`[docs](https\://example.com)`, "https://example.com"],
			[String.raw`[mail](mailto\:editor@example.com)`, "mailto:editor@example.com"],
			["[docs](https://example.com/?q=&#38;#58;)", "https://example.com/?q=&#58;"],
		]) {
			expect(convert(markdown!, ["links"]).root.children[0]).toMatchObject({
				children: [{ type: "link", url }],
			});
		}
		for (const markdown of [String.raw`[bad](javascript\:alert)`, "[bad](javascript&#58;alert)"])
			expect(convert(markdown, ["links"]).root.children[0]).toMatchObject({
				children: [{ type: "text", text: "[bad](javascript:alert)" }],
			});
	});
});
