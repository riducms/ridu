import { describe, expect, it } from "bun:test";
import type { SchemaBlockType, SchemaField } from "@riducms/protocol";
import { copyBlockClipboardNodes } from "../src/block/rich-text-block-clipboard";
import { blockSummary, richTextBlockTypes } from "../src/field/rich-text-blocks";

const callout: SchemaBlockType = {
	slug: "callout",
	labels: { singular: "Callout", plural: "Callouts" },
	admin: { rowLabel: "appearance.caption" },
	fields: [],
};
const field: SchemaField = {
	id: "body",
	path: "body",
	name: "body",
	type: "plugin",
	category: "plugin",
	required: false,
	unique: false,
	admin: { label: "Body" },
	plugin: {
		key: "richtext",
		config: { blocks: [{ key: "injected" }] },
		embeddedTrees: [
			{
				version: 1,
				key: "blocks",
				root: ["root"],
				children: "children",
				tag: "type",
				cases: [
					{
						tagValue: "block",
						payload: "fields",
						discriminator: "blockType",
						identity: "_key",
						types: [callout],
					},
				],
			},
		],
	},
};

describe("rich-text schema block authoring", () => {
	it("uses resolved definitions, not arbitrary plugin settings, and names a useful nested summary", () => {
		expect(richTextBlockTypes(field)).toEqual([callout]);
		expect(blockSummary({ appearance: { caption: "A useful summary" } }, callout)).toBe(
			"A useful summary"
		);
		expect(blockSummary({}, callout)).toBe("");
	});
	it("clones declared clipboard payloads through the host without interpreting arbitrary nested JSON", () => {
		const nodes = [
			{
				type: "block",
				version: 1,
				fields: {
					_key: "old",
					blockType: "callout",
					ordinary: { type: "block", fields: { blockType: "not-a-plugin" } },
				},
			},
		];
		let copies = 0;
		const copied = copyBlockClipboardNodes(nodes, [callout], (scope, payload) => {
			expect(scope).toEqual({ treeKey: "blocks", caseTag: "block", variantSlug: "callout" });
			copies++;
			return { ...payload, _key: "fresh" };
		});
		expect(copies).toBe(1);
		expect(copied.hasBlocks).toBe(true);
		expect(copied.nodes[0]).toMatchObject({
			fields: { _key: "fresh", ordinary: nodes[0].fields.ordinary },
		});
		expect(nodes[0].fields._key).toBe("old");
	});
	it("rejects unknown variants before creating any copies", () => {
		let copies = 0;
		expect(() =>
			copyBlockClipboardNodes(
				[
					{ type: "block", version: 1, fields: { blockType: "missing" } },
					{ type: "block", version: 1, fields: { blockType: "callout" } },
				],
				[callout],
				(scope, payload) => {
					copies++;
					return { ...payload };
				}
			)
		).toThrow("does not support a block type");
		expect(copies).toBe(0);
	});
	it("bounds clipboard traversal and leaves plain text to the normal Lexical importer", () => {
		let nested: Record<string, unknown> = { type: "paragraph", version: 1, children: [] };
		for (let index = 0; index < 66; index++)
			nested = { type: "paragraph", version: 1, children: [nested] };
		expect(() =>
			copyBlockClipboardNodes([nested], [callout], (scope, payload) => ({ ...payload }))
		).toThrow("traversal limit");
		expect(
			copyBlockClipboardNodes(
				[{ type: "text", version: 1, text: "hello" }],
				[],
				(scope, payload) => ({ ...payload })
			).hasBlocks
		).toBe(false);
	});
	it("rejects extra clipboard envelope properties before copying payloads", () => {
		let copies = 0;
		for (const node of [
			{ type: "block", version: 1, fields: { blockType: "callout" }, legacy: "keep" },
			{ type: "text", version: 1, text: "Keep", extension: "keep" },
		]) {
			expect(() =>
				copyBlockClipboardNodes([node], [callout], (_scope, payload) => {
					copies++;
					return { ...payload };
				})
			).toThrow("contract");
		}
		expect(copies).toBe(0);
	});
});
