import { describe, expect, test } from "bun:test";
import type { SchemaFieldCondition } from "@riducms/protocol";

import { evaluateFieldCondition, fieldConditionValuePath } from "@admin/core/forms/field-condition";

describe("field condition expressions", () => {
	test("evaluates typed all, any, not, equals, and oneOf nodes", () => {
		const values = {
			status: "published",
			sections: [{}, { points: 3, enabled: false }],
		};
		const condition: SchemaFieldCondition = {
			kind: "all",
			conditions: [
				{
					kind: "predicate",
					predicate: {
						scope: "document",
						path: "status",
						operator: "oneOf",
						values: [
							{ type: "string", value: "draft" },
							{ type: "string", value: "published" },
						],
					},
				},
				{
					kind: "any",
					conditions: [
						{
							kind: "not",
							conditions: [
								{
									kind: "predicate",
									predicate: {
										scope: "sibling",
										path: "enabled",
										operator: "equals",
										values: [{ type: "boolean", value: "false" }],
									},
								},
							],
						},
						{
							kind: "predicate",
							predicate: {
								scope: "sibling",
								path: "points",
								operator: "equals",
								values: [{ type: "number", value: "3" }],
							},
						},
					],
				},
			],
		};
		const before = JSON.stringify(values);

		expect(evaluateFieldCondition(condition, "sections.1.answer", read(values))).toBe(true);
		expect(JSON.stringify(values)).toBe(before);
	});

	test("keeps document and current-row sibling scopes distinct", () => {
		const values = {
			mode: "document",
			sections: [{ mode: "first" }, { mode: "second" }],
		};
		const document: SchemaFieldCondition = predicate("document", "mode", "document");
		const sibling: SchemaFieldCondition = predicate("sibling", "mode", "second");

		expect(evaluateFieldCondition(document, "sections.1.answer", read(values))).toBe(true);
		expect(evaluateFieldCondition(sibling, "sections.1.answer", read(values))).toBe(true);
		expect(fieldConditionValuePath("sibling", "mode", "rootField")).toBe("mode");
		expect(fieldConditionValuePath("sibling", "meta.mode", "sections.1.answer")).toBe(
			"sections.1.meta.mode"
		);
	});

	test("uses strict scalar types and treats a missing value as not equal", () => {
		const values = { sections: [{ points: "3" }] };
		const number: SchemaFieldCondition = {
			kind: "predicate",
			predicate: {
				scope: "sibling",
				path: "points",
				operator: "equals",
				values: [{ type: "number", value: "3" }],
			},
		};
		const missing: SchemaFieldCondition = {
			kind: "predicate",
			predicate: {
				scope: "sibling",
				path: "state",
				operator: "notEquals",
				values: [{ type: "string", value: "archived" }],
			},
		};

		expect(evaluateFieldCondition(number, "sections.0.answer", read(values))).toBe(false);
		expect(evaluateFieldCondition(missing, "sections.0.answer", read(values))).toBe(true);
	});
});

function predicate(
	scope: "document" | "sibling",
	path: string,
	value: string
): SchemaFieldCondition {
	return {
		kind: "predicate",
		predicate: { scope, path, operator: "equals", values: [{ type: "string", value }] },
	};
}

function read(values: Record<string, unknown>) {
	return (path: string): unknown => {
		let current: unknown = values;
		for (const segment of path.split(".")) {
			if (Array.isArray(current)) current = current[Number(segment)];
			else if (typeof current === "object" && current !== null)
				current = (current as Record<string, unknown>)[segment];
			else return undefined;
		}
		return current;
	};
}
