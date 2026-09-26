import { resolveAdminConfig, validateAdminManifest } from "../src/admin";
import { expect, test } from "bun:test";
import type { SchemaField } from "@riducms/protocol";
import { defineAdmin, defineRowLabel, type AdminConfig } from "../src/admin";
import { defineAdminPlugin, defineFieldComponent, definePluginField } from "../src/authoring/v1";
import { resolveAdminExtensions } from "../src/plugin";
import { createAdminI18n } from "@riducms/translations";
import { createAdminLoader } from "@riducms/sdk";
const Component = () => ({});

test("dashboard, route and core-view loaders must match the generated Go contract", () => {
	const loader = createAdminLoader<{}, string>({
		key: "report",
		input: { kind: "object" },
		output: { kind: "string" },
	});
	const configs: AdminConfig[] = [
		{ dashboardPanels: [{ key: "report", loader, component: Component }] },
		{ routes: [{ path: "report", loader, component: Component }] },
		{ coreViews: [{ key: "report", surface: "notFound", loader, component: Component }] },
	];
	for (const config of configs) {
		const manifest = {
			application: { name: "Views", adminLoaders: [loader.contract] },
			collections: [],
			globals: [],
			plugins: [],
		} as unknown as Parameters<typeof validateAdminManifest>[1];
		validateAdminManifest(resolveAdminConfig(config), manifest, { completeManifest: true });
		expect(() =>
			validateAdminManifest(
				resolveAdminConfig(config),
				{ ...manifest, application: { name: "Views", adminLoaders: [] } },
				{ completeManifest: true }
			)
		).toThrow("matching generated Go loader");
	}
});

test("list results reject competing renderers and replaced collection targets", () => {
	const results = { key: "cards", collection: "posts", component: Component };
	expect(() =>
		resolveAdminConfig({ listResultsRenderers: [results, { ...results, key: "other" }] })
	).toThrow("already registered");
	const manifest = {
		collections: [{ slug: "posts", fields: [] }],
		globals: [],
		plugins: [],
	} as unknown as Parameters<typeof validateAdminManifest>[1];
	validateAdminManifest(resolveAdminConfig({ listResultsRenderers: [results] }), manifest, {
		completeManifest: true,
	});
	expect(() =>
		validateAdminManifest(
			resolveAdminConfig({ listResultsRenderers: [{ ...results, collection: "missing" }] }),
			manifest,
			{ completeManifest: true }
		)
	).toThrow("unknown collection");
	expect(() =>
		validateAdminManifest(
			resolveAdminConfig({
				listResultsRenderers: [results],
				coreViews: [
					{ key: "replace", surface: "collectionList", collection: "posts", component: Component },
				],
			}),
			manifest,
			{ completeManifest: true }
		)
	).toThrow("replaced collection view");
});
const plugin = defineAdminPlugin({
	key: "paired",
	pairingVersion: 1,
	dashboardPanels: [{ key: "paired", component: Component }],
});

