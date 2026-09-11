import { resolveAdminConfig, validateAdminManifest } from "../src/admin";
import { describe, expect, it } from "bun:test";

import {
	ADMIN_PLUGIN_API_VERSION,
	defineAdminMessages,
	defineRowLabelPlugin,
	resolveAdminExtensions,
	assertAdminPluginPairs,
	type AdminPluginPair,
} from "../src";
import { defineAdminPlugin, definePluginField, defineFieldComponent } from "../src/authoring/v1";
import { resolvePluginFields } from "../src/plugin-registry";
import type { Component } from "svelte";

function checkedConfig(pairs: readonly AdminPluginPair[]) {
	assertAdminPluginPairs(pairs);
	return resolveAdminConfig({ plugins: pairs.map((pair) => pair.admin) });
}

const field = definePluginField({
	component: () => ({}),
	decodeValue: (value: unknown): string => {
		if (typeof value !== "string") throw new Error("Expected string");
		return value;
	},
});

const admin = defineAdminPlugin({
	key: "color",
	pairingVersion: 2,
	fields: { color: field },
});

const backend = {
	apiVersion: ADMIN_PLUGIN_API_VERSION,
	export: "colorAdminPlugin",
	key: "color",
	package: "@example/color-admin",
	fieldTypes: ["color"],
	pairingVersion: 2,
};

