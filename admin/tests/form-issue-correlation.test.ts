import { expect, test } from "bun:test";
import type { SchemaField } from "@riducms/protocol";
import {
	canonicalIssueTarget,
	correlateFormIssues,
	indexFieldValues,
} from "../src/core/forms/form-issue-correlation";

const sku: SchemaField = {
	id: "sku",
	name: "sku",
	path: "sections.products.sku",
	type: "text",
	category: "scalar",
	required: false,
	unique: false,
	admin: { label: "SKU" },
};
const products: SchemaField = {
	...sku,
	id: "products",
	name: "products",
	path: "sections.products",
	type: "array",
	category: "nested",
	nested: { fields: [sku] },
};
const sections: SchemaField = {
	...products,
	id: "sections",
	name: "sections",
	path: "sections",
	nested: { fields: [products] },
};
const fields = [sections];
const before = {
	sections: [
		{
			_key: "A",
			products: [
				{ _key: "B", sku: "invalid" },
				{ _key: "C", sku: "valid" },
			],
		},
		{ _key: "D", products: [] },
	],
};
const sourcePath = "sections.0.products.0.sku";
const target = indexFieldValues(fields, before).find(
	(location) => location.path === sourcePath
)!.token;

test("server targets correlate against submitted identities when server hooks reordered the candidate", () => {
	const after = {
		sections: [
			{ _key: "D", products: [] },
			{
				_key: "A",
				products: [
					{ _key: "C", sku: "valid" },
					{ _key: "B", sku: "invalid" },
				],
			},
		],
	};
	const issues = [{ code: "sku", message: "Reserved", path: "sections.0.products.1.sku", target }];
	expect(correlateFormIssues(fields, before, after, issues)).toEqual([
		{ ...issues[0], path: "sections.1.products.1.sku" },
	]);
	const deleted = { sections: [{ _key: "A", products: [{ _key: "C", sku: "valid" }] }] };
	expect(correlateFormIssues(fields, before, deleted, issues)).toEqual([]);
	const changed = structuredClone(before);
	changed.sections[0]!.products[0]!.sku = "fixed";
	expect(correlateFormIssues(fields, before, changed, issues)).toEqual([]);
	expect(
		correlateFormIssues(fields, before, before, [{ ...issues[0]!, target: "unknown" }])
	).toEqual([]);
});

test("display-path issues preserve identity through nested reorders and never target a replacement", () => {
	const after = {
		sections: [
			{ _key: "D", products: [] },
			{ _key: "A", products: [{ _key: "B", sku: "invalid" }] },
		],
	};
	const issue = { code: "sku", message: "Reserved", path: sourcePath };
	expect(correlateFormIssues(fields, before, after, [issue])).toEqual([
		{ ...issue, path: "sections.1.products.0.sku" },
	]);
	expect(
		correlateFormIssues(
			fields,
			before,
			{ sections: [{ _key: "new", products: [{ _key: "B", sku: "invalid" }] }] },
			[issue]
		)
	).toEqual([]);
});

test("Go-escaped Unicode issue targets retain exact keys and cannot alias literal backslash sequences", () => {
	for (const separator of ["\u2028", "\u2029"]) {
		const unicode = `A${separator}B`;
		const escaped = separator === "\u2028" ? "A\\u2028B" : "A\\u2029B";
		const submitted = {
			sections: [
				{ _key: unicode, products: [{ _key: "B", sku: "invalid" }] },
				{ _key: escaped, products: [{ _key: "B", sku: "invalid" }] },
			],
		};
		const current = { sections: [...submitted.sections].reverse() };
		const location = indexFieldValues(fields, submitted).find((item) => item.path === sourcePath)!;
		const goTarget = location.token.replaceAll("\u2028", "\\u2028").replaceAll("\u2029", "\\u2029");
		expect(goTarget).not.toBe(location.token);
		expect(canonicalIssueTarget(goTarget)).toBe(location.token);
		const issue = { code: "sku", path: sourcePath, message: "Reserved", target: goTarget };
		expect(correlateFormIssues(fields, submitted, current, [issue])).toEqual([
			{ ...issue, path: "sections.1.products.0.sku" },
		]);
		const literalTarget = indexFieldValues(fields, submitted).find(
			(item) => item.path === "sections.1.products.0.sku"
		)!.token;
		expect(canonicalIssueTarget(literalTarget)).toBe(literalTarget);
		expect(canonicalIssueTarget(literalTarget)).not.toBe(location.token);
	}
});

test("malformed occurrence targets never fall back to a matching display path", () => {
	for (const target of ["invalid", "null", "{}", '["sections", 1]', '["sections", {}]']) {
		expect(canonicalIssueTarget(target)).toBeUndefined();
		expect(
			correlateFormIssues(fields, before, before, [
				{ code: "sku", path: sourcePath, message: "Reserved", target },
			])
		).toEqual([]);
	}
});
