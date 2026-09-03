import { describe, expect, it } from "bun:test";

import { isErrorEnvelope } from "../src";

describe("Go protocol conformance fixtures", () => {
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
