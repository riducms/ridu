import { describe, expect, test } from "bun:test";
import type { SchemaBlockType, SchemaField } from "@riducms/protocol";
import { createAdminI18n, en, fr } from "@riducms/translations";

import { ListFilterFields } from "@admin/features/collections/list-filter-fields";
import { listMetadataFields } from "@admin/features/collections/list-workspace";

import { bindBlockFields, blockDefinition } from "./block-manifest";

const i18n = createAdminI18n();

function field(name: string, type: SchemaField["type"], extra: Partial<SchemaField> = {}) {
	const label = name.charAt(0).toUpperCase() + name.slice(1);
	return { name, path: name, type, admin: { label }, ...extra } as SchemaField;
}
// Wire paths of root-level nested fields are absolute; block definitions rebase theirs.
const nested = (name: string, fields: SchemaField[]): SchemaField[] =>
	fields.map((child) => ({
		...child,
		path: `${name}.${child.path}`,
		...(child.nested && { nested: { fields: nested(name, child.nested.fields) } }),
	}));
const group = (name: string, fields: SchemaField[], extra: Partial<SchemaField> = {}) =>
	field(name, "group", { nested: { fields: nested(name, fields) }, ...extra });
const array = (name: string, fields: SchemaField[]) =>
	field(name, "array", { nested: { fields: nested(name, fields) } });
const blocks = (name: string, slugs: string[], extra: Partial<SchemaField> = {}) =>
	field(name, "blocks", { blocks: { blockReferences: slugs }, ...extra });

/** Pages whose layout places a small registry: hero (with nested note blocks) and quote. */
function pages() {
	const note = blockDefinition("note", [field("body", "textarea")], {
		singular: "Note",
		plural: "Notes",
	});
	const hero = blockDefinition(
		"hero",
		[
			field("heading", "text", { localized: true }),
			field("image", "upload", { upload: { collectionSlug: "media" } } as Partial<SchemaField>),
			field("secret", "text", { queryRestricted: true }),
			field("data", "json"),
			group("settings", [field("wide", "checkbox"), field("raw", "json")]),
			array("links", [field("label", "text")]),
			blocks("children", ["note"]),
			blocks("restricted", ["note"], { queryRestricted: true }),
		],
		{ singular: "Hero", plural: "Heroes", singularTranslations: { fr: "Bannière" } }
	);
	const quote = blockDefinition("quote", [field("author", "relationship")], {
		singular: "Quote",
		plural: "Quotes",
	});
	const empty = blockDefinition("embed", [field("payload", "json")], {
		singular: "Embed",
		plural: "Embeds",
	});
	return bindBlockFields([note, hero, quote, empty] satisfies SchemaBlockType[], [
		field("title", "text", {
			admin: { label: "Title", labelTranslations: { fr: "Titre" } },
		}),
		group("seo", [field("description", "textarea")], { admin: { label: "SEO" } }),
		blocks("layout", ["hero", "quote", "embed"]),
		field("content", "json", { plugin: { key: "richtext", config: {} } }),
		field("total", "number", { virtual: { valueType: "number" } }),
	]);
}

function catalog(fields: readonly SchemaField[] = pages(), language = i18n) {
	return new ListFilterFields({
		fields,
		metadata: listMetadataFields(undefined, language),
		i18n: language,
	});
}

const summary = (entries: readonly { kind: string; path: string; trail: readonly string[] }[]) =>
	entries.map((entry) => `${entry.kind} ${entry.path} ${entry.trail.join(" > ")}`);