describe("admin plugin pairing", () => {
	it("returns registrations only after metadata agrees", () => {
		const resolved = checkedConfig([{ admin, backend }]);

		expect(resolved.plugins).toEqual([admin]);
		expect(resolved.fields.map((item) => item.registration)).toEqual([field]);
		expect(Object.isFrozen(resolved.plugins)).toBe(true);
		expect(Object.isFrozen(resolved.fields)).toBe(true);
		expect(resolved.extensions.rowLabels).toEqual([]);
		expect(Object.isFrozen(resolved.extensions.rowLabels)).toBe(true);
		expect(resolved.extensions.routes).toEqual([]);
		expect(Object.isFrozen(resolved.extensions.routes)).toBe(true);
		expect(resolved.extensions.dashboard).toEqual([]);
		expect(resolved.extensions.login).toEqual([]);
		expect(resolved.extensions.account).toEqual([]);
		expect(resolved.extensions.navigation).toEqual([]);
		expect(resolved.extensions.logoutButton).toBeUndefined();
		expect(resolved.extensions.views).toEqual([]);
		expect(resolved.extensions.branding).toEqual([]);
		expect(resolved.extensions.shell).toEqual([]);
		expect(resolved.extensions.providers).toEqual([]);
		expect(resolved.extensions.listCells).toEqual([]);
		expect(resolved.extensions.documentActions).toEqual([]);
		expect(resolved.extensions.documentViews).toEqual([]);
		expect(resolved.extensions.messages).toEqual({});
	});

	it("indexes exact row-label identities and rejects mismatches and collisions", () => {
		const component = (() => undefined) as unknown as Component;
		const rowLabel = defineRowLabelPlugin({
			key: "color",
			componentKey: "swatchSummary",
			component,
		});
		const resolved = checkedConfig([{ admin: { ...admin, rowLabels: [rowLabel] }, backend }]);
		expect(resolved.extensions.rowLabels).toEqual([rowLabel]);
		expect(Object.isFrozen(resolved.extensions.rowLabels)).toBe(true);

		expect(() =>
			checkedConfig([
				{
					admin: { ...admin, rowLabels: [{ ...rowLabel, key: "other" }] },
					backend,
				},
			])
		).toThrow("Admin plugin color registered row label component for plugin other");
		expect(() =>
			checkedConfig([
				{
					admin: { ...admin, rowLabels: [{ ...rowLabel, componentKey: "bad-key" }] },
					backend,
				},
			])
		).toThrow("color:bad-key has an invalid component key");
		expect(() =>
			checkedConfig([{ admin: { ...admin, rowLabels: [rowLabel, { ...rowLabel }] }, backend }])
		).toThrow("Admin row label component color:swatchSummary is already registered");
	});

	it("validates and namespaces exact plugin message catalogs", () => {
		const messages = defineAdminMessages({
			fallback: { bold: "Bold", selected: { one: "{count} selected", other: "{count} selected" } },
			translations: {
				fr: {
					bold: "Gras",
					selected: { one: "{count} sélectionné", other: "{count} sélectionnés" },
				},
			},
		});
		const translatedAdmin = { ...admin, messages };
		const resolved = checkedConfig([{ admin: translatedAdmin, backend }]);
		expect(resolved.extensions.messages.color).toBe(messages);
		expect(Object.isFrozen(messages)).toBe(true);
		expect(Object.isFrozen(messages.fallback)).toBe(true);
		expect(Object.isFrozen(messages.translations?.fr)).toBe(true);
		expect(() =>
			defineAdminMessages({
				fallback: { bold: "Bold {value}" },
				// @ts-expect-error Exercise the runtime guard against mismatched placeholders.
				translations: { fr: { bold: "Gras" } },
			})
		).toThrow("different placeholders");

		expect(() =>
			checkedConfig([
				{
					admin: {
						...admin,
						messages: {
							fallback: { bold: "Bold {value}" },
							translations: { fr: { bold: "Gras" } },
						},
					},
					backend,
				},
			])
		).toThrow("different placeholders");
	});

	it("rejects translated labels outside the owning plugin catalog", () => {
		const component = (() => undefined) as unknown as Component;
		const messages = defineAdminMessages({ fallback: { route: "Route" } });
		expect(() =>
			resolveAdminExtensions([
				{
					...admin,
					messages,
					routes: [
						{
							path: "route",
							component,
							navigation: { label: "Route", labelKey: "plugin.other:route" },
						},
					],
				},
			])
		).toThrow("must use its own plugin.color: namespace");

		expect(() =>
			resolveAdminExtensions([
				{
					...admin,
					messages,
					documentViews: [
						{
							key: "audit",
							label: "Audit",
							labelKey: "plugin.color:missing",
							component,
						},
					],
				},
			])
		).toThrow("is not defined in its messages");
	});

	it("rejects a stale admin half", () => {
		expect(() => checkedConfig([{ admin: { ...admin, pairingVersion: 1 }, backend }])).toThrow(
			"install matching plugin packages"
		);
	});

	it("rejects the wrong package export before field registration", () => {
		expect(() => checkedConfig([{ admin: { ...admin, key: "other" }, backend }])).toThrow(
			"compiled backend expects color"
		);
	});

	it("requires exact backend field-type declarations independently of plugin identity", () => {
		expect(() =>
			checkedConfig([{ admin: { ...admin, fields: { other: field } }, backend }])
		).toThrow("field types");
		const result = checkedConfig([
			{
				admin: { ...admin, fields: { other: field } },
				backend: { ...backend, fieldTypes: ["other"] },
			},
		]);
		expect(result.fields[0]?.owner).toBe("color");
		expect(result.fields[0]?.key).toBe("other");
	});
	it("derives named built-in renderer ownership from the enclosing plugin", () => {
		const title = defineFieldComponent({
			type: "text",
			component: () => ({}),
			decodeValue: (value: unknown): string => String(value),
		});
		const resolved = resolvePluginFields([{ ...admin, components: { title } }]);
		expect(resolved.find((item) => item.componentKey === "title")?.owner).toBe("color");
	});
	it("rejects competing field-type claims from independent plugins", () => {
		expect(() => resolvePluginFields([admin, { ...admin, key: "other" }])).toThrow(
			"Duplicate admin field renderer field:color"
		);
	});

	it("pairs declared routes and static assets exactly", () => {
		const component = (() => undefined) as unknown as Component;
		const routedAdmin = {
			...admin,
			routes: [{ path: "color/palette", component, navigation: { label: "Palette" } }],
			assets: ["color.css"],
		};
		const routedBackend = {
			...backend,
			routes: ["color/palette"],
			assets: ["color.css"],
		};

		expect(
			checkedConfig([{ admin: routedAdmin, backend: routedBackend }]).extensions.routes
		).toEqual(routedAdmin.routes);
		expect(() =>
			checkedConfig([{ admin: routedAdmin, backend: { ...routedBackend, routes: [] } }])
		).toThrow("routes do not match");
	});

	it("rejects route collisions across plugins", () => {
		const component = (() => undefined) as unknown as Component;
		const route = { path: "tools/import", component };
		const firstAdmin = { ...admin, routes: [route] };
		const firstBackend = { ...backend, routes: [route.path] };
		const secondAdmin = { ...firstAdmin, key: "other", pairingVersion: 1, fields: {} };
		const secondBackend = {
			...firstBackend,
			key: "other",
			pairingVersion: 1,
			package: "@example/other-admin",
			fieldTypes: [],
		};

		expect(() =>
			checkedConfig([
				{ admin: firstAdmin, backend: firstBackend },
				{ admin: secondAdmin, backend: secondBackend },
			])
		).toThrow("already registered");
	});

	it("finalizes broad admin extension points and rejects conflicting slots", () => {
		const component = (() => undefined) as unknown as Component;
		const extendedAdmin = {
			...admin,
			dashboard: [{ key: "summary", component }],
			login: [{ key: "login-message", component, position: "before" as const }],
			account: [{ key: "profile-message", surface: "profile" as const, component }],
			navigation: [{ key: "nav-message", position: "after" as const, component }],
			logoutButton: { key: "logout", component },
			views: [
				{ key: "posts-list", surface: "collectionList" as const, collection: "posts", component },
			],
			branding: [{ key: "logo", surface: "loginLogo" as const, component }],
			shell: [{ key: "support", position: "settingsMenu" as const, component }],
			providers: [{ key: "context", component }],
			listCells: [{ key: "score", collection: "posts", field: "score", label: "Score", component }],
			documentActions: [{ key: "review", collection: "posts", component }],
			documentViews: [{ key: "insights", label: "Insights", collection: "posts", component }],
		};
		const resolved = checkedConfig([{ admin: extendedAdmin, backend }]);

		expect(resolved.extensions.dashboard).toEqual(extendedAdmin.dashboard);
		expect(resolved.extensions.login).toEqual(extendedAdmin.login);
		expect(resolved.extensions.account).toEqual(extendedAdmin.account);
		expect(resolved.extensions.navigation).toEqual(extendedAdmin.navigation);
		expect(resolved.extensions.logoutButton).toEqual(extendedAdmin.logoutButton);
		expect(resolved.extensions.views).toEqual(extendedAdmin.views);
		expect(resolved.extensions.branding).toEqual(extendedAdmin.branding);
		expect(resolved.extensions.shell).toEqual(extendedAdmin.shell);
		expect(resolved.extensions.providers).toEqual(extendedAdmin.providers);
		expect(resolved.extensions.listCells).toEqual(extendedAdmin.listCells);
		expect(resolved.extensions.documentActions).toEqual(extendedAdmin.documentActions);
		expect(resolved.extensions.documentViews).toEqual(extendedAdmin.documentViews);
		expect(Object.isFrozen(resolved.extensions.documentViews)).toBe(true);

		expect(() =>
			checkedConfig([
				{ admin: extendedAdmin, backend },
				{
					admin: {
						...admin,
						key: "other",
						pairingVersion: 1,
						fields: {},
						dashboard: extendedAdmin.dashboard,
					},
					backend: {
						...backend,
						key: "other",
						pairingVersion: 1,
						package: "@example/other-admin",
						fieldTypes: [],
					},
				},
			])
		).toThrow("Admin dashboard panel summary is already registered");
	});

	it("allows only one dashboard replacement", () => {
		const component = (() => undefined) as unknown as Component;
		const replacement = { key: "custom", position: "replace" as const, component };
		const firstAdmin = { ...admin, dashboard: [replacement] };
		const secondAdmin = {
			...admin,
			key: "other",
			pairingVersion: 1,
			fields: {},
			dashboard: [{ ...replacement, key: "other" }],
		};

		expect(() =>
			checkedConfig([
				{ admin: firstAdmin, backend },
				{
					admin: secondAdmin,
					backend: {
						...backend,
						key: "other",
						pairingVersion: 1,
						package: "@example/other-admin",
						fieldTypes: [],
					},
				},
			])
		).toThrow("both registered");
	});

	it("allows only one replacement for each core surface", () => {
		const component = (() => undefined) as unknown as Component;
		const firstAdmin = {
			...admin,
			login: [{ key: "login", component, position: "replace" as const }],
			account: [
				{ key: "profile", surface: "profile" as const, component, position: "replace" as const },
			],
			navigation: [{ key: "navigation", component, position: "replace" as const }],
			logoutButton: { key: "logout", component },
		};
		const secondAdmin = {
			...firstAdmin,
			key: "other",
			pairingVersion: 1,
			fields: {},
			login: [{ key: "other-login", component, position: "replace" as const }],
			account: [
				{
					key: "other-profile",
					surface: "profile" as const,
					component,
					position: "replace" as const,
				},
			],
			navigation: [{ key: "other-navigation", component, position: "replace" as const }],
			logoutButton: { key: "other-logout", component },
		};
		const secondBackend = {
			...backend,
			key: "other",
			pairingVersion: 1,
			package: "@example/other-admin",
			fieldTypes: [],
		};

		expect(() =>
			checkedConfig([
				{ admin: firstAdmin, backend },
				{ admin: secondAdmin, backend: secondBackend },
			])
		).toThrow("login replacements");

		expect(() =>
			checkedConfig([
				{ admin: { ...firstAdmin, login: [] }, backend },
				{ admin: { ...secondAdmin, login: [] }, backend: secondBackend },
			])
		).toThrow("profile account replacements");

		expect(() =>
			checkedConfig([
				{ admin: { ...firstAdmin, login: [], account: [] }, backend },
				{ admin: { ...secondAdmin, login: [], account: [] }, backend: secondBackend },
			])
		).toThrow("navigation replacements");

		expect(() =>
			checkedConfig([
				{ admin: { ...firstAdmin, login: [], account: [], navigation: [] }, backend },
				{
					admin: { ...secondAdmin, login: [], account: [], navigation: [] },
					backend: secondBackend,
				},
			])
		).toThrow("logout buttons");
	});

	it("rejects duplicate resource view replacements but permits scoped overrides", () => {
		const component = (() => undefined) as unknown as Component;
		const firstAdmin = {
			...admin,
			views: [
				{ key: "all-lists", surface: "collectionList" as const, component },
				{ key: "posts-list", surface: "collectionList" as const, collection: "posts", component },
			],
		};
		const secondAdmin = {
			...admin,
			key: "other",
			pairingVersion: 1,
			fields: {},
			views: [
				{ key: "duplicate", surface: "collectionList" as const, collection: "posts", component },
			],
		};

		expect(checkedConfig([{ admin: firstAdmin, backend }]).extensions.views).toHaveLength(2);
		expect(() =>
			checkedConfig([
				{ admin: firstAdmin, backend },
				{
					admin: secondAdmin,
					backend: {
						...backend,
						key: "other",
						pairingVersion: 1,
						package: "@example/other-admin",
						fieldTypes: [],
					},
				},
			])
		).toThrow("core view collectionList.posts");
	});

	it("allows ordered shell slots but one owner for each brand surface", () => {
		const component = (() => undefined) as unknown as Component;
		const firstAdmin = {
			...admin,
			branding: [{ key: "logo", surface: "navigationLogo" as const, component }],
			shell: [
				{ key: "first", position: "actions" as const, component },
				{ key: "second", position: "actions" as const, component },
			],
		};
		const secondAdmin = {
			...admin,
			key: "other",
			pairingVersion: 1,
			fields: {},
			branding: [{ key: "other-logo", surface: "navigationLogo" as const, component }],
		};

		expect(checkedConfig([{ admin: firstAdmin, backend }]).extensions.shell).toEqual(
			firstAdmin.shell
		);
		expect(() =>
			checkedConfig([
				{ admin: firstAdmin, backend },
				{
					admin: secondAdmin,
					backend: {
						...backend,
						key: "other",
						pairingVersion: 1,
						package: "@example/other-admin",
						fieldTypes: [],
					},
				},
			])
		).toThrow("branding surface navigationLogo");
	});

	it("orders providers and rejects duplicate provider keys", () => {
		const component = (() => undefined) as unknown as Component;
		const firstAdmin = { ...admin, providers: [{ key: "theme", component }] };
		const secondAdmin = {
			...admin,
			key: "other",
			pairingVersion: 1,
			fields: {},
			providers: [{ key: "theme", component }],
		};

		expect(checkedConfig([{ admin: firstAdmin, backend }]).extensions.providers).toEqual(
			firstAdmin.providers
		);
		expect(() =>
			checkedConfig([
				{ admin: firstAdmin, backend },
				{
					admin: secondAdmin,
					backend: {
						...backend,
						key: "other",
						pairingVersion: 1,
						package: "@example/other-admin",
						fieldTypes: [],
					},
				},
			])
		).toThrow("provider theme");
	});
});

