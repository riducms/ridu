import { expect, it } from "vitest";
import {
	bindSchemaManifest,
	cloneSchemaField,
	resolveBlockTypes,
	type SchemaBlockType,
	type SchemaField,
} from "@riducms/protocol";

it("shares browser schema metadata while extension snapshots retain isolated placement views", async ({
	annotate,
}) => {
	const text = (name: string): SchemaField => ({
		id: `block-card-${name}`,
		name,
		path: name,
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: name },
		text: {},
	});
	const definition: SchemaBlockType = {
		slug: "card",
		labels: { singular: "Card", plural: "Cards" },
		fields: Array.from({ length: 40 }, (_, i) => text(`text${i}`)),
	};
	const fields = Array.from({ length: 100 }, (_, i): SchemaField => ({
		...text(`layout${i}`),
		id: `pages-layout${i}`,
		type: "blocks",
		category: "nested",
		blocks: { blockReferences: ["card"] },
	}));
	const manifest = {
		blocks: [definition],
		globals: [],
		collections: [
			{
				id: "pages",
				slug: "pages",
				labels: { singular: "Page", plural: "Pages" },
				fields,
				admin: {},
				capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
			},
		],
	};
	const memory = performance as Performance & { memory?: { usedJSHeapSize: number } };
	const before = memory.memory?.usedJSHeapSize;
	bindSchemaManifest(manifest);
	const children = fields.flatMap((field) => resolveBlockTypes(field.blocks)[0]!.fields);
	const afterReferences = memory.memory?.usedJSHeapSize;
	const inline = fields.map((field) => ({
		...field,
		blocks: {
			types: [
				{
					...definition,
					fields: definition.fields.map((child) => ({
						...structuredClone(child),
						path: `${field.path}.card.${child.path}`,
						id: `${field.id}-card-${child.name}`,
					})),
				},
			],
		},
	}));
	const inlineChildren = inline.flatMap((field) => resolveBlockTypes(field.blocks)[0]!.fields);
	expect(new Set(inlineChildren.map((child) => child.text)).size).toBe(4000);
	expect(new Set(children.map((child) => child.text)).size).toBe(40);
	expect(new Set(children.map((child) => child.path)).size).toBe(4000);
	const detached = cloneSchemaField(fields[0]!);
	const clonedChildren = resolveBlockTypes(detached.blocks)[0]!.fields;
	clonedChildren[0]!.admin.label = "Extension-owned change";
	expect(children[0]!.admin.label).toBe("text0");
	expect(clonedChildren[0]!.path).toBe("layout0.card.text0");
	await annotate(
		JSON.stringify({
			placements: 100,
			fieldViews: children.length,
			sharedTextMetadata: 40,
			inlineTextMetadata: 4000,
			manifestBytes: JSON.stringify(manifest).length,
			heapBefore: before,
			heapAfterReferences: afterReferences,
			heapAfterInline: memory.memory?.usedJSHeapSize,
		}),
		"schema-memory"
	);
});