test("complete registry validation checks plugin pairing, field ownership and selected renderers", () => {
	const color = definePluginField({
		component: Component,
		decodeValue: (value: unknown) => value,
		decodeConfig(value: unknown) {
			if (
				typeof value !== "object" ||
				value === null ||
				!("limit" in value) ||
				typeof value.limit !== "number"
			)
				throw new Error("limit must be numeric");
			return value;
		},
	});
	const field: SchemaField = {
		id: "accent",
		name: "accent",
		path: "accent",
		type: "plugin",
		category: "plugin",
		required: false,
		unique: false,
		admin: { label: "Accent" },
		plugin: { key: "color", config: { limit: 5 } },
	};
	const manifest = {
		plugins: [
			{
				key: "shapes",
				admin: { apiVersion: 1, pairingVersion: 3, package: "@example/shapes", export: "shapes" },
				fieldTypes: [{ key: "color" }, { key: "outline" }],
			},
		],
		collections: [{ slug: "posts", fields: [field] }],
		globals: [],
	} as unknown as Parameters<typeof validateAdminManifest>[1];
	const registration = defineAdminPlugin({
		key: "shapes",
		pairingVersion: 3,
		fields: { color, outline: color },
	});
	const check = (config: AdminConfig, selected: SchemaField = field) =>
		validateAdminManifest(
			resolveAdminConfig(config),
			{ ...manifest, collections: [{ ...manifest.collections[0]!, fields: [selected] }] },
			{ completeManifest: true }
		);
	check({ plugins: [registration] });
	expect(() =>
		check({
			plugins: [
				defineAdminPlugin({ key: "shapes", pairingVersion: 3, fields: { color, missing: color } }),
			],
		})
	).toThrow("field types do not match");
	expect(() =>
		check({
			plugins: [
				defineAdminPlugin({ key: "shapes", pairingVersion: 4, fields: { color, outline: color } }),
			],
		})
	).toThrow("pairing version 4 does not match backend version 3");
	expect(() =>
		check(
			{ plugins: [registration] },
			{ ...field, plugin: { key: "color", config: { limit: "five" } } }
		)
	).toThrow("limit must be numeric");
	const text = defineFieldComponent({
		type: "text",
		component: Component,
		decodeValue: (value: unknown) => String(value),
	});
	const { plugin: _plugin, ...builtinField } = field;
	expect(() =>
		check(
			{
				plugins: [
					defineAdminPlugin({
						key: "shapes",
						pairingVersion: 3,
						fields: { color, outline: color },
						fieldEditors: { text },
					}),
				],
			},
			{
				...builtinField,
				type: "number",
				category: "scalar",
				admin: { ...field.admin, component: { plugin: "shapes", component: "text" } },
			}
		)
	).toThrow("expects text");
	const selected = {
		...field,
		admin: { ...field.admin, component: { plugin: "shapes", component: "Alternate" } },
		plugin: { key: "color", config: {} },
	} satisfies SchemaField;
	const alternate = (fieldType: "color" | "outline") =>
		defineAdminPlugin({
			key: "shapes",
			pairingVersion: 3,
			fields: { color, outline: color },
			fieldEditors: {
				Alternate: defineFieldComponent({
					type: "plugin",
					fieldType,
					component: Component,
					decodeValue: (value: unknown) => value,
				}),
			},
		});
	check({ plugins: [alternate("color")] }, selected);
	expect(() => check({ plugins: [alternate("outline")] }, selected)).toThrow(
		"expects plugin field type outline"
	);
});

test("local contributions compose after real plugins and share exclusive slots", () => {
	const config = defineAdmin({
		plugins: [plugin],
		dashboardPanels: [{ key: "local", component: Component }],
	});
	expect(
		resolveAdminExtensions(config.plugins!, config).dashboardPanels.map((item) => item.key)
	).toEqual(["paired", "local"]);
	expect(() =>
		resolveAdminConfig({
			plugins: [plugin],
			dashboardPanels: [{ key: "paired", component: Component }],
		})
	).toThrow("already registered");
	const replacing = {
		...plugin,
		dashboardPanels: [{ key: "paired", component: Component, position: "replace" as const }],
	};
	expect(() =>
		resolveAdminConfig({
			plugins: [replacing],
			dashboardPanels: [{ key: "local", component: Component, position: "replace" }],
		})
	).toThrow("both registered");
});

test("application and plugin routes cannot shadow framework routes or normalize to another registration", () => {
	for (const path of [
		"",
		"/reports",
		"reports/",
		"reports//today",
		"reports/../today",
		"reports?x",
		"reports/:id",
		"account/security",
		"Collections/posts",
		"ACCOUNT/security",
		"globals/site",
		"collections/posts",
		"login",
		"verify-email",
	]) {
		expect(() => resolveAdminConfig({ routes: [{ path, component: Component }] })).toThrow(
			"relative path"
		);
		expect(() =>
			resolveAdminConfig({ plugins: [{ ...plugin, routes: [{ path, component: Component }] }] })
		).toThrow("relative path");
	}
	expect(() =>
		resolveAdminConfig({ routes: [{ path: "reports/today", component: Component }] })
	).not.toThrow();
	expect(() =>
		resolveAdminConfig({
			plugins: [{ ...plugin, routes: [{ path: "reports", component: Component }] }],
			routes: [{ path: "reports", component: Component }],
		})
	).toThrow("already registered");
});