it("rejects unsupported versions for embedded schema host plugins", () => {
	expect(() => checkedConfig([{ backend: { ...backend, apiVersion: 99 }, admin }])).toThrow(
		"requires admin plugin API 99"
	);
});

it("freezes registration ownership independently of caller objects", () => {
	const entries = { color: field };
	const plugin = defineAdminPlugin({ key: "immutable", pairingVersion: 1, fields: entries });
	entries.color = definePluginField({ component: () => ({}), decodeValue: () => "changed" });
	expect(plugin.fields.color).toBe(field);
	expect(Object.isFrozen(plugin.fields)).toBe(true);
});
it("rejects malformed declarations and does not restamp incompatible APIs", () => {
	expect(admin.apiVersion).toBe(1);
	for (const fields of [[], new Map(), { bad: { type: "plugin", component: () => ({}) } }]) {
		expect(() => defineAdminPlugin({ key: "bad", pairingVersion: 1, fields } as never)).toThrow();
	}
	expect(() => defineAdminPlugin({ ...admin, apiVersion: 1 } as never)).toThrow("restamp");
	expect(() => resolvePluginFields([{ ...admin, apiVersion: 99 } as never])).toThrow("uses API 99");
	expect(() =>
		definePluginField({
			component: () => ({}),
			decodeValue: () => 1,
			canRender: () => true,
		} as never)
	).toThrow("matching");
});
it("checks decoder results, serialized configuration and builtin input values at runtime", () => {
	const schema = {
		type: "plugin",
		path: "body",
		admin: { label: "Body" },
		plugin: { key: "body", config: { required: true } },
	} as import("@riducms/protocol").SchemaField;
	expect(() => field.decodeConfig(schema)).toThrow("no decodeConfig");
	const asyncConfig = definePluginField({
		component: () => ({}),
		decodeValue: () => 1,
		decodeConfig: async () => ({}),
	});
	expect(() => asyncConfig.decodeConfig(schema)).toThrow("synchronous");
	const nonJSON = definePluginField({ component: () => ({}), decodeValue: () => new Date() });
	expect(() => nonJSON.decodeValue({})).toThrow("JSON");
	const badInput = defineFieldComponent({
		type: "number",
		component: () => ({}),
		decodeValue: () => 1,
		decodeInput: () => "bad",
	} as never);
	expect(() => badInput.decodeInput(1)).toThrow("Invalid decoded number");
});
it("named plugin renderer config is independent of backend value config", () => {
	const renderer = defineFieldComponent({
		type: "plugin",
		fieldType: "color",
		component: () => ({}),
		decodeValue: (raw: unknown) => raw,
	});
	const schema = {
		type: "plugin",
		path: "color",
		admin: { label: "Color", component: { plugin: "tools", component: "Compact" } },
		plugin: { key: "color", config: { backend: true } },
	} as import("@riducms/protocol").SchemaField;
	expect(renderer.decodeConfig(schema)).toBeUndefined();
	expect(() =>
		renderer.decodeConfig({ ...schema, plugin: { key: "outline", config: {} } })
	).toThrow("expects plugin field type color");
});

