import { describe, expect, it, mock } from "bun:test";

// Registry tests keep UI bodies inert; Svelte and browser suites compile/render the actual components.
for (const file of [
	"overview-field",
	"preview-field",
	"meta-title-field",
	"meta-description-field",
	"meta-image-field",
])
	mock.module(`../src/lib/${file}.svelte`, () => ({ default: () => ({}) }));
const { generationScopeToken, generationSnapshotToken, lengthState, seoMessages } =
	await import("../src/lib");

describe("SEO admin contract", () => {
	it("matches the inclusive Payload length guidance boundaries", () => {
		expect(lengthState("", 50, 60)).toMatchObject({ status: "missing", progress: 0 });
		expect(lengthState("x".repeat(45), 50, 60).status).toBe("tooShort");
		expect(lengthState("x".repeat(46), 50, 60).status).toBe("almostThere");
		expect(lengthState("x".repeat(50), 50, 60)).toMatchObject({
			status: "good",
			remaining: 10,
		});
		expect(lengthState("x".repeat(60), 50, 60)).toMatchObject({
			status: "good",
			remaining: 0,
		});
		expect(lengthState("x".repeat(61), 50, 60)).toMatchObject({
			status: "tooLong",
			remaining: 1,
		});
	});

	it("distinguishes draft, resource and locale snapshot identities", () => {
		const submitted = generationSnapshotToken({ title: "Before", nested: { live: true } });
		expect(generationSnapshotToken({ title: "Before", nested: { live: true } })).toBe(submitted);
		expect(generationSnapshotToken({ title: "After", nested: { live: true } })).not.toBe(submitted);
		const scope = generationScopeToken({ collection: "pages", id: "one" }, "en");
		expect(generationScopeToken({ collection: "pages", id: "one" }, "en")).toBe(scope);
		expect(generationScopeToken({ collection: "pages", id: "one" }, "fr")).not.toBe(scope);
		expect(generationScopeToken({ collection: "pages", id: "two" }, "en")).not.toBe(scope);
	});

	it("keeps localized length guidance composable with the best-practices link", () => {
		for (const language of ["fr", "ar"] as const) {
			const messages = seoMessages.translations?.[language];
			expect(messages?.lengthTipTitle).toEndWith(" ");
			expect(messages?.lengthTipDescription).toEndWith(" ");
			expect(messages?.lengthTipTitle).not.toMatch(/[.!؟]\s$/u);
			expect(messages?.lengthTipDescription).not.toMatch(/[.!؟]\s$/u);
		}
	});
});
