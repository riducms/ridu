import { describe, expect, it } from "bun:test";
import { documentRecoveryIssue, type RichTextNode } from "@riducms/sdk/richtext";

import { convertMarkdownToLexical } from "../src/lib/markdown/index";

const markdown = [
	"# Drink water",
	"",
	"Every **day**, *even* ~~not~~ when `you` are [not thirsty](https://example.com).",
	"",
	"- Before meals",
	"- After exercise",
	"",
	"1. First",
	"",
	"- [x] Done",
	"",
	"> Ask a doctor",
	"",
	"```",
	"const water = 2;",
	"```",
	"",
	"---",
].join("\n");

function types(nodes: readonly RichTextNode[]): string[] {
	return nodes.map((node) => node.type);
}

describe("convertMarkdownToLexical", () => {
	it("makes the document the editor's Markdown shortcuts make", () => {
		const document = convertMarkdownToLexical(markdown);
		expect(documentRecoveryIssue(document)).toBeUndefined();
		expect(types(document.root.children)).toEqual([
			"heading",
			"paragraph",
			"list",
			"list",
			"list",
			"quote",
			"code",
			"horizontalrule",
		]);
		const [heading, paragraph, , , checklist] = document.root.children;
		expect(heading).toMatchObject({ tag: "h1", children: [{ text: "Drink water" }] });
		expect(paragraph?.type === "paragraph" && paragraph.children).toEqual([
			expect.objectContaining({ text: "Every " }),
			expect.objectContaining({ text: "day", format: 1 }),
			expect.objectContaining({ text: ", " }),
			expect.objectContaining({ text: "even", format: 2 }),
			expect.objectContaining({ text: " " }),
			expect.objectContaining({ text: "not", format: 4 }),
			expect.objectContaining({ text: " when " }),
			expect.objectContaining({ text: "you", format: 16 }),
			expect.objectContaining({ text: " are " }),
			expect.objectContaining({ type: "link", url: "https://example.com" }),
			expect.objectContaining({ text: "." }),
		]);
		expect(checklist).toMatchObject({ listType: "check", children: [{ checked: true }] });
	});

	it("keeps Markdown for features the field doesn't have as text", () => {
		const document = convertMarkdownToLexical(markdown, { features: ["links"] });
		expect(types(document.root.children)).not.toContain("list");
		expect(types(document.root.children)).not.toContain("code");
		expect(types(document.root.children)).not.toContain("horizontalrule");
		expect(JSON.stringify(document)).toContain("- Before meals");
	});

	// Lexical records the Markdown it read, such as a `*` bullet or a fence's backticks, on the
	// nodes, and makes a tab its own node. The document stores neither.
	it.each([
		["a `*` bullet", "* Starred", "list"],
		["a `+` bullet", "+ Plus", "list"],
		["a `*` checklist", "* [ ] Todo", "list"],
		["a hard break of two spaces", "Line  \nbreak", "paragraph"],
		["a hard break of a backslash", "Line\\\nbreak", "paragraph"],
		["a fence of four backticks", "````\ncode\n````", "code"],
		["a tab in code", "```\n\tindented\n```", "code"],
	])("stores %s", (_, input, type) => {
		const document = convertMarkdownToLexical(input);
		expect(documentRecoveryIssue(document)).toBeUndefined();
		expect(types(document.root.children)).toEqual([type]);
		expect(JSON.stringify(document)).not.toContain('"$"');
	});

	it("writes a code block's lines with line breaks, like the editor", () => {
		const [code] = convertMarkdownToLexical("```\nfirst\nsecond\n```").root.children;
		expect(code?.type === "code" && code.children).toEqual([
			expect.objectContaining({ type: "text", text: "first" }),
			{ type: "linebreak", version: 1 },
			expect.objectContaining({ type: "text", text: "second" }),
		]);
	});

	it("stores a tab as text", () => {
		const [code] = convertMarkdownToLexical("```\n\tindented\n```").root.children;
		expect(code?.type === "code" && code.children).toEqual([
			expect.objectContaining({ type: "text", text: "\t" }),
			expect.objectContaining({ type: "text", text: "indented" }),
		]);
	});

	it("leaves an unsafe link as text, like the editor", () => {
		const document = convertMarkdownToLexical("[click](javascript:alert(1))");
		expect(JSON.stringify(document)).not.toContain('"type":"link"');
	});

	it("turns empty Markdown into one empty paragraph, as the editor opens an empty document", () => {
		expect(convertMarkdownToLexical("").root.children).toEqual([
			expect.objectContaining({ type: "paragraph", children: [] }),
		]);
	});
});