it("keeps default field identities separate from named component identities", () => {
	const plugin = defineAdminPlugin({
		key: "plugin",
		pairingVersion: 1,
		fields: { color: field },
		components: {
			color: defineFieldComponent({
				type: "text",
				component: () => ({}),
				decodeValue: (raw: unknown) => String(raw),
			}),
		},
	});
	expect(resolvePluginFields([plugin]).map(({ key, componentKey }) => [key, componentKey])).toEqual(
		[
			["color", undefined],
			["color", "color"],
		]
	);
});

it("checks completeness at build time while validating selected fields in partial startup manifests", () => {
	const plugin = defineAdminPlugin({
		key: "tools",
		pairingVersion: 1,
		components: {
			Compact: defineFieldComponent({
				type: "plugin",
				fieldType: "color",
				component: () => ({}),
				decodeValue: (raw: unknown) => raw,
			}),
		},
	});
	const partial = { collections: [], globals: [], plugins: [] };
	expect(() =>
		validateAdminManifest(resolveAdminConfig({ plugins: [plugin] }), partial, {
			completeManifest: false,
		})
	).not.toThrow();
	expect(() =>
		validateAdminManifest(resolveAdminConfig({ plugins: [plugin] }), partial, {
			completeManifest: true,
		})
	).toThrow("undeclared field type color");
	const schema = {
		type: "plugin",
		path: "body",
		admin: { label: "Body", component: { plugin: "tools", component: "Compact" } },
		plugin: { key: "outline", config: {} },
	} as import("@riducms/protocol").SchemaField;
	const collection = {
		slug: "pages",
		fields: [schema],
	} as import("@riducms/protocol").SchemaCollection;
	expect(() =>
		validateAdminManifest(
			resolveAdminConfig({ plugins: [plugin] }),
			{ ...partial, collections: [collection] },
			{ completeManifest: false }
		)
	).toThrow("expects plugin field type color");
});

