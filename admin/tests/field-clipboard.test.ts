import type { SchemaBlockType, SchemaField } from "@riducms/protocol";
import { describe, expect, test } from "bun:test";

import {
	compatibleClipboardValue,
	createFieldClipboardPayload,
	fieldClipboardSignature,
	parseFieldClipboardPayload,
} from "@admin/fields/field-clipboard";

import { bindBlockField } from "./block-manifest";

function scalar(name: string, type: SchemaField["type"] = "text"): SchemaField {
	return {
		id: name,
		name,
		path: name,
		type,
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: name },
	};
}

function hero(childType: SchemaField["type"]): SchemaBlockType {
	const child = (name: string, type: SchemaField["type"] = "text"): SchemaField => ({
		...scalar(name, type),
		id: `block-hero-${name.replace(/([A-Z])/g, "-$1").toLowerCase()}`,
	});
	return {
		slug: "hero",
		labels: { singular: "Hero", plural: "Heroes" },
		fields: [
			child("blockName"),
			child("heading", childType),
			{ ...child("links", "array"), nested: { fields: [child("label")] } },
			child("metadata", "json"),
		],
	};
}

function blocks(childType: SchemaField["type"] = "text"): SchemaField {
	return bindBlockField([hero(childType)], {
		...scalar("layout", "blocks"),
		category: "nested",
		blocks: { blockReferences: ["hero"] },
	});
}

function richText(): SchemaField {
	return bindBlockField([hero("text")], {
		...scalar("body", "plugin"),
		category: "plugin",
		plugin: {
			key: "richtext",
			config: {},
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
							blockReferences: ["hero"],
						},
					],
				},
			],
		},
	});
}

describe("field clipboard", () => {
	test("copies a named ordinary block with fresh owned identities", () => {
		const field = blocks();
		const payload = createFieldClipboardPayload(field, "row", {
			_key: "old-row",
			blockType: "hero",
			blockName: "Homepage hero",
			heading: "Copied hero",
			links: [{ _key: "old-link", label: "Read more" }, { label: "Keyless" }],
			metadata: { _key: "business-key", inner: [{ _key: "external" }] },
		});
		const pasted = compatibleClipboardValue(payload, field, "row") as Record<string, unknown>;
		expect(pasted.metadata).toEqual({ _key: "business-key", inner: [{ _key: "external" }] });
		expect((pasted.links as Record<string, unknown>[])[1]?._key).toBeString();
		expect(pasted.blockName).toBe("Homepage hero");
		expect(pasted.heading).toBe("Copied hero");
		expect(pasted._key).not.toBe("old-row");
		expect((pasted.links as Record<string, unknown>[])[0]?._key).not.toBe("old-link");
		expect(compatibleClipboardValue(payload, field, "field")).toBeUndefined();
		expect(compatibleClipboardValue(payload, blocks("number"), "row")).toBeUndefined();
	});

	test("copies a named rich-text block with fresh block and nested identities", () => {
		const field = richText();
		const source = {
			version: 1,
			root: {
				type: "root",
				children: [
					{
						type: "block",
						fields: {
							blockType: "hero",
							_key: "old-rich-block",
							blockName: "Release callout",
							heading: "Copied rich block",
							links: [{ _key: "old-rich-link", label: "Read release" }],
						},
					},
				],
			},
		};
		const payload = createFieldClipboardPayload(field, "field", source);
		const pasted = compatibleClipboardValue(payload, field, "field") as typeof source;
		const copiedFields = pasted.root.children[0]!.fields;

		expect(copiedFields.blockName).toBe("Release callout");
		expect(copiedFields.heading).toBe("Copied rich block");
		expect(copiedFields._key).not.toBe("old-rich-block");
		expect(copiedFields.links[0]!._key).not.toBe("old-rich-link");
		expect(source.root.children[0]!.fields._key).toBe("old-rich-block");
		expect(source.root.children[0]!.fields.links[0]!._key).toBe("old-rich-link");
	});

	test("includes row bounds in compatibility and rejects untrusted text", () => {
		const first = {
			...scalar("team", "array"),
			category: "nested" as const,
			nested: { fields: [scalar("blockName")], minRows: 1, maxRows: 3 },
		};
		const second = { ...first, nested: { ...first.nested, maxRows: 4 } };
		expect(fieldClipboardSignature(first)).not.toBe(fieldClipboardSignature(second));
		expect(parseFieldClipboardPayload("plain clipboard text")).toBeUndefined();
		expect(parseFieldClipboardPayload("ridu-field-clipboard:{broken")).toBeUndefined();
	});

	test("parses a bounded versioned envelope", () => {
		const payload = createFieldClipboardPayload(blocks(), "field", []);
		const encode = (candidate: unknown) => `ridu-field-clipboard:${JSON.stringify(candidate)}`;

		expect(parseFieldClipboardPayload(encode(payload))).toEqual(payload);
		expect(parseFieldClipboardPayload(encode({ ...payload, version: 2 }))).toBeUndefined();
		expect(parseFieldClipboardPayload(encode({ ...payload, kind: "column" }))).toBeUndefined();
		expect(parseFieldClipboardPayload(encode({ ...payload, signature: 42 }))).toBeUndefined();
		expect(
			parseFieldClipboardPayload(`ridu-field-clipboard:${"x".repeat(1_000_000)}`)
		).toBeUndefined();
	});
});