test("application translations have an independent namespace and preserve plugin messages", () => {
	const config = defineAdmin({
		messages: { fallback: { reports: "Reports" }, translations: { fr: { reports: "Rapports" } } },
		routes: [
			{
				path: "reports",
				component: Component,
				navigation: { label: "Reports", labelKey: "app:reports" },
			},
		],
	});
	const resolved = resolveAdminExtensions(
		[{ ...plugin, messages: { fallback: { reports: "Plugin reports" } } }],
		config
	);
	const i18n = createAdminI18n({
		applicationMessages: resolved.applicationMessages!,
		pluginMessages: resolved.messages,
	});
	expect(i18n.t("app:reports")).toBe("Reports");
	expect(i18n.t("plugin.paired:reports")).toBe("Plugin reports");
	expect(() =>
		resolveAdminConfig({
			routes: [
				{
					path: "reports",
					component: Component,
					navigation: { label: "Reports", labelKey: "app:missing" },
				},
			],
		})
	).toThrow("not defined");
	expect(() =>
		resolveAdminConfig({
			routes: [
				{
					path: "reports",
					component: Component,
					navigation: { label: "Reports", labelKey: "plugin.paired:reports" },
				},
			],
		})
	).toThrow("own app:");
});

const row: SchemaField = {
	id: "rows",
	name: "rows",
	path: "rows",
	type: "array",
	category: "nested",
	required: false,
	unique: false,
	admin: { label: "Rows" },
	nested: {
		fields: [],
		rowLabelComponent: { reference: "app:summary", config: { title: "Summary" } },
	},
};
const manifest = {
	collections: [{ slug: "posts", fields: [row] }],
	globals: [],
} as unknown as Parameters<typeof validateAdminManifest>[1];
test("local row labels validate detached config and missing selections without mounting components", () => {
	let decoded = 0;
	const label = defineRowLabel({
		component: () => {
			throw new Error("must not mount");
		},
		decodeConfig(value: unknown) {
			decoded++;
			if (
				typeof value !== "object" ||
				value === null ||
				!("title" in value) ||
				typeof value.title !== "string"
			)
				throw new Error("title required");
			value.title = "changed";
			return { title: value.title };
		},
	});
	validateAdminManifest(resolveAdminConfig({ rowLabels: { "app:summary": label } }), manifest);
	expect(decoded).toBe(1);
	expect(row.nested?.rowLabelComponent?.config).toEqual({ title: "Summary" });
	expect(() => validateAdminManifest(resolveAdminConfig({}), manifest)).toThrow("not registered");
	expect(() =>
		validateAdminManifest(
			resolveAdminConfig({
				rowLabels: { "app:summary": defineRowLabel({ component: Component }) },
			}),
			manifest
		)
	).toThrow("no decodeConfig");
	expect(() => resolveAdminConfig({ rowLabels: { "app:summary": { ...label } } })).toThrow(
		"defineRowLabel"
	);
});

test("full registry checks reject absent targets while permission-filtered runtime manifests are allowed", () => {
	const config = defineAdmin({
		listCellRenderers: [
			{
				key: "summary",
				collection: "posts",
				field: "rows",
				label: "Summary",
				component: Component,
			},
		],
	});
	validateAdminManifest(resolveAdminConfig(config), { collections: [], globals: [] });
	expect(() =>
		validateAdminManifest(
			resolveAdminConfig(config),
			{ collections: [], globals: [] },
			{ completeManifest: true }
		)
	).toThrow("unknown field");
	validateAdminManifest(
		resolveAdminConfig({
			...config,
			rowLabels: {
				"app:summary": defineRowLabel({
					component: Component,
					decodeConfig: (value: unknown) => value,
				}),
			},
		}),
		manifest,
		{ completeManifest: true }
	);
});

test("malformed unchecked entries fail before mounting", () => {
	for (const config of [
		{ dashboardPanels: {} },
		{ dashboardPanels: [{ key: "x", component: null }] },
		{ shellSlots: [{ key: "x", component: Component, position: "bad" }] },
		{ coreViews: [{ key: "x", component: Component, surface: "missing" }] },
		{ documentActions: [{ key: "x", component: Component, requires: "superuser" }] },
	])
		expect(() => resolveAdminConfig(config as unknown as AdminConfig)).toThrow();
});

