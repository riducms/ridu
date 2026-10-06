import { describe, expect, test } from "bun:test";
import {
	bindSchemaManifest,
	blockDefinitionPath,
	mapBlockTypes,
	resolveBlockTypes,
} from "./schema-registry.js";
import type {
	SchemaBlockType,
	SchemaCollection,
	SchemaField,
	SchemaManifest,
} from "./generated.js";

const text = (name = "title"): SchemaField => ({
	id: `block-card-${name}`,
	name,
	path: name,
	type: "text",
	category: "scalar",
	required: false,
	unique: false,
	localized: true,
	admin: { label: "Title", row: { id: "block-card-title" } },
	text: {},
});
const block = (): SchemaBlockType => ({
	slug: "card",
	typeName: "Card",
	labels: { singular: "Card", plural: "Cards" },
	fields: [text()],
});
const field = (path: string, refs = ["card"]): SchemaField => ({
	...text(path),
	id: `pages-${path}`,
	localized: path === "localized",
	type: "blocks",
	category: "nested",
	blocks: { blockReferences: refs },
});
const resource = (fields: SchemaField[], id = "pages"): SchemaCollection => ({
	id,
	slug: id,
	fields,
	labels: { singular: id, plural: id },
	capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
	admin: {},
});
function manifest(fields = [field("layout"), field("localized")], blocks = [block()]) {
	return { collections: [resource(fields)], globals: [], blocks } satisfies Pick<
		SchemaManifest,
		"collections" | "globals" | "blocks"
	>;
}

