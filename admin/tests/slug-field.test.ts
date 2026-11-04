import { describe, expect, it } from "bun:test";

import { slugFollowsSource } from "../src/fields/text/slug";

describe("slug field", () => {
	it("infers generated versus manual values without persisted UI state", () => {
		expect(slugFollowsSource("hello-world", "Hello World")).toBe(true);
		expect(slugFollowsSource("hand-authored", "Hello World")).toBe(false);
	});
});