test("route collision identity follows the router's case-insensitive matching", () => {
	expect(() =>
		resolveAdminConfig({
			routes: [
				{ path: "reports", component: Component },
				{ path: "Reports", component: Component },
			],
		})
	).toThrow("already registered");
	expect(() =>
		resolveAdminConfig({
			plugins: [{ ...plugin, routes: [{ path: "Reports", component: Component }] }],
			routes: [{ path: "reports", component: Component }],
		})
	).toThrow("already registered");
});

test("embedded local row labels use the same registration and config checks as ordinary rows", () => {
	const embeddedRow = { ...row, path: "body.blocks.card.rows" };
	const embeddedManifest = {
		collections: [
			{
				slug: "articles",
				fields: [
					{
						id: "body",
						name: "body",
						path: "body",
						type: "plugin",
						category: "plugin",
						required: false,
						unique: false,
						admin: { label: "Body" },
						plugin: {
							key: "body",
							config: {},
							embeddedTrees: [{ cases: [{ types: [{ fields: [embeddedRow] }] }] }],
						},
					},
				],
			},
		],
		globals: [],
	} as unknown as Parameters<typeof validateAdminManifest>[1];
	const label = defineRowLabel({
		component: Component,
		decodeConfig(value: unknown) {
			if (typeof value !== "object" || value === null || !("title" in value))
				throw new Error("title required");
			return value;
		},
	});
	const plugins = [
		defineAdminPlugin({
			key: "body",
			pairingVersion: 1,
			fields: {
				body: definePluginField({ component: Component, decodeValue: (value: unknown) => value }),
			},
		}),
	];
	validateAdminManifest(
		resolveAdminConfig({ plugins, rowLabels: { "app:summary": label } }),
		embeddedManifest
	);
	expect(() => validateAdminManifest(resolveAdminConfig({ plugins }), embeddedManifest)).toThrow(
		"articles.body.blocks.card.rows"
	);
	expect(() =>
		validateAdminManifest(
			resolveAdminConfig({
				plugins,
				rowLabels: { "app:summary": defineRowLabel({ component: Component }) },
			}),
			embeddedManifest
		)
	).toThrow("articles.body.blocks.card.rows");
});

test("local row label settings distinguish omission, explicit empty config and malformed supplied values", () => {
	const simple = defineRowLabel({ component: Component });
	const configured = defineRowLabel({
		component: Component,
		decodeConfig: () => ({ title: "Checked" }),
	});
	const absent = {
		...row,
		nested: { fields: [], rowLabelComponent: { reference: "app:summary" } },
	};
	expect(simple.decode(absent)).toBeUndefined();
	expect(() => configured.decode(absent)).toThrow("config is required by decodeConfig");
	const supplied = (config: unknown) => ({
		...row,
		nested: { fields: [], rowLabelComponent: { reference: "app:summary", config } },
	});
	expect(() => simple.decode(supplied({}))).toThrow("no decodeConfig");
	expect(configured.decode(supplied({}))).toEqual({ title: "Checked" });
	for (const value of [null, undefined, [], { nested: undefined }])
		expect(() => configured.decode(supplied(value))).toThrow("config");
});

test("authoring is typed only and each resolution owns detached static registries", () => {
	const plugins = [plugin];
	const config = { plugins, dashboardPanels: [{ key: "local", component: Component }] };
	expect(defineAdmin(config)).toBe(config);
	const first = resolveAdminConfig(config);
	plugins.length = 0;
	config.dashboardPanels.length = 0;
	expect(first.plugins).toEqual([plugin]);
	expect(first.extensions.dashboardPanels.map((item) => item.key)).toEqual(["paired", "local"]);
	expect(Object.isFrozen(first)).toBe(true);
	expect(Object.isFrozen(first.fieldEditors)).toBe(true);
	expect(resolveAdminConfig(config).plugins).toEqual([]);
	const conflict = {
		plugins: [plugin],
		dashboardPanels: [{ key: "paired", component: Component }],
	};
	expect(() => defineAdmin(conflict)).not.toThrow();
	expect(() => resolveAdminConfig(conflict)).toThrow("already registered");
});
