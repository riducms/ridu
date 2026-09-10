import { describe, expect, test } from "bun:test";
import type { SchemaField } from "@riducms/protocol";
import { validateFormValues } from "@admin/core/forms/form-validation";
import {
	initialFormValues,
	reconcileFormSchema,
	submissionFormValues,
} from "@admin/core/forms/form-schema";
import { primitiveNumberInput } from "@admin/fields/primitive-list/primitive-number-input";
import {
	filterOperatorsFor,
	buildListFilterWhere,
	sortableField,
} from "@admin/features/collections/list-workspace";

function list(type: "text-list" | "number-list", options: Partial<SchemaField> = {}): SchemaField {
	return {
		id: "values",
		name: "values",
		path: "values",
		type,
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Values" },
		...options,
	};
}
const validate = (field: SchemaField, values: Record<string, unknown>) =>
	validateFormValues([field], values, { requireMissing: true });

describe("primitive list form contract", () => {
	test("preserves order, duplicate entries, zero, whitespace and empty strings", () => {
		for (const [type, values] of [
			["text-list", ["", " x ", " x "]],
			["number-list", [0, -1, 0]],
		] as const) {
			const field = list(type);
			expect(validate(field, { values })).toEqual([]);
			expect(submissionFormValues([field], { values })).toEqual({ values });
		}
	});
	test("empty, omitted and null lists obey requiredness and minimum counts, with dynamic omission deferred", () => {
		for (const values of [{}, { values: null }, { values: [] }]) {
			expect(validate(list("text-list"), values)).toEqual([]);
			expect(validate(list("text-list", { required: true }), values)).toHaveLength(1);
			expect(validate(list("text-list", { list: { minRows: 2 } }), values)[0]?.code).toBe(
				"min_rows"
			);
		}
		expect(
			validate(
				list("number-list", { required: true, dynamicDefault: true, list: { minRows: 2 } }),
				{}
			)
		).toEqual([]);
		expect(
			validate(list("number-list", { required: true, dynamicDefault: true }), { values: [] })
		).toHaveLength(1);
	});
	test("rejects wrong containers and items with field-level one-based diagnostics", () => {
		for (const value of ["x", 1, {}, [null], [{}], [1], ["ok", false]]) {
			const issues = validate(list("text-list"), { values: value });
			expect(issues.length).toBeGreaterThan(0);
			expect(issues.every((issue) => issue.path === "values")).toBe(true);
		}
		for (const item of [null, {}, "", "-", "1e", Infinity, NaN]) {
			expect(validate(list("number-list"), { values: [0, item] })[0]).toMatchObject({
				path: "values",
				code: "invalid_number",
				message: "Values, item 2: enter a finite number",
			});
		}
	});
	test("applies Unicode character lengths and numeric bounds to each item and counts to the list", () => {
		expect(
			validate(list("text-list", { text: { maxLength: 1 } }), { values: ["🌳", "ab"] })
		).toMatchObject([{ path: "values", code: "max_length" }]);
		expect(
			validate(list("number-list", { number: { min: 0, max: 10 }, list: { maxRows: 2 } }), {
				values: [0, -1, 11],
			}).map((issue) => issue.code)
		).toEqual(["max_rows", "min_value", "max_value"]);
	});
	test("literal list defaults and reconciled values are detached; scalar-to-list is incompatible", () => {
		const field = list("text-list", { default: '["oak","oak",""]' });
		const first = initialFormValues([field]);
		(first.values as string[])[0] = "changed";
		expect(initialFormValues([field]).values).toEqual(["oak", "oak", ""]);
		const scalar = { ...field, type: "text" as const, default: undefined };
		const result = reconcileFormSchema(
			{ values: { values: "changed" }, original: { values: "prior" } },
			[scalar],
			[field]
		);
		expect(result.detached[0]?.reason).toBe("incompatible");
	});
	test("numeric entry retains unfinished input without coercion and preserves zero", () => {
		for (const text of ["", "-", "+", "1.", "1e", "1e-", " ", "1e999", "NaN"])
			expect(primitiveNumberInput(text)).toBe(text);
		for (const [text, value] of [
			["0", 0],
			["-2.5", -2.5],
			["1e2", 100],
			[".5", 0.5],
		] as const)
			expect(primitiveNumberInput(text)).toBe(value);
	});
	test("list filters offer item membership and presence rather than scalar sorting or equality", () => {
		for (const type of ["text-list", "number-list"] as const) {
			const field = list(type);
			expect(filterOperatorsFor(field)).toEqual(["in", "exists"]);
			expect(sortableField(field)).toBe(false);
			expect(
				buildListFilterWhere([{ field: "values", operator: "equals", value: "1" }], [field])
			).toEqual([]);
			expect(
				buildListFilterWhere([{ field: "values", operator: "in", value: "1" }], [field])
			).toEqual([{ values: { in: [type === "text-list" ? "1" : 1] } }]);
		}
	});
});
