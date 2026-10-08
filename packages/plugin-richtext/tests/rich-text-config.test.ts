import { describe, expect, it } from "bun:test";

import { hasRichTextFeature, decodeRichTextConfig } from "../src/lib/field/rich-text-config";

const defaultAdmin = {
	fixedToolbar: false,
	hideGutter: false,
	hideDraggableBlockElement: false,
	hideAddBlockButton: false,
	hideInsertParagraphAtEnd: false,
};

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
			admin: defaultAdmin,
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
			{ admin: null },
			{ admin: { hideGutter: "yes" } },
			{ admin: { toolbar: "fixed" } },
		])
			expect(() => decodeRichTextConfig(invalid)).toThrow();
		expect(decodeRichTextConfig({})).toEqual({
			features: [],
			uploadCollections: [],
			relationshipCollections: [],
			admin: defaultAdmin,
		});
	});

	it("applies admin options over their defaults", () => {
		expect(decodeRichTextConfig({ admin: { fixedToolbar: true, hideGutter: true } }).admin).toEqual(
			{ ...defaultAdmin, fixedToolbar: true, hideGutter: true }
		);
	});
});