describe("compact block registry", () => {
	test("binds detached placement identities and inherited localization without changing the wire tree", () => {
		const schema = manifest();
		const wire = JSON.stringify(schema);
		bindSchemaManifest(schema);
		const [layout, localized] = schema.collections[0]!.fields;
		const first = resolveBlockTypes(layout!.blocks)[0]!;
		const second = resolveBlockTypes(localized!.blocks)[0]!;
		expect(first.fields[0]).toMatchObject({
			id: "pages-layout-card-title",
			path: "layout.card.title",
			localized: true,
			admin: { row: { id: "pages-layout-card-title" } },
		});
		expect(second.fields[0]).toMatchObject({
			id: "pages-localized-card-title",
			path: "localized.card.title",
			localized: false,
			admin: { row: { id: "pages-localized-card-title" } },
		});
		expect(resolveBlockTypes(layout!.blocks)[0]).toBe(first);
		expect(first).not.toBe(second);
		expect(first.fields[0]!.text).toBe(second.fields[0]!.text);
		expect(JSON.stringify(schema)).toBe(wire);
	});
	test("metadata projections stay lazy and preserve compact references", () => {
		const schema = bindSchemaManifest(manifest());
		let visits = 0;
		const projected = mapBlockTypes(schema.collections[0]!.fields[0]!.blocks!, (block) => {
			visits++;
			return { ...block, labels: { ...block.labels, singular: "Translated" } };
		});
		expect(visits).toBe(0);
		expect(JSON.stringify(projected)).toBe('{"blockReferences":["card"]}');
		expect(resolveBlockTypes(projected)[0]!.labels.singular).toBe("Translated");
		expect(visits).toBe(1);
	});
	test("a placement cannot mutate shared definition metadata", () => {
		const schema = bindSchemaManifest(manifest());
		const first = resolveBlockTypes(schema.collections[0]!.fields[0]!.blocks)[0]!;
		expect(Object.isFrozen(first.fields[0]!.text)).toBe(true);
		expect(() => {
			first.fields[0]!.text!.minLength = 20;
		}).toThrow();
		expect(schema.blocks[0]!.fields[0]!.text!.minLength).toBeUndefined();
	});
	test("forward and nested references resolve in picker order", () => {
		const leaf = { ...block(), slug: "leaf", fields: [{ ...text(), id: "block-leaf-title" }] };
		const card = { ...block(), fields: [field("children", ["leaf"])] };
		const schema = bindSchemaManifest(manifest([field("layout", ["leaf", "card"])], [card, leaf]));
		const types = resolveBlockTypes(schema.collections[0]!.fields[0]!.blocks);
		expect(types.map((t) => t.slug)).toEqual(["leaf", "card"]);
		expect(resolveBlockTypes(types[1]!.fields[0]!.blocks)[0]!.fields[0]!.path).toBe(
			"layout.card.children.leaf.title"
		);
	});
	test("locates a canonical path's innermost block definition", () => {
		const leaf = { ...block(), slug: "leaf", fields: [{ ...text(), id: "block-leaf-title" }] };
		const card = { ...block(), fields: [field("children", ["leaf"]), text("caption")] };
		const schema = bindSchemaManifest(manifest([field("layout", ["leaf", "card"])], [card, leaf]));
		const fields = schema.collections[0]!.fields;
		expect(blockDefinitionPath(fields, "layout.card.caption")).toEqual({
			block: "card",
			path: "caption",
		});
		expect(blockDefinitionPath(fields, "layout.card.children.leaf.title")).toEqual({
			block: "leaf",
			path: "title",
		});
		for (const path of ["layout", "layout.card", "layout.missing.title", "missing.card.title"])
			expect(blockDefinitionPath(fields, path)).toBeUndefined();
	});
	test("rejects missing, duplicate and cyclic references", () => {
		for (const schema of [
			manifest([field("layout", ["missing"])]),
			manifest([field("layout", ["card", "card"])]),
			manifest([], [block(), block()]),
			manifest([], [{ ...block(), fields: [field("recursive")] }]),
		])
			expect(() => bindSchemaManifest(schema)).toThrow();
	});
	test("repeated placements retain one schema metadata tree", () => {
		const definition = {
			...block(),
			fields: Array.from({ length: 40 }, (_, i) => text(`field${i}`)),
		};
		const fields = Array.from({ length: 100 }, (_, i) => field(`layout${i}`));
		const schema = manifest(fields, [definition]);
		const compact = JSON.stringify(schema).length;
		// Repeating the definition at each placement would cost at least this much.
		const expanded = fields.length * JSON.stringify(definition).length;
		bindSchemaManifest(schema);
		const metadata = new Set(
			fields.flatMap((f) => resolveBlockTypes(f.blocks)[0]!.fields.map((child) => child.text))
		);
		expect(metadata.size).toBe(40);
		expect(compact).toBeLessThan(expanded / 10);
		console.info(
			`100 placements × 40 fields: compact ${compact} bytes; repeated at least ${expanded} bytes; ${metadata.size} shared text metadata nodes`
		);
	});
	test("binds once per definition however often the graph places it", () => {
		// Only 60 fields on the wire, but each level doubles the placed graph: 2^30 placements.
		const blocks = Array.from({ length: 31 }, (_, i) => ({
			...block(),
			slug: `level-${i}`,
			fields:
				i === 0
					? [text()]
					: [field("left", [`level-${i - 1}`]), field("right", [`level-${i - 1}`])],
		}));
		const schema = bindSchemaManifest(manifest([field("layout", ["level-30"])], blocks));
		let types = resolveBlockTypes(schema.collections[0]!.fields[0]!.blocks);
		for (let level = 30; level > 0; level--) types = resolveBlockTypes(types[0]!.fields[1]!.blocks);
		expect(types[0]!.fields[0]!.path).toBe(
			`layout.${Array.from({ length: 30 }, (_, i) => `level-${30 - i}.right`).join(".")}.level-0.title`
		);
		const deep = Array.from({ length: 49 }, (_, i) => ({
			...block(),
			slug: `level-${i}`,
			fields: i === 0 ? [text()] : [field("child", [`level-${i - 1}`])],
		}));
		expect(() => bindSchemaManifest(manifest([field("layout", ["level-48"])], deep))).toThrow(
			"levels deep"
		);
	});
});
