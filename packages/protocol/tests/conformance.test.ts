import { describe, expect, it } from "bun:test";

import { isErrorEnvelope, isValidationIssue } from "../src";

describe("Go protocol conformance fixtures", () => {
	it("validates optional resolved issue metadata without trusting TypeScript types", () => {
		const issue = { code: "url", path: "sections.0.links.0.url", message: "Choose a URL" };
		expect(isValidationIssue(issue)).toBe(true);
		for (const key of ["target", "fieldId", "collectionId", "globalId", "locale"]) {
			expect(isValidationIssue({ ...issue, [key]: "identity" })).toBe(true);
			for (const value of [null, 1, [], {}])
				expect(isValidationIssue({ ...issue, [key]: value })).toBe(false);
		}
	});
	it("decodes the checked Go error envelope", async () => {
		const fixture = await Bun.file(
			new URL("../../../testdata/protocol/error.json", import.meta.url)
		).json();
		expect(isErrorEnvelope(fixture)).toBe(true);
	});

	it("rejects an unstable error code", () => {
		expect(
			isErrorEnvelope({
				error: {
					code: "surprise",
					status: 500,
					message: "No",
					issues: [],
				},
			})
		).toBe(false);
	});
});
