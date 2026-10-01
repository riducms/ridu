import { describe, expect, it } from "bun:test";
import type { RichTextBlockRenderers, RichTextDocument } from "../src/richtext/index";
import { documentRecoveryIssue, renderRichTextHTML, safeRichTextURL } from "../src/richtext/index";

type Callout = { blockType: "callout"; _key: string; title?: string };
type CTA = { blockType: "cta"; _key: string; label?: string; url?: string };
const blocks: RichTextBlockRenderers<Callout | CTA, string> = {
	callout: (block) => `<aside>${block.title ?? ""}</aside>`,
	cta: (block) => `<button>${block.label ?? ""}</button>`,
};
const value: RichTextDocument<Callout | CTA> = {
	version: 1,
	root: {
		type: "root",
		children: [
			{ type: "paragraph", children: [{ type: "text", text: "<Hello>&", format: 1 }] },
			{
				type: "block",
				version: 1,
				fields: { blockType: "callout", _key: "one", title: "Read me" },
			},
			{ type: "block", version: 1, fields: { blockType: "cta", _key: "two", label: "Go" } },
		],
	},
};

describe("portable rich-text rendering", () => {
	it("escapes prose and dispatches typed block variants without fetching", () => {
		expect(renderRichTextHTML(value, { blocks })).toBe(
			"<p><strong>&lt;Hello&gt;&amp;</strong></p><aside>Read me</aside><button>Go</button>"
		);
	});
	it("fails explicitly or invokes the supplied unknown-content fallback", () => {
		expect(() => renderRichTextHTML(value)).toThrow("No renderer registered");
		expect(
			renderRichTextHTML(value, {
				fallback: () => "<aside>Unsupported content — export required</aside>",
			})
		).toContain("Unsupported content");
	});
	it("does not treat object prototype properties as registered block renderers", () => {
		const inherited: RichTextDocument<{ blockType: "constructor"; _key: string }> = {
			version: 1,
			root: {
				type: "root",
				children: [
					{ type: "block", version: 1, fields: { blockType: "constructor", _key: "one" } },
				],
			},
		};
		expect(() =>
			renderRichTextHTML(inherited, { blocks: Object.create({ constructor: () => "incorrect" }) })
		).toThrow("No renderer registered");
	});
	it("preserves unsupported envelopes for explicit recovery instead of rendering partial content", () => {
		const historical = {
			version: 1,
			root: { type: "root", children: [{ type: "text", text: "Visible", caption: "Keep this" }] },
		} as unknown as RichTextDocument;
		expect(() => renderRichTextHTML(historical)).toThrow("root.children.0.caption");
		expect(
			renderRichTextHTML(historical, {
				fallback: (raw, error) => {
					expect(raw).toBe(historical);
					expect(error.message).toContain("root.children.0.caption");
					return "<aside>Export required</aside>";
				},
			})
		).toBe("<aside>Export required</aside>");
	});
	it("does not permit unsafe links or unbounded nesting", () => {
		const unsafe: RichTextDocument = {
			version: 1,
			root: {
				type: "root",
				children: [{ type: "link", url: "javascript:alert(1)", children: [] }],
			},
		};
		expect(() => renderRichTextHTML(unsafe)).toThrow("root.children.0.url");
		let node = { type: "paragraph", children: [] } as RichTextDocument["root"]["children"][number];
		for (let index = 0; index < 70; index += 1) node = { type: "paragraph", children: [node] };
		expect(() =>
			renderRichTextHTML({ version: 1, root: { type: "root", children: [node] } })
		).toThrow("budget");
	});
});

describe("portable rich-text link URLs", () => {
	it("returns safe absolute, relative, fragment and mail destinations unchanged", () => {
		for (const value of ["https://ridu.dev", "/docs", "#overview", "mailto:editor@ridu.dev"])
			expect(safeRichTextURL(value)).toBe(value);
	});
	it("rejects executable, unlisted, protocol-relative and control-character-obfuscated URLs", () => {
		for (const value of [
			"javascript:alert(1)",
			"data:text/html,<script>alert(1)</script>",
			"sms:+447700900123",
			"//example.com/path",
			"java\nscript:alert(1)",
		])
			expect(() => safeRichTextURL(value)).toThrow("Unsafe");
	});
});

describe("portable rich-text document envelopes", () => {
	it("reports unsupported historical properties while leaving block payloads opaque", () => {
		const value = {
			version: 1,
			root: { type: "root", children: [{ type: "text", text: "Keep", extension: "historical" }] },
		};
		expect(documentRecoveryIssue(value)).toBe("root.children.0.extension");
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
	it("rejects modes and alignment values that editors would silently normalize", () => {
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
	it("rejects links that portable renderers cannot expose", () => {
		for (const url of ["javascript:alert(1)", "sms:+447700900123", "//example.com/path"])
			expect(
				documentRecoveryIssue({
					version: 1,
					root: { type: "root", children: [{ type: "link", url, children: [] }] },
				})
			).toBe("root.children.0.url");
	});
});
