import { describe, expect, it } from "bun:test";
import { $createLinkNode, LinkNode } from "@lexical/link";
import { $createParagraphNode, $createTextNode, $getRoot, createEditor } from "lexical";

import { normalizeLinkURL } from "../src/link/link-url";
import { registerSafeLinkTransform } from "../src/link/safe-link-transform";
import { safeRichTextURL } from "../src/render";

function transformedLink(url: string) {
	const editor = createEditor({
		namespace: "safe-link-transform",
		nodes: [LinkNode],
		onError(error) {
			throw error;
		},
	});
	const unregister = registerSafeLinkTransform(editor);
	editor.update(
		() => {
			const link = $createLinkNode(url);
			link.append($createTextNode("Link"));
			$getRoot().append($createParagraphNode().append(link));
		},
		{ discrete: true }
	);
	const paragraph = editor.getEditorState().toJSON().root.children[0];
	const node =
		paragraph !== undefined && "children" in paragraph && Array.isArray(paragraph.children)
			? paragraph.children[0]
			: undefined;
	unregister();
	return node;
}

describe("rich-text link URLs", () => {
	it("normalizes ordinary author input and retains safe relative destinations", () => {
		expect(normalizeLinkURL("ridu.dev/docs")).toBe("https://ridu.dev/docs");
		expect(normalizeLinkURL("editor@ridu.dev")).toBe("mailto:editor@ridu.dev");
		expect(normalizeLinkURL("/docs/getting-started#install")).toBe("/docs/getting-started#install");
		expect(normalizeLinkURL("#overview")).toBe("#overview");
		for (const value of ["https://ridu.dev", "/docs", "#overview", "mailto:editor@ridu.dev"])
			expect(safeRichTextURL(value)).toBe(value);
	});

	it("rejects executable, malformed, and control-character-obfuscated schemes", () => {
		expect(normalizeLinkURL("javascript:alert(1)")).toBeUndefined();
		expect(normalizeLinkURL("data:text/html,<script>alert(1)</script>")).toBeUndefined();
		expect(normalizeLinkURL("java\nscript:alert(1)")).toBeUndefined();
		expect(normalizeLinkURL("https://")).toBeUndefined();
		expect(normalizeLinkURL("sms:+447700900123")).toBeUndefined();
		expect(normalizeLinkURL("//example.com/path")).toBeUndefined();
		expect(normalizeLinkURL(" ")).toBeUndefined();
		expect(() => safeRichTextURL("sms:+447700900123")).toThrow("Unsafe");
		expect(() => safeRichTextURL("//example.com/path")).toThrow("Unsafe");
	});

	it("unwraps imported unsafe links before serialization", () => {
		expect(transformedLink("javascript:alert(1)")).toMatchObject({ type: "text", text: "Link" });
		expect(transformedLink("https://ridu.dev/docs")).toMatchObject({
			type: "link",
			url: "https://ridu.dev/docs",
		});
	});
});
