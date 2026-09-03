import { describe, expect, test } from "bun:test";

import { contentCSSHash } from "../../packages/build/src/vite/compiler-options.ts";

describe("admin build reproducibility", () => {
	test("scopes component CSS independently of its checkout path", () => {
		const hash = (input: string) => input;
		const shared = { name: "Toaster", css: ".toast{display:block}", hash };

		expect(contentCSSHash({ ...shared, filename: "/workspace-a/Toaster.svelte" })).toBe(
			contentCSSHash({ ...shared, filename: "/workspace-b/Toaster.svelte" })
		);
		expect(
			contentCSSHash({
				...shared,
				filename: "/workspace-a/Toaster.svelte",
				css: ".toast{display:grid}",
			})
		).not.toBe(contentCSSHash({ ...shared, filename: "/workspace-a/Toaster.svelte" }));
	});
});
