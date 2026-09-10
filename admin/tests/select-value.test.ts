import { describe, expect, it } from "bun:test";

import {
	removeSelectValue,
	selectOptionLabel,
	selectManyValues,
} from "../src/fields/select/select-value";

describe("multi-select values", () => {
	it("preserves authored order while discarding values the control cannot render", () => {
		expect(selectManyValues(["editor", 42, "admin", null])).toEqual(["editor", "admin"]);
		expect(selectManyValues(["admin", "admin"])).toEqual(["admin", "admin"]);
		expect(selectManyValues("admin")).toEqual([]);
	});

	it("removes a selected value without reordering the remaining values", () => {
		expect(removeSelectValue(["editor", "admin", "reviewer"], "admin")).toEqual([
			"editor",
			"reviewer",
		]);
	});

	it("uses the authored option label with an exact-value fallback", () => {
		const options = [
			{ value: "admin", label: "Administrator" },
			{ value: "editor", label: "Editor" },
		];
		expect(selectOptionLabel(options, "admin")).toBe("Administrator");
		expect(selectOptionLabel(options, "unknown")).toBe("unknown");
	});
});
