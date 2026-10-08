import { describe, expect, it } from "bun:test";

import { filterRichTextOptions } from "../src/lib/menu/rich-text-option-filter";

function option(label: string) {
	return { label, description: "", keywords: [] };
}

describe("rich-text option filtering", () => {
	it("does not turn punctuation-only compact queries into match-all searches", () => {
		const options = [option("Paragraph"), option("Numbered list")];

		expect(filterRichTextOptions(options, "-", "en")).toEqual([]);
		expect(filterRichTextOptions(options, "_", "en")).toEqual([]);
		expect(filterRichTextOptions(options, "numberedlist", "en")).toEqual([options[1]]);
	});
});
