import { describe, expect, it } from "bun:test";

import { normalizeSlug, slugFollowsSource } from "../src/fields/text/slug";

describe("slug field", () => {
	it("matches the server normalization contract", () => {
		expect(normalizeSlug("  Hello,  WORLD!  ")).toBe("hello-world");
		expect(normalizeSlug("already--slugged")).toBe("already-slugged");
		expect(normalizeSlug("snake_case")).toBe("snake_case");
		expect(normalizeSlug("Café & Tea")).toBe("caf-tea");
		expect(normalizeSlug("---")).toBe("");
	});

	it("infers generated versus manual values without persisted UI state", () => {
		expect(slugFollowsSource("hello-world", "Hello World")).toBe(true);
		expect(slugFollowsSource("hand-authored", "Hello World")).toBe(false);
	});
});
