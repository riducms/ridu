import { resolveAdminConfig, validateAdminManifest } from "../src/admin";
import { expect, test } from "bun:test";
import type { SchemaField } from "@riducms/protocol";
import { defineFieldEditor } from "../src/editor";
import { defineAdmin } from "../src/admin";

const field: SchemaField = {
	id: "accent",
	name: "accent",
	path: "accent",
	type: "text",
	category: "scalar",
	required: false,
	unique: false,
	text: {},
	admin: { label: "Accent", editor: { reference: "app:color" } },
};

test("decoders validate detached serialized data, including reactive proxies", () => {
	const raw = { palette: ["red"] };
	const editor = defineFieldEditor({
		type: "text",
		component: () => ({}),
		decodeConfig(value: unknown) {
			if (
				typeof value !== "object" ||
				value === null ||
				!("palette" in value) ||
				!Array.isArray(value.palette)
			)
				throw new Error("palette must be an array");
			value.palette.push("blue");
			return { palette: value.palette };
		},
	});
	const schema = {
		...field,
		admin: { ...field.admin, editor: { reference: "app:color", config: new Proxy(raw, {}) } },
	};
	expect(editor.decode(schema)).toEqual({ palette: ["red", "blue"] });
	expect(raw).toEqual({ palette: ["red"] });
	expect(() => editor.decode(field)).toThrow("config is required by decodeConfig");
});

test("unchecked configuration and async decoders fail before mounting", () => {
	const editor = defineFieldEditor({ type: "text", component: () => ({}) });
	expect(editor.decode(field)).toBeUndefined();
	expect(() =>
		editor.decode({
			...field,
			admin: { ...field.admin, editor: { reference: "app:color", config: { palette: [] } } },
		})
	).toThrow("no decodeConfig");
	const asyncEditor = defineFieldEditor({
		type: "text",
		component: () => ({}),
		decodeConfig: async () => ({}),
	});
	expect(() =>
		asyncEditor.decode({
			...field,
			admin: { ...field.admin, editor: { reference: "app:color", config: {} } },
		})
	).toThrow("must be synchronous");
});

test("spreading a registration cannot separate its decoder from its field type", () => {
	const text = defineFieldEditor({ type: "text", component: () => ({}) });
	const number = defineFieldEditor({ type: "number", component: () => ({}) });
	expect(() =>
		resolveAdminConfig({
			fields: { "app:color": { ...text, type: "number", component: number.component } },
		})
	).toThrow("defineFieldEditor");
	expect(() => resolveAdminConfig({ fields: { "app:color": text } })).not.toThrow();
});

test("registration rejects malformed application editor references", () => {
	const editor = defineFieldEditor({ type: "text", component: () => ({}) });
	for (const reference of ["bad/name", "app:", "other:color"]) {
		expect(() => resolveAdminConfig({ fields: { [reference]: editor } })).toThrow(
			"Invalid editor reference"
		);
	}
});

test("validation traverses nested, block, global and embedded schema and reports every owner", () => {
	const child = (path: string): SchemaField => ({ ...field, path });
	const manifest = {
		collections: [
			{
				slug: "posts",
				fields: [
					{ ...field, admin: { label: "Group" }, nested: { fields: [child("group.accent")] } },
					{
						...field,
						admin: { label: "Blocks" },
						blocks: { types: [{ fields: [child("layout.hero.accent")] }] },
					},
					{
						...field,
						admin: { label: "Embedded" },
						plugin: {
							embeddedTrees: [{ cases: [{ types: [{ fields: [child("body.hero.accent")] }] }] }],
						},
					},
				],
			},
		],
		globals: [{ slug: "settings", fields: [field] }],
	} as unknown as Parameters<typeof validateAdminManifest>[1];
	let diagnostic = "";
	try {
		validateAdminManifest(resolveAdminConfig({}), manifest);
	} catch (error) {
		diagnostic = String(error);
	}
	for (const owner of [
		"posts.group.accent",
		"posts.layout.hero.accent",
		"posts.body.hero.accent",
		"settings.accent",
	])
		expect(diagnostic).toContain(owner);
	expect(diagnostic).toContain("not registered");
	expect(() =>
		validateAdminManifest(
			resolveAdminConfig({
				fields: { "app:color": defineFieldEditor({ type: "text", component: () => ({}) }) },
			}),
			manifest
		)
	).not.toThrow();
	expect(diagnostic).toContain("admin/src/admin.config.ts");
});

test("all configured editor failures identify the resource and unified graph field", () => {
	const candidates: SchemaField[] = [
		{ ...field, path: "meta.accent", type: "number" },
		{
			...field,
			path: "sections.accent",
			admin: { ...field.admin, editor: { reference: "app:missing" } },
		},
		{
			...field,
			path: "content.card.accent",
			admin: { ...field.admin, editor: { reference: "app:color", config: { invalid: true } } },
		},
	];
	const manifest = {
		collections: [{ slug: "articles", fields: candidates }],
		globals: [],
	} as unknown as Parameters<typeof validateAdminManifest>[1];
	let failure = "";
	try {
		validateAdminManifest(
			resolveAdminConfig({
				fields: { "app:color": defineFieldEditor({ type: "text", component: () => ({}) }) },
			}),
			manifest
		);
	} catch (error) {
		failure = String(error);
	}
	for (const path of [
		"articles.meta.accent",
		"articles.sections.accent",
		"articles.content.card.accent",
	])
		expect(failure).toContain(path);
	expect(failure).toContain("expects text");
	expect(failure).toContain("not registered");
	expect(failure).toContain("no decodeConfig");
});

test("omitted configuration differs from every supplied object, including empty settings", () => {
	const unconfigured = defineFieldEditor({ type: "text", component: () => ({}) });
	const configured = defineFieldEditor({
		type: "text",
		component: () => ({}),
		decodeConfig: () => ({ ready: true }),
	});
	expect(unconfigured.decode(field)).toBeUndefined();
	expect(() => configured.decode(field)).toThrow("config is required by decodeConfig");
	const selected = (config: unknown): SchemaField => ({
		...field,
		admin: { ...field.admin, editor: { reference: "app:color", config } },
	});
	for (const value of [{}, { ready: true }]) {
		expect(() => unconfigured.decode(selected(value))).toThrow("no decodeConfig");
		expect(configured.decode(selected(value))).toEqual({ ready: true });
	}
	const cycle: Record<string, unknown> = {};
	cycle.self = cycle;
	for (const value of [
		undefined,
		null,
		[],
		"invalid",
		{ value: undefined },
		{ value: NaN },
		{ value: Infinity },
		{ value: () => 1 },
		cycle,
	]) {
		expect(() => configured.decode(selected(value))).toThrow("config");
	}
});
