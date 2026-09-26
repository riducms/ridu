import { expect, test } from "bun:test";
import type { SchemaField } from "@riducms/protocol";
import { referenceListWhere } from "@admin/features/reference-browser/reference-list-query";

const fields = [
	{ name: "title", path: "title", type: "text", admin: { label: "Title" } },
	{ name: "count", path: "count", type: "number", admin: { label: "Count" } },
	{ name: "featured", path: "featured", type: "checkbox", admin: { label: "Featured" } },
	{ name: "tags", path: "tags", type: "text-list", admin: { label: "Tags" } },
] as SchemaField[];

test("reference filters preserve OR groups, AND conditions, and typed false/zero operands", () => {
	expect(
		referenceListWhere(
			[
				[
					{ field: "count", operator: "greaterThanEqual", value: "0" },
					{ field: "featured", operator: "equals", value: "false" },
				],
				[{ field: "title", operator: "exists", value: "false" }],
			],
			fields
		)
	).toEqual({
		or: [
			{ and: [{ count: { greaterThanEqual: 0 } }, { featured: { equals: false } }] },
			{ title: { exists: false } },
		],
	});
});

test("reference filters omit incomplete or obsolete conditions without widening populated groups", () => {
	expect(
		referenceListWhere(
			[
				[{ field: "count", operator: "equals", value: "not a number" }],
				[{ field: "missing", operator: "equals", value: "obsolete" }],
				[{ field: "title", operator: "like", value: "" }],
				[{ field: "tags", operator: "in", value: "editorial" }],
			],
			fields
		)
	).toEqual({ tags: { in: ["editorial"] } });
});
