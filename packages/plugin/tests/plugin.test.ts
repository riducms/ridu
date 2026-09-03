import { describe, expect, it } from "bun:test";

import {
	ADMIN_PLUGIN_API_VERSION,
	defineAdminPlugin,
	defineAdminMessages,
	defineFieldPlugin,
	defineRowLabelPlugin,
	resolveAdminPluginExtensions,
	resolveAdminPluginPairs,
} from "../src";
import type { Component } from "svelte";

const field = defineFieldPlugin({
	type: "plugin",
	key: "color",
	canRender: (candidate) => candidate.plugin?.key === "color",
});

const admin = defineAdminPlugin({
	apiVersion: ADMIN_PLUGIN_API_VERSION,
	key: "color",
	pairingVersion: 2,
	fields: [field],
});

const backend = {
	apiVersion: ADMIN_PLUGIN_API_VERSION,
	export: "colorAdminPlugin",
	key: "color",
	package: "@example/color-admin",
	pairingVersion: 2,
};

describe("admin plugin pairing", () => {
	it("returns registrations only after metadata agrees", () => {
		const resolved = resolveAdminPluginPairs([{ admin, backend }]);

		expect(resolved.plugins).toEqual([admin]);
		expect(resolved.fields).toEqual([field]);
		expect(Object.isFrozen(resolved.plugins)).toBe(true);
		expect(Object.isFrozen(resolved.fields)).toBe(true);
		expect(resolved.rowLabels).toEqual([]);
		expect(Object.isFrozen(resolved.rowLabels)).toBe(true);
		expect(resolved.routes).toEqual([]);
		expect(Object.isFrozen(resolved.routes)).toBe(true);
		expect(resolved.dashboard).toEqual([]);
		expect(resolved.login).toEqual([]);
		expect(resolved.account).toEqual([]);
		expect(resolved.navigation).toEqual([]);
		expect(resolved.logoutButton).toBeUndefined();
		expect(resolved.views).toEqual([]);
		expect(resolved.branding).toEqual([]);
		expect(resolved.shell).toEqual([]);
		expect(resolved.providers).toEqual([]);
		expect(resolved.listCells).toEqual([]);
		expect(resolved.documentActions).toEqual([]);
		expect(resolved.documentViews).toEqual([]);
		expect(resolved.messages).toEqual({});
	});

	it("indexes exact row-label identities and rejects mismatches and collisions", () => {
		const component = (() => undefined) as unknown as Component;
		const rowLabel = defineRowLabelPlugin({
			key: "color",
			componentKey: "swatchSummary",
			component,
		});
		const resolved = resolveAdminPluginPairs([
			{ admin: { ...admin, rowLabels: [rowLabel] }, backend },
		]);
		expect(resolved.rowLabels).toEqual([rowLabel]);
		expect(Object.isFrozen(resolved.rowLabels)).toBe(true);

		expect(() =>
			resolveAdminPluginPairs([
				{
					admin: { ...admin, rowLabels: [{ ...rowLabel, key: "other" }] },
					backend,
				},
			])
		).toThrow("Admin plugin color registered row label component for plugin other");
		expect(() =>
			resolveAdminPluginPairs([
				{
					admin: { ...admin, rowLabels: [{ ...rowLabel, componentKey: "bad-key" }] },
					backend,
				},
			])
		).toThrow("color:bad-key has an invalid component key");
		expect(() =>
			resolveAdminPluginPairs([
				{ admin: { ...admin, rowLabels: [rowLabel, { ...rowLabel }] }, backend },
			])
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
		const resolved = resolveAdminPluginPairs([{ admin: translatedAdmin, backend }]);
		expect(resolved.messages.color).toBe(messages);
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
			resolveAdminPluginPairs([
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
			resolveAdminPluginExtensions([
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
			resolveAdminPluginExtensions([
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
		expect(() =>
			resolveAdminPluginPairs([{ admin: { ...admin, pairingVersion: 1 }, backend }])
		).toThrow("install matching plugin packages");
	});

	it("rejects the wrong package export before field registration", () => {
		expect(() => resolveAdminPluginPairs([{ admin: { ...admin, key: "other" }, backend }])).toThrow(
			"compiled backend expects color"
		);
	});

	it("rejects plugin fields owned by another backend key", () => {
		const wrongField = { ...field, key: "other" };
		expect(() =>
			resolveAdminPluginPairs([{ admin: { ...admin, fields: [wrongField] }, backend }])
		).toThrow("registered plugin field other");
	});

	it("requires named built-in renderers to belong to their paired backend", () => {
		const renderer = defineFieldPlugin({
			type: "text",
			key: "other",
			componentKey: "title",
			canRender: () => true,
		});
		expect(() =>
			resolveAdminPluginPairs([{ admin: { ...admin, fields: [renderer] }, backend }])
		).toThrow("registered plugin field other");
	});

	it("rejects duplicate exact built-in renderer identities before mounting", () => {
		const fields = [
			{ type: "text" as const, key: "color", componentKey: "title", canRender: () => true },
			{ type: "text" as const, key: "color", componentKey: "title", canRender: () => true },
		];
		expect(() => resolveAdminPluginPairs([{ backend, admin: { ...admin, fields } }])).toThrow(
			"Admin field renderer text:color:title is registered more than once"
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
			resolveAdminPluginPairs([{ admin: routedAdmin, backend: routedBackend }]).routes
		).toEqual(routedAdmin.routes);
		expect(() =>
			resolveAdminPluginPairs([{ admin: routedAdmin, backend: { ...routedBackend, routes: [] } }])
		).toThrow("routes do not match");
	});

	it("rejects route collisions across plugins", () => {
		const component = (() => undefined) as unknown as Component;
		const route = { path: "tools/import", component };
		const firstAdmin = { ...admin, routes: [route] };
		const firstBackend = { ...backend, routes: [route.path] };
		const secondAdmin = { ...firstAdmin, key: "other", pairingVersion: 1, fields: [] };
		const secondBackend = {
			...firstBackend,
			key: "other",
			pairingVersion: 1,
			package: "@example/other-admin",
		};

		expect(() =>
			resolveAdminPluginPairs([
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
		const resolved = resolveAdminPluginPairs([{ admin: extendedAdmin, backend }]);

		expect(resolved.dashboard).toEqual(extendedAdmin.dashboard);
		expect(resolved.login).toEqual(extendedAdmin.login);
		expect(resolved.account).toEqual(extendedAdmin.account);
		expect(resolved.navigation).toEqual(extendedAdmin.navigation);
		expect(resolved.logoutButton).toEqual(extendedAdmin.logoutButton);
		expect(resolved.views).toEqual(extendedAdmin.views);
		expect(resolved.branding).toEqual(extendedAdmin.branding);
		expect(resolved.shell).toEqual(extendedAdmin.shell);
		expect(resolved.providers).toEqual(extendedAdmin.providers);
		expect(resolved.listCells).toEqual(extendedAdmin.listCells);
		expect(resolved.documentActions).toEqual(extendedAdmin.documentActions);
		expect(resolved.documentViews).toEqual(extendedAdmin.documentViews);
		expect(Object.isFrozen(resolved.documentViews)).toBe(true);

		expect(() =>
			resolveAdminPluginPairs([
				{ admin: extendedAdmin, backend },
				{
					admin: {
						...admin,
						key: "other",
						pairingVersion: 1,
						fields: [],
						dashboard: extendedAdmin.dashboard,
					},
					backend: {
						...backend,
						key: "other",
						pairingVersion: 1,
						package: "@example/other-admin",
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
			fields: [],
			dashboard: [{ ...replacement, key: "other" }],
		};

		expect(() =>
			resolveAdminPluginPairs([
				{ admin: firstAdmin, backend },
				{
					admin: secondAdmin,
					backend: {
						...backend,
						key: "other",
						pairingVersion: 1,
						package: "@example/other-admin",
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
			fields: [],
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
		};

		expect(() =>
			resolveAdminPluginPairs([
				{ admin: firstAdmin, backend },
				{ admin: secondAdmin, backend: secondBackend },
			])
		).toThrow("login replacements");

		expect(() =>
			resolveAdminPluginPairs([
				{ admin: { ...firstAdmin, login: [] }, backend },
				{ admin: { ...secondAdmin, login: [] }, backend: secondBackend },
			])
		).toThrow("profile account replacements");

		expect(() =>
			resolveAdminPluginPairs([
				{ admin: { ...firstAdmin, login: [], account: [] }, backend },
				{ admin: { ...secondAdmin, login: [], account: [] }, backend: secondBackend },
			])
		).toThrow("navigation replacements");

		expect(() =>
			resolveAdminPluginPairs([
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
			fields: [],
			views: [
				{ key: "duplicate", surface: "collectionList" as const, collection: "posts", component },
			],
		};

		expect(resolveAdminPluginPairs([{ admin: firstAdmin, backend }]).views).toHaveLength(2);
		expect(() =>
			resolveAdminPluginPairs([
				{ admin: firstAdmin, backend },
				{
					admin: secondAdmin,
					backend: {
						...backend,
						key: "other",
						pairingVersion: 1,
						package: "@example/other-admin",
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
			fields: [],
			branding: [{ key: "other-logo", surface: "navigationLogo" as const, component }],
		};

		expect(resolveAdminPluginPairs([{ admin: firstAdmin, backend }]).shell).toEqual(
			firstAdmin.shell
		);
		expect(() =>
			resolveAdminPluginPairs([
				{ admin: firstAdmin, backend },
				{
					admin: secondAdmin,
					backend: {
						...backend,
						key: "other",
						pairingVersion: 1,
						package: "@example/other-admin",
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
			fields: [],
			providers: [{ key: "theme", component }],
		};

		expect(resolveAdminPluginPairs([{ admin: firstAdmin, backend }]).providers).toEqual(
			firstAdmin.providers
		);
		expect(() =>
			resolveAdminPluginPairs([
				{ admin: firstAdmin, backend },
				{
					admin: secondAdmin,
					backend: {
						...backend,
						key: "other",
						pairingVersion: 1,
						package: "@example/other-admin",
					},
				},
			])
		).toThrow("provider theme");
	});
});