describe("collection list filter fields", () => {
	test("browse one level at a time through registry block types", () => {
		const fields = catalog();
		expect(summary(fields.level("")!.entries)).toEqual([
			"field title Title",
			"group seo SEO",
			"blocks layout Layout",
			"field id ID",
			"field createdAt Created At",
			"field updatedAt Updated At",
		]);
		// A definition without filterable fields is not offered as a dead end.
		expect(summary(fields.level("layout")!.entries)).toEqual([
			"block layout.hero Layout > Hero",
			"block layout.quote Layout > Quote",
		]);
		expect(summary(fields.level("layout.hero")!.entries)).toEqual([
			"field layout.hero.heading Layout > Hero > Heading",
			"field layout.hero.image Layout > Hero > Image",
			"group layout.hero.settings Layout > Hero > Settings",
			"array layout.hero.links Layout > Hero > Links",
			"blocks layout.hero.children Layout > Hero > Children",
		]);
		expect(summary(fields.level("layout.hero.children.note")!.entries)).toEqual([
			"field layout.hero.children.note.body Layout > Hero > Children > Note > Body",
		]);
		expect(fields.level("layout.hero.restricted")).toBeUndefined();
		expect(fields.level("layout.embed")).toBeUndefined();
		expect(fields.level("layout.missing")).toBeUndefined();
		expect(fields.level("title")).toBeUndefined();
	});

	test("resolve exactly the leaf paths the list planner and REST where admit", () => {
		const fields = catalog();
		for (const path of [
			"title",
			"seo.description",
			"layout.hero.heading",
			"layout.hero.image",
			"layout.hero.settings.wide",
			"layout.hero.links.label",
			"layout.hero.children.note.body",
			"layout.quote.author",
			"id",
			"updatedAt",
		])
			expect(fields.resolve(path)?.field.path).toBe(path);
		for (const path of [
			"seo",
			"layout",
			"layout.hero",
			"layout.hero.children",
			"layout.hero.children.note",
			"layout.heading",
			"layout.missing.heading",
			"layout.hero.secret",
			"layout.hero.data",
			"layout.hero.settings.raw",
			"layout.hero.restricted.note.body",
			"layout.embed.payload",
			"content",
			"total",
			"",
			"title.",
			".title",
		])
			expect(fields.resolve(path)).toBeUndefined();
	});

	test("localized fields and containers keep locale-free paths", () => {
		const note = blockDefinition("note", [field("body", "textarea", { localized: true })]);
		const fields = catalog(
			bindBlockFields(
				[note],
				[
					blocks("shared", ["note"]),
					blocks("translated", ["note"], { localized: true }),
					group("seo", [field("description", "textarea")], { localized: true }),
				]
			)
		);
		// Each placement reports localization once, on its outermost localized container;
		// the content locale is a request option, never a path segment.
		expect(fields.resolve("shared.note.body")?.field.localized).toBe(true);
		expect(fields.resolve("translated.note.body")?.field.localized).toBe(false);
		expect(fields.resolve("seo.description")?.trail).toEqual(["Seo", "Description"]);
	});

	test("search finds definitions' fields at concrete paths, shallowest first", () => {
		const fields = catalog();
		expect(summary(fields.search("hero").entries)).toEqual(["block layout.hero Layout > Hero"]);
		expect(summary(fields.search("  BODY ").entries)).toEqual([
			"field layout.hero.children.note.body Layout > Hero > Children > Note > Body",
		]);
		expect(summary(fields.search("e").entries).slice(0, 4)).toEqual([
			"field title Title",
			"group seo SEO",
			"field createdAt Created At",
			"field updatedAt Updated At",
		]);
		// Search is scoped to the open level.
		expect(summary(fields.search("label", "layout.hero").entries)).toEqual([
			"field layout.hero.links.label Layout > Hero > Links > Label",
		]);
		expect(fields.search("label", "seo").entries).toEqual([]);
		expect(fields.search("secret").entries).toEqual([]);
		expect(fields.search("payload").entries).toEqual([]);
		expect(fields.search(" ").entries).toEqual([]);
	});

	test("labels follow the interface language", () => {
		const french = createAdminI18n({ languages: [en, fr], language: "fr" });
		const fields = catalog(pages(), french);
		expect(fields.resolve("title")?.trail).toEqual(["Titre"]);
		expect(fields.level("layout.hero")?.trail).toEqual(["Layout", "Bannière"]);
		expect(summary(fields.search("bannière").entries)).toEqual([
			"block layout.hero Layout > Bannière",
		]);
	});

	test("readable views hide denied paths and everything beneath denied containers", () => {
		const denied = new Set(["layout.hero.heading", "layout.hero.settings", "seo"]);
		const fields = catalog().readable((path) => !denied.has(path));
		expect(fields.level("")!.entries.map((entry) => entry.path)).not.toContain("seo");
		expect(fields.level("layout.hero")!.entries.map((entry) => entry.path)).toEqual([
			"layout.hero.image",
			"layout.hero.links",
			"layout.hero.children",
		]);
		expect(fields.resolve("layout.hero.heading")).toBeUndefined();
		expect(fields.resolve("layout.hero.settings.wide")).toBeUndefined();
		expect(fields.resolve("seo.description")).toBeUndefined();
		expect(fields.level("seo")).toBeUndefined();
		expect(fields.search("wide").entries).toEqual([]);
		expect(fields.resolve("layout.hero.image")).toBeDefined();
		expect(catalog().readable(() => false).empty).toBe(true);
	});

	test("the signature tracks filterable structure, not labels or placements", () => {
		const base = catalog().signature;
		expect(catalog().signature).toBe(base);
		expect(
			catalog(pages(), createAdminI18n({ languages: [en, fr], language: "fr" })).signature
		).toBe(base);
		expect(catalog([...pages(), field("summary", "textarea")]).signature).not.toBe(base);
	});
});

