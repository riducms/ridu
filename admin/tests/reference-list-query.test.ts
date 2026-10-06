import { expect, test } from "bun:test";
import type { SchemaBlockType, SchemaField } from "@riducms/protocol";
import { createAdminI18n } from "@riducms/translations";
import { ListFilterFields } from "@admin/features/collections/list-filter-fields";
import { referenceListWhere } from "@admin/features/reference-browser/reference-list-query";

import { bindBlockFields, blockDefinition } from "./block-manifest";

const fields = new ListFilterFields({
	fields: [
		{ name: "title", path: "title", type: "text", admin: { label: "Title" } },
		{ name: "count", path: "count", type: "number", admin: { label: "Count" } },
		{ name: "featured", path: "featured", type: "checkbox", admin: { label: "Featured" } },
		{ name: "tags", path: "tags", type: "text-list", admin: { label: "Tags" } },
	] as SchemaField[],
	metadata: [],
	i18n: createAdminI18n(),
});

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

test("reference filters send membership candidates and negate them for is none of", () => {
	const membershipFields = new ListFilterFields({
		fields: [
			{ name: "sizes", path: "sizes", type: "number-list", admin: { label: "Sizes" } },
			{
				name: "authors",
				path: "authors",
				type: "relationship",
				admin: { label: "Authors" },
				relationship: { collectionSlug: "people", hasMany: true, onDelete: "nullify" },
			},
			{
				name: "subject",
				path: "subject",
				type: "relationship",
				admin: { label: "Subject" },
				relationship: {
					polymorphic: true,
					targets: [{ collectionId: "people", collectionSlug: "people" }],
					onDelete: "nullify",
				},
			},
		] as SchemaField[],
		metadata: [],
		i18n: createAdminI18n(),
	});
	expect(
		referenceListWhere(
			[
				[
					{ field: "sizes", operator: "in", value: ["0", "8.5"] },
					{ field: "authors", operator: "notIn", value: ["ada"] },
					{ field: "subject", operator: "in", value: ["people:ada"] },
					{ field: "subject", operator: "in", value: ["people:"] },
					{ field: "authors", operator: "equals", value: "ada" },
				],
			],
			membershipFields
		)
	).toEqual({
		and: [
			{ sizes: { in: [0, 8.5] } },
			{ not: { authors: { in: ["ada"] } } },
			{ subject: { in: [{ relationTo: "people", id: "ada" }] } },
		],
	});
});

test("reference filters keep block-qualified paths exactly as the REST where contract names them", () => {
	const note = blockDefinition(
		"note",
		[{ name: "body", type: "textarea", admin: { label: "Body" } } as SchemaField],
		{ singular: "Note", plural: "Notes" }
	);
	const hero = blockDefinition(
		"hero",
		[
			{ name: "rank", type: "number", admin: { label: "Rank" } } as SchemaField,
			{
				name: "children",
				type: "blocks",
				admin: { label: "Children" },
				blocks: { blockReferences: ["note"] },
			} as SchemaField,
		],
		{ singular: "Hero", plural: "Heroes" }
	);
	const layout = bindBlockFields([note, hero] satisfies SchemaBlockType[], [
		{
			name: "layout",
			path: "layout",
			type: "blocks",
			admin: { label: "Layout" },
			blocks: { blockReferences: ["hero"] },
		} as SchemaField,
	]);
	const blocks = new ListFilterFields({ fields: layout, metadata: [], i18n: createAdminI18n() });
	expect(
		referenceListWhere(
			[
				[
					{ field: "layout.hero.rank", operator: "greaterThan", value: "2" },
					{ field: "layout.hero.children.note.body", operator: "like", value: "launch" },
				],
				// Containers and block types alone are not filterable leaves.
				[{ field: "layout.hero.children", operator: "exists", value: "true" }],
				[{ field: "layout.hero", operator: "exists", value: "true" }],
			],
			blocks
		)
	).toEqual({
		and: [
			{ "layout.hero.rank": { greaterThan: 2 } },
			{ "layout.hero.children.note.body": { like: "launch" } },
		],
	});
});
