import { describe, expect, it } from "bun:test";

import { normalizeSlug } from "../src";

describe("slug normalization", () => {
	it("matches the Go normalizer on the shared fixture", async () => {
		const cases: { input: string; slug: string }[] = await Bun.file(
			new URL("../../../testdata/protocol/slug.json", import.meta.url)
		).json();
		expect(cases.length).toBeGreaterThan(0);
		for (const { input, slug } of cases)
			expect([input, normalizeSlug(input)]).toEqual([input, slug]);
	});
});
