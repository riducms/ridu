import { describe, expect, it } from "bun:test";

import { hasRichTextFeature, parseRichTextConfig } from "../src/field/rich-text-config";

describe("rich-text configuration", () => {
	it("accepts only supported features and string reference collections", () => {
		const config = parseRichTextConfig({
			features: ["links", "not-a-feature", 42, "uploads"],
			uploadCollections: ["media", null, "documents"],
			relationshipCollections: ["posts", false, "pages"],
		});

		expect(config).toEqual({
			features: ["links", "uploads"],
			uploadCollections: ["media", "documents"],
			relationshipCollections: ["posts", "pages"],
		});
		expect(hasRichTextFeature(config, "links")).toBe(true);
		expect(hasRichTextFeature(config, "code")).toBe(false);
	});

	it("uses an empty configuration for malformed input", () => {
		expect(parseRichTextConfig(null)).toEqual({
			features: [],
			uploadCollections: [],
			relationshipCollections: [],
		});
		expect(parseRichTextConfig([])).toEqual({
			features: [],
			uploadCollections: [],
			relationshipCollections: [],
		});
	});
});
