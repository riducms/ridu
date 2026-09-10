import { describe, expect, it } from "bun:test";

import { hasRichTextFeature, decodeRichTextConfig } from "../src/field/rich-text-config";

describe("rich-text configuration", () => {
	it("accepts only supported features and string reference collections", () => {
		const config = decodeRichTextConfig({
			features: ["links", "uploads"],
			uploadCollections: ["media", "documents"],
			relationshipCollections: ["posts", "pages"],
		});

		expect(config).toEqual({
			features: ["links", "uploads"],
			uploadCollections: ["media", "documents"],
			relationshipCollections: ["posts", "pages"],
		});
		expect(hasRichTextFeature(config, "links")).toBe(true);
		expect(hasRichTextFeature(config, "code")).toBe(false);
	});

	it("rejects malformed and unsupported serialized config", () => {
		for (const invalid of [
			null,
			[],
			{ features: ["not-a-feature"] },
			{ features: [42] },
			{ uploadCollections: [null] },
			{ unknown: true },
		])
			expect(() => decodeRichTextConfig(invalid)).toThrow();
		expect(decodeRichTextConfig({})).toEqual({
			features: [],
			uploadCollections: [],
			relationshipCollections: [],
		});
	});
});
