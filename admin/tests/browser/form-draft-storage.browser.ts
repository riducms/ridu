import { expect, it } from "vitest";
import {
	bindSchemaManifest,
	resolveBlockTypes,
	type SchemaBlockType,
	type SchemaCollection,
	type SchemaField,
} from "@riducms/protocol";

import {
	clearFormDraft,
	peekFormDraft,
	saveFormDraft,
} from "@admin/core/forms/form-draft-recovery";
import { reconcileFormSchema, recoverFormDraft } from "@admin/core/forms/form-schema";

const text = (name: string): SchemaField => ({
	id: name,
	name,
	path: name,
	type: "text",
	category: "scalar",
	required: false,
	unique: false,
	admin: { label: name },
	text: {},
});
const collection: SchemaCollection = {
	id: "checkpoint-storage",
	slug: "checkpoint-storage",
	labels: { singular: "Checkpoint", plural: "Checkpoints" },
	admin: {},
	capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
	fields: [text("title")],
};

it("keeps create checkpoints separate from a document whose ID is new", () => {
	try {
		expect(saveFormDraft(collection, undefined, { title: "Create draft" }, {})).toBe(true);
		expect(saveFormDraft(collection, "new", { title: "Document draft" }, {})).toBe(true);
		expect(peekFormDraft(collection.id, undefined)?.values.title).toBe("Create draft");
		expect(peekFormDraft(collection.id, "new")?.values.title).toBe("Document draft");
		clearFormDraft(collection.id, "new");
		expect(peekFormDraft(collection.id, "new")).toBeUndefined();
		expect(peekFormDraft(collection.id, undefined)?.values.title).toBe("Create draft");
	} finally {
		clearFormDraft(collection.id, undefined);
		clearFormDraft(collection.id, "new");
	}
});

it("keeps unset locales and separator-containing identities distinct", () => {
	const first = { ...collection, id: "checkpoint:storage" };
	const second = { ...collection, id: "checkpoint" };
	try {
		saveFormDraft(collection, "one", { title: "No locale" }, {});
		saveFormDraft(collection, "one", { title: "Named default locale" }, {}, {}, "default");
		saveFormDraft(first, "one", { title: "First identity" }, {}, {}, "en");
		saveFormDraft(second, "storage:one", { title: "Second identity" }, {}, {}, "en");
		expect(peekFormDraft(collection.id, "one")?.values.title).toBe("No locale");
		expect(peekFormDraft(collection.id, "one", "default")?.values.title).toBe(
			"Named default locale"
		);
		expect(peekFormDraft(first.id, "one", "en")?.values.title).toBe("First identity");
		expect(peekFormDraft(second.id, "storage:one", "en")?.values.title).toBe("Second identity");
	} finally {
		clearFormDraft(collection.id, "one");
		clearFormDraft(collection.id, "one", "default");
		clearFormDraft(first.id, "one", "en");
		clearFormDraft(second.id, "storage:one", "en");
	}
});

it("round-trips compact shared blocks and binds saved placement schemas before recovery", () => {
	const blocks: SchemaBlockType[] = [
		{
			slug: "card",
			labels: { singular: "Card", plural: "Cards" },
			fields: [
				text("title"),
				{
					...text("children"),
					type: "blocks",
					category: "nested",
					blocks: { blockReferences: ["note"] },
				},
			],
		},
		{ slug: "note", labels: { singular: "Note", plural: "Notes" }, fields: [text("message")] },
	];
	const schema = {
		...collection,
		fields: [
			{
				...text("layout"),
				type: "blocks" as const,
				category: "nested" as const,
				blocks: { blockReferences: ["card"] },
			},
			{
				...text("body"),
				type: "plugin" as const,
				category: "plugin" as const,
				plugin: {
					key: "outline",
					config: {},
					embeddedTrees: [
						{
							version: 1 as const,
							key: "widgets",
							root: ["outline"],
							children: "items",
							tag: "kind",
							cases: [
								{
									tagValue: "widget",
									payload: "content",
									discriminator: "schema",
									identity: "uid",
									blockReferences: ["card"],
								},
							],
						},
					],
				},
			},
		],
	} satisfies SchemaCollection;
	bindSchemaManifest({ collections: [schema], globals: [], blocks });
	const original = { layout: [{ _key: "one", blockType: "card", title: "Saved title" }] };
	const values = { layout: [{ ...original.layout[0], title: "Recovered title" }] };
	try {
		expect(saveFormDraft(schema, "one", values, original, {}, "en", blocks)).toBe(true);
		const checkpoint = peekFormDraft(schema.id, "one", "en");
		expect(checkpoint).toBeDefined();
		if (checkpoint === undefined) throw new Error("Expected a shared-block checkpoint");
		expect(checkpoint.blocks).toEqual(blocks);
		const [layout, body] = checkpoint.collection.fields;
		expect(JSON.stringify(layout!.blocks)).toBe('{"blockReferences":["card"]}');
		const card = resolveBlockTypes(layout!.blocks)[0]!;
		expect(card.fields[0]!.path).toBe("layout.card.title");
		expect(resolveBlockTypes(card.fields[1]!.blocks)[0]!.fields[0]!.path).toBe(
			"layout.card.children.note.message"
		);
		const embeddedCard = resolveBlockTypes(body!.plugin!.embeddedTrees![0]!.cases[0])[0]!;
		expect(embeddedCard.fields[0]!.path).toBe("body.widgets.widget.card.title");
		expect(
			recoverFormDraft(
				{ values: original, original },
				reconcileFormSchema(checkpoint, checkpoint.collection.fields, schema.fields),
				schema.fields
			).values
		).toEqual(values);
	} finally {
		clearFormDraft(schema.id, "one", "en");
	}
});

it("ignores a checkpoint with unresolved shared block definitions", () => {
	const schema: SchemaCollection = {
		...collection,
		fields: [
			{
				...text("layout"),
				type: "blocks",
				category: "nested",
				blocks: { blockReferences: ["missing"] },
			},
		],
	};
	try {
		saveFormDraft(schema, "one", {}, {});
		expect(peekFormDraft(schema.id, "one")).toBeUndefined();
	} finally {
		clearFormDraft(schema.id, "one");
	}
});