it("paired components distinguish omitted settings from supplied component configuration", () => {
	const component = defineFieldComponent({
		type: "text",
		component: () => ({}),
		decodeValue: (value: unknown) => String(value),
	});
	const configured = defineFieldComponent({
		type: "text",
		component: () => ({}),
		decodeValue: (value: unknown) => String(value),
		decodeConfig: () => ({ checked: true }),
	});
	const schema = {
		type: "text",
		path: "title",
		admin: { label: "Title", component: { plugin: "tools", component: "Title" } },
	} as import("@riducms/protocol").SchemaField;
	expect(component.decodeConfig(schema)).toBeUndefined();
	expect(() => configured.decodeConfig(schema)).toThrow("config is required by decodeConfig");
	const supplied = {
		...schema,
		admin: { ...schema.admin, component: { ...schema.admin.component!, config: {} } },
	};
	expect(() => component.decodeConfig(supplied)).toThrow("no decodeConfig");
	expect(configured.decodeConfig(supplied)).toEqual({ checked: true });
	for (const config of [undefined, null, [], { nested: undefined }]) {
		const malformed = {
			...supplied,
			admin: { ...supplied.admin, component: { ...supplied.admin.component, config } },
		};
		expect(() => configured.decodeConfig(malformed)).toThrow("config");
	}
});

it("paired primitive list components validate real element types and detach array values", () => {
	const text = defineFieldComponent({
		type: "text-list",
		component: () => ({}),
		decodeValue: (value: unknown) => value as string[],
	});
	const numbers = defineFieldComponent({
		type: "number-list",
		component: () => ({}),
		decodeValue: (value: unknown) => value as number[],
	});
	for (const [component, value] of [
		[text, ["", "oak", "oak"]],
		[numbers, [0, 8, 8]],
	] as const) {
		const decoded = component.decodeValue(value);
		expect(decoded).toEqual(value);
		expect(decoded).not.toBe(value);
	}
	for (const value of [[null], [{}], [1], Array(2), "scalar"])
		expect(() => text.decodeValue(value)).toThrow("Invalid decoded text-list value");
	for (const value of [[null], ["1"], Array(2), 1])
		expect(() => numbers.decodeValue(value)).toThrow("Invalid decoded number-list value");
	for (const value of [[NaN], [Infinity]])
		expect(() => numbers.decodeValue(value)).toThrow("finite, acyclic JSON data");
});