describe("filter fields over deep block reference graphs", () => {
	// 60 fields on the wire; each level doubles the placed graph to 2^30 leaf placements.
	function doubling() {
		const definitions = Array.from({ length: 31 }, (_, level) =>
			blockDefinition(
				`level-${level}`,
				level === 0
					? [field("title", "text")]
					: [
							field("title", "text"),
							blocks("left", [`level-${level - 1}`]),
							blocks("right", [`level-${level - 1}`]),
						]
			)
		);
		return catalog(bindBlockFields(definitions, [blocks("layout", ["level-30"])]));
	}

	test("navigation and resolution stay proportional to the requested path", () => {
		const fields = doubling();
		let scope = "layout";
		for (let level = 30; level > 0; level--) {
			const entries = fields.level(scope)!.entries;
			expect(entries.length).toBe(1);
			scope = `${entries[0]!.path}.right`;
		}
		const deepest = `${scope}.level-0.title`;
		expect(deepest.split(".").length).toBe(63);
		expect(fields.resolve(deepest)?.trail.length).toBe(63);
		expect(fields.resolve(deepest.replace("level-0.title", "level-0.missing"))).toBeUndefined();
	});

	test("search returns a bounded, shallowest-first page of concrete paths", () => {
		const fields = doubling();
		const started = performance.now();
		const result = fields.search("title");
		const elapsed = performance.now() - started;
		expect(result.truncated).toBe(true);
		expect(result.entries.length).toBe(50);
		expect(result.entries[0]!.path).toBe("layout.level-30.title");
		const depths = result.entries.map((entry) => entry.path.split(".").length);
		expect(depths).toEqual([...depths].sort((left, right) => left - right));
		// Enumerating 2^30 placements would not finish; the bounded search is immediate.
		expect(elapsed).toBeLessThan(1000);

		const deepest = fields.search("level-0");
		expect(deepest.truncated).toBe(true);
		expect(deepest.entries.length).toBe(50);
		expect(deepest.entries[0]!.path).toBe(
			`layout.${Array.from({ length: 30 }, (_, i) => `level-${30 - i}.left`).join(".")}.level-0`
		);
		expect(fields.search("absent").entries).toEqual([]);
	});
});
