import type { SchemaField } from "@riducms/protocol";
import { describe, expect, test } from "bun:test";

import {
	compatibleClipboardValue,
	createFieldClipboardPayload,
	fieldClipboardSignature,
	parseFieldClipboardPayload,
} from "@admin/fields/field-clipboard";

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

function blocks(childType: SchemaField["type"] = "text"): SchemaField {
	return {
		...scalar("layout", "blocks"),
		category: "nested",
		blocks: {
			types: [
				{
					slug: "hero",
					labels: { singular: "Hero", plural: "Heroes" },
					fields: [
						scalar("heading", childType),
						{ ...scalar("links", "array"), nested: { fields: [scalar("label")] } },
						scalar("metadata", "json"),
					],
				},
			],
		},
	};
}

describe("field clipboard", () => {
	test("accepts only the matching field shape and clipboard kind", () => {
		const field = blocks();
		const payload = createFieldClipboardPayload(field, "row", {
			_key: "old-row",
			blockType: "hero",
			heading: "Copied hero",
			links: [{ _key: "old-link", label: "Read more" }, { label: "Keyless" }],
			metadata: { _key: "business-key", inner: [{ _key: "external" }] },
		});
		const pasted = compatibleClipboardValue(payload, field, "row") as Record<string, unknown>;
		expect(pasted.metadata).toEqual({ _key: "business-key", inner: [{ _key: "external" }] });
		expect((pasted.links as Record<string, unknown>[])[1]?._key).toBeString();
		expect(pasted.heading).toBe("Copied hero");
		expect(pasted._key).not.toBe("old-row");
		expect((pasted.links as Record<string, unknown>[])[0]?._key).not.toBe("old-link");
		expect(compatibleClipboardValue(payload, field, "field")).toBeUndefined();
		expect(compatibleClipboardValue(payload, blocks("number"), "row")).toBeUndefined();
	});

	test("includes row bounds in compatibility and rejects untrusted text", () => {
		const first = {
			...scalar("team", "array"),
			category: "nested" as const,
			nested: { fields: [scalar("name")], minRows: 1, maxRows: 3 },
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
