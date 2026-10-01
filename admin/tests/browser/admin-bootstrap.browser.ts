import { afterEach, describe, expect, test, vi } from "vitest";
import {
	ADMIN_PREPARED_ROUTE_STATE_VERSION,
	SCHEMA_MANIFEST_VERSION,
	type AdminPreparedRouteStateV1,
	type AdminPreparedRouteDataV1,
	type AdminPreparedRuntimeV1,
	type SchemaManifest,
} from "@riducms/protocol";

import type { AdminClient } from "@admin/core/api/admin-client";
import { saveFormDraft } from "@admin/core/forms/form-draft-recovery";
import { createAdminLoader } from "@riducms/sdk";
import { withAdminLoader, type AdminExtensionProps, type AdminLoaderProps } from "@riducms/plugin";
import type { Component } from "svelte";
import {
	normalizedURLIdentity,
	validateAdminPreparedState,
} from "@admin/core/bootstrap/admin-prepared-state";

const { AdminBootstrapCoordinator } = await import("@admin/core/bootstrap/admin-bootstrap");
const { AdminRuntime } = await import("@admin/core/runtime/admin-runtime.svelte");

type PreparedState = Extract<AdminPreparedRouteStateV1, { outcome: "prepared" }>;

const originalURL = window.location.href;
const originalFetch = window.fetch;
const runtimes: InstanceType<typeof AdminRuntime>[] = [];

afterEach(() => {
	for (const runtime of runtimes.splice(0)) runtime.dispose();
	document.getElementById("ridu-admin-initial-state")?.remove();
	document.querySelector('meta[name="ridu-admin-build-id"]')?.remove();
	sessionStorage.clear();
	history.replaceState({}, "", originalURL);
	window.fetch = originalFetch;
	vi.restoreAllMocks();
});

describe("prepared state boundary", () => {
	test.each([
		["/admin/", "/admin"],
		["/", "/"],
		["/admin?q=hello%20world&page=2", "/admin?page=2&q=hello+world"],
		["/admin?b=1&a=two&a=one", "/admin?a=two&a=one&b=1"],
		["/admin?z=&a&z=last", "/admin?a=&z=&z=last"],
		["/admin?\uE000=private&\u{10000}=astral", "/admin?%F0%90%80%80=astral&%EE%80%80=private"],
	])("normalizes %s without changing repeated-value order", (input, expected) => {
		expect(normalizedURLIdentity(new URL(input, location.origin))).toBe(expected);
	});

	test.each([
		{ outcome: "redirect", location: "/admin/login" },
		{ outcome: "reload" },
		{
			outcome: "fallback",
			diagnostic: { code: "snapshot_too_large", message: "Use client loader" },
		},
	])("accepts the $outcome payload without route data", (outcome) => {
		const state = {
			...preparedState("/admin", ""),
			runtime: undefined,
			route: undefined,
			...outcome,
		};
		expect(validateAdminPreparedState(state, new URL("/admin", location.origin))).toEqual(state);
	});

	test.each([
		{ outcome: "redirect", location: 123 },
		{ outcome: "reload" }, // The base fixture still carries its runtime and route.
		{ outcome: "fallback", diagnostic: undefined },
		{
			outcome: "fallback",
			diagnostic: { code: "fallback", message: "Fallback" },
			location: "/admin/login",
		},
		{ outcome: "unknown" },
		{ moduleGroups: ["entry", "entry"] },
		{ moduleGroups: ["entry", "reference-browser"] },
		{ moduleGroups: [] },
	])("rejects invalid outcome or module data: %j", (patch) => {
		expect(() =>
			validateAdminPreparedState(
				{ ...preparedState("/admin", ""), ...patch },
				new URL("/admin", location.origin)
			)
		).toThrow();
	});

	test("requires the document chunk before a create route can be prepared", () => {
		const state = preparedState("/admin/collections/pages/create", "", {
			kind: "collection-create",
			create: { values: {}, access: { value: access(true) } },
		});
		const url = new URL(state.pathname, location.origin);
		expect(validateAdminPreparedState(state, url)).toEqual(state);
		expect(() => validateAdminPreparedState({ ...state, moduleGroups: ["entry"] }, url)).toThrow();
	});

	test("requires the bulk upload workspace before revealing a prepared upload route", () => {
		const state = {
			...preparedState("/admin/collections/media/upload", "", {
				kind: "upload",
				access: { value: access(true) },
			}),
			moduleGroups: ["entry", "bulk-upload"],
		};
		const url = new URL(state.pathname, location.origin);

		expect(validateAdminPreparedState(state, url)).toEqual(state);
		expect(() => validateAdminPreparedState({ ...state, moduleGroups: ["entry"] }, url)).toThrow();
	});
});

describe("admin bootstrap coordinator", () => {
	test.each(["after", "replace"] as const)(
		"validates loader data for a %s dashboard panel",
		(position) => {
			const loader = createAdminLoader<{}, { count: number }>({
				key: "summary",
				input: { kind: "object" },
				output: { kind: "object", fields: { count: { kind: "number" } } },
			});
			const component: Component<
				AdminExtensionProps & AdminLoaderProps<{ count: number }>
			> = () => ({});
			const runtime = new AdminRuntime({} as AdminClient, {
				dashboardPanels: [
					{ key: "summary", position, ...withAdminLoader(loader, component) },
					// A replacement hides other panels; their absent seeds must not block it.
					...(position === "replace"
						? [
								{
									key: "hidden",
									...withAdminLoader({ ...loader, key: "hidden" }, component),
								},
							]
						: []),
				],
			});
			runtimes.push(runtime);
			history.replaceState({}, "", "/admin");
			const coordinator = new AdminBootstrapCoordinator();
			coordinator.configure(runtime);
			const state = preparedState("/admin", "");

			expect(coordinator.stage(state)).toBe(false);
			expect(
				coordinator.stage({ ...state, loaders: { summary: { value: { count: "bad" } } } })
			).toBe(false);
			expect(coordinator.stage({ ...state, loaders: { summary: { value: { count: 2 } } } })).toBe(
				true
			);
			expect(coordinator.stage({ ...state, loaders: { summary: { error: errorPayload } } })).toBe(
				true
			);
		}
	);

	test("does not seed a dashboard replacement without a loader", () => {
		const runtime = new AdminRuntime({} as AdminClient, {
			dashboardPanels: [{ key: "custom", position: "replace", component: () => ({}) }],
		});
		runtimes.push(runtime);
		history.replaceState({}, "", "/admin");
		const coordinator = new AdminBootstrapCoordinator();
		coordinator.configure(runtime);
		expect(coordinator.stage(preparedState("/admin", ""))).toBe(false);
	});

	test("prepares a loaded not-found view beneath a literal custom route", async () => {
		const loader = createAdminLoader<{}, { count: number }>({
			key: "missing",
			input: { kind: "object" },
			output: { kind: "object", fields: { count: { kind: "number" } } },
		});
		const component: Component<
			AdminExtensionProps & AdminLoaderProps<{ count: number }>
		> = () => ({});
		const runtime = new AdminRuntime({} as AdminClient, {
			routes: [{ path: "report", component: () => ({}) }],
			coreViews: [{ key: "missing", surface: "notFound", ...withAdminLoader(loader, component) }],
		});
		runtimes.push(runtime);
		history.replaceState({}, "", "/admin/report/unknown");
		embedState({
			...preparedState("/admin/report/unknown", "", { kind: "custom" }),
			loaders: { missing: { value: { count: 1 } } },
		});
		const coordinator = new AdminBootstrapCoordinator();
		coordinator.configure(runtime);
		await coordinator.prepareInitial();
		expect(coordinator.loaderData("missing", "/report/unknown")).toEqual({ value: { count: 1 } });
	});
	test("accepts only the selected custom view's exact valid loader seed", async () => {
		const loader = createAdminLoader<{}, { count: number }>({
			key: "report",
			input: { kind: "object" },
			output: { kind: "object", fields: { count: { kind: "number" } } },
		});
		const component: Component<
			AdminExtensionProps & AdminLoaderProps<{ count: number }>
		> = () => ({});
		const runtime = new AdminRuntime({} as AdminClient, {
			routes: [{ path: "report", ...withAdminLoader(loader, component) }],
		});
		runtimes.push(runtime);
		history.replaceState({}, "", "/admin/report?q=one");
		const state = {
			...preparedState("/admin/report", "?q=one", { kind: "custom" }),
			loaders: { report: { value: { count: 1 } } },
		};
		embedState(state);
		const coordinator = new AdminBootstrapCoordinator();
		coordinator.configure(runtime);
		await coordinator.prepareInitial();
		expect(coordinator.loaderData("report", "/report", "?q=one")).toEqual({ value: { count: 1 } });
		expect(coordinator.loaderData("report", "/report", "?q=other")).toBeUndefined();
		expect(
			coordinator.stage({ ...state, loaders: { report: { value: { count: "wrong" } } } })
		).toBe(false);
		expect(coordinator.stage({ ...state, loaders: { other: { value: { count: 1 } } } })).toBe(
			false
		);
		expect(coordinator.stage({ ...state, route: { kind: "dashboard" as const } })).toBe(false);
		expect(coordinator.stage({ ...state, contextKey: "stale" })).toBe(false);
	});
	test("exposes exact list data without impersonating SDK calls", async () => {
		history.replaceState({}, "", "/admin/collections/pages?a=one&a=two");
		embedState(preparedState("/admin/collections/pages", "?a=one&a=two", listRoute("prepared")));
		let networkCalls = 0;
		const client = {
			list: async () => {
				networkCalls += 1;
				return page("network");
			},
		} as unknown as AdminClient;
		const coordinator = new AdminBootstrapCoordinator();
		coordinator.configure(createRuntime());

		await coordinator.prepareInitial();
		expect(
			listData(coordinator, "/collections/pages", location.search)?.page?.value?.docs[0]?.title
		).toBe("prepared");
		expect(listData(coordinator, "/collections/pages", "?a=other")).toBeUndefined();
		expect((await client.list("pages", {})).docs[0]?.title).toBe("network");
		expect(networkCalls).toBe(1);
		coordinator.discardRouteData(coordinator.initialState!);
		expect(listData(coordinator, "/collections/pages", location.search)).toBeUndefined();
	});

	test("rejects stale and malformed embedded states", () => {
		history.replaceState({}, "", "/admin/collections/pages?page=2");
		embedState(preparedState("/admin/collections/pages", "?page=1"));
		expect(new AdminBootstrapCoordinator().initialState).toBeUndefined();

		embedState({
			...preparedState("/admin/collections/pages", "?page=2"),
			route: {
				kind: "collection-list",
				data: { ...listRoute("bad").data, page: { value: page("bad"), error: errorPayload } },
			},
		} as unknown as AdminPreparedRouteStateV1);
		expect(new AdminBootstrapCoordinator().initialState).toBeUndefined();
	});

	test("does not stage route data when runtime adoption failed", async () => {
		history.replaceState({}, "", "/admin/collections/pages");
		const state = preparedState("/admin/collections/pages", "", listRoute("invalid runtime seed"));
		embedState({
			...state,
			runtime: {
				...preparedRuntime,
				manifest: {
					...manifest,
					blocks: [{ slug: "INVALID", fields: [] }],
				} as unknown as SchemaManifest,
			},
		});
		const coordinator = new AdminBootstrapCoordinator();
		coordinator.configure(createRuntime());
		const list = vi.fn(async () => page("network"));
		const client = { list } as unknown as AdminClient;
		const runtime = new AdminRuntime({} as AdminClient);
		expect(() => runtime.adoptPrepared(coordinator.initialState!.runtime!)).toThrow();
		await coordinator.prepareInitial({ stageRoute: false });
		expect(coordinator.initialState).toBeUndefined();
		expect(coordinator.contextKey).toBe("");
		expect(document.documentElement.dataset.riduContext).toBeUndefined();
		expect((await client.list("pages", {})).docs[0]?.title).toBe("network");
		expect(list).toHaveBeenCalledTimes(1);
	});

	test("never stages a navigation response after its router request aborts", async () => {
		history.replaceState({}, "", "/admin");
		embedState(preparedState("/admin", ""));
		const coordinator = new AdminBootstrapCoordinator();
		coordinator.configure(createRuntime());
		await coordinator.prepareInitial();
		let resolveFetch!: (response: Response) => void;
		window.fetch = vi.fn(
			() => new Promise<Response>((resolve) => (resolveFetch = resolve))
		) as unknown as typeof fetch;
		const abort = new AbortController();
		const request = new Request(`${location.origin}/admin/collections/pages`, {
			signal: abort.signal,
		});
		const pending = coordinator.loader({ request });
		abort.abort();
		resolveFetch(
			new Response(
				JSON.stringify(
					navigationState(preparedState("/admin/collections/pages", "", listRoute("aborted")))
				),
				{ status: 200, headers: { "Content-Type": "application/json" } }
			)
		);
		expect(await pending).toBeNull();
		expect(
			listData(coordinator, "/collections/pages", new URL(request.url).search)
		).toBeUndefined();
		let networkCalls = 0;
		const client = {
			list: async () => {
				networkCalls += 1;
				return page("network");
			},
		} as unknown as AdminClient;
		history.replaceState({}, "", "/admin/collections/pages");
		expect((await client.list("pages", {})).docs[0]?.title).toBe("network");
		expect(networkCalls).toBe(1);
	});

	test("exposes a completed navigation seed only when the router commits it", async () => {
		history.replaceState({}, "", "/admin");
		embedState(preparedState("/admin", ""));
		const coordinator = new AdminBootstrapCoordinator();
		const runtime = createRuntime();
		runtime.adoptPrepared(preparedRuntime);
		const initialManifest = runtime.manifest;
		const initialPreferences = runtime.preparedPreferences;
		coordinator.configure(runtime);
		await coordinator.prepareInitial();
		const destination = navigationState(
			preparedState("/admin/collections/pages", "?page=2", listRoute("prepared navigation"))
		);
		window.fetch = vi.fn(
			async () =>
				new Response(JSON.stringify(destination), {
					status: 200,
					headers: { "Content-Type": "application/json" },
				})
		) as unknown as typeof fetch;
		const list = vi.fn(async () => page("network"));
		const client = { list } as unknown as AdminClient;
		const request = new Request(`${location.origin}/admin/collections/pages?page=2`);
		const state = await coordinator.loader({ request });
		expect((await client.list("pages", {})).docs[0]?.title).toBe("network");
		expect(
			coordinator.commit(state, new URL(`${location.origin}/admin/collections/pages?page=2`))
		).toBe(true);
		expect(runtime.manifest).toBe(initialManifest);
		expect(runtime.preparedPreferences).toBe(initialPreferences);
		expect(state?.runtime).toBeUndefined();
		expect(
			listData(coordinator, "/collections/pages", new URL(request.url).search)?.page?.value?.docs[0]
				?.title
		).toBe("prepared navigation");
		expect((await client.list("pages", {})).docs[0]?.title).toBe("network");
		expect(list).toHaveBeenCalledTimes(2);
	});

	test("rejects incomplete domain data before staging", async () => {
		history.replaceState({}, "", "/admin/collections/pages");
		embedState({
			...preparedState("/admin/collections/pages", ""),
			route: { kind: "collection-list", data: { page: { value: { incomplete: true } } } },
		} as unknown as PreparedState);
		const coordinator = new AdminBootstrapCoordinator();
		coordinator.configure(createRuntime());
		await coordinator.prepareInitial();
		expect(coordinator.routeData("/collections/pages")).toBeUndefined();
		expect(coordinator.initialState).toBeUndefined();
	});

	for (const base of ["/control/", "/"]) {
		test(`stages basename-relative list data under ${base}`, async () => {
			const pathname = `${base === "/" ? "" : "/control"}/collections/pages`;
			history.replaceState({}, "", pathname);
			embedState(preparedState(pathname, "", listRoute("custom base")));
			const coordinator = new AdminBootstrapCoordinator();
			coordinator.configure(createRuntime(base));
			await coordinator.prepareInitial();
			expect(listData(coordinator, "/collections/pages")?.page?.value?.docs[0]?.title).toBe(
				"custom base"
			);
		});
	}

	test("adopts a complete runtime without invoking browser bootstrap reads", () => {
		const client = new Proxy(
			{},
			{
				get: () => () => {
					throw new Error("unexpected browser bootstrap read");
				},
			}
		) as AdminClient;
		const runtime = new AdminRuntime(client);
		runtime.adoptPrepared(preparedRuntime);
		expect(runtime.loading).toBe(false);
		expect(runtime.manifest?.application.name).toBe("Prepared application");
		expect(runtime.contentLocale).toBe("en");
		expect(runtime.resolvedTheme).toBe("dark");
	});

	test("navigation runtime cannot roll back local display preferences", () => {
		const runtime = new AdminRuntime({} as AdminClient);
		const localizedManifest = {
			...manifest,
			application: {
				...manifest.application,
				localization: {
					defaultLocale: "en",
					fallback: true,
					locales: [
						{ code: "en", label: "English" },
						{ code: "fr", label: "French" },
					],
				},
			},
		} satisfies SchemaManifest;
		runtime.adoptPrepared({ ...preparedRuntime, manifest: localizedManifest });
		runtime.adoptPreparedNavigation({
			collectionOperations: { pages: access(false).operations },
			globalOperations: {},
			contentLocale: "fr",
		});
		expect(runtime.themePreference).toBe("dark");
		expect(runtime.i18n.language).toBe("en");
		expect(runtime.i18n.timeZone).toBeUndefined();
		expect(runtime.contentLocale).toBe("fr");
		expect(runtime.collectionOperations.pages.create).toBe(false);
	});

	test.each(["cold", "navigation"])(
		"finishes restored-create access before staging the %s seed",
		async (mode) => {
			history.replaceState({}, "", mode === "cold" ? "/admin/collections/pages/create" : "/admin");
			const collection = {
				id: "pages-id",
				slug: "pages",
				fields: [
					{
						id: "owner-id",
						name: "owner",
						path: "owner",
						type: "text",
						category: "scalar",
						admin: {},
						text: {},
					},
				],
				admin: {},
				capabilities: {},
				labels: { singular: "Page", plural: "Pages" },
			} as unknown as SchemaManifest["collections"][number];
			saveFormDraft(
				collection,
				undefined,
				{ owner: "actor-2" },
				{},
				{},
				mode === "cold" ? "en" : "fr"
			);
			const destination = {
				...preparedState("/admin/collections/pages/create", ""),
				runtime: {
					...preparedRuntime,
					manifest: { ...manifest, collections: [collection] },
				},
				route: {
					kind: "collection-create",
					create: { values: {}, access: { value: access(true) } },
				},
			} satisfies PreparedState;
			embedState(
				mode === "cold"
					? destination
					: {
							...preparedState("/admin", ""),
							runtime: destination.runtime,
						}
			);
			const accessRead = vi.fn(async () => access(false));
			const coordinator = new AdminBootstrapCoordinator();
			const runtime = createRuntime("/admin", {
				collectionAccess: accessRead,
			} as unknown as AdminClient);
			runtime.adoptPrepared(destination.runtime);
			coordinator.configure(runtime);
			await coordinator.prepareInitial();
			if (mode === "navigation") {
				const state = navigationState(destination);
				state.navigation.contentLocale = "fr";
				window.fetch = vi.fn(
					async () => new Response(JSON.stringify(state))
				) as unknown as typeof fetch;
				const request = new Request(`${location.origin}/admin/collections/pages/create`);
				const result = await coordinator.loader({ request });
				expect(coordinator.isRouteStaged(state)).toBe(false);
				expect(coordinator.commit(result, new URL(request.url))).toBe(true);
			}
			expect(accessRead).toHaveBeenCalledWith(
				"pages",
				expect.objectContaining({ data: { owner: "actor-2" } })
			);
			const route = coordinator.routeData("/collections/pages/create");
			expect(route?.kind).toBe("collection-create");
			if (route?.kind !== "collection-create") throw new Error("Expected create data");
			expect(route.create.values).toEqual({ owner: "actor-2" });
			expect(route.create.access.value?.operations.create).toBe(false);
			expect(accessRead).toHaveBeenCalledWith(
				"pages",
				expect.objectContaining({
					locale: mode === "cold" ? "en" : "fr",
				})
			);
			expect(accessRead).toHaveBeenCalledTimes(1);
		}
	);

	test("compact state cannot initialize a cold document", () => {
		history.replaceState({}, "", "/admin");
		embedState(navigationState(preparedState("/admin", "")));
		expect(new AdminBootstrapCoordinator().initialState).toBeUndefined();
	});

	test("rejects a fallback carrying a redirect location", () => {
		history.replaceState({}, "", "/admin");
		embedState({
			...preparedState("/admin", ""),
			outcome: "fallback",
			route: undefined,
			location: "/admin/login",
			diagnostic: { code: "snapshot_too_large", message: "Fallback" },
		} as unknown as AdminPreparedRouteStateV1);
		expect(new AdminBootstrapCoordinator().initialState).toBeUndefined();
	});

	test("browser-only startup navigates without requesting Go-prepared route state", async () => {
		history.replaceState({}, "", "/admin");
		const coordinator = new AdminBootstrapCoordinator();
		coordinator.configure(createRuntime());
		await coordinator.prepareInitial();
		window.fetch = vi.fn() as unknown as typeof fetch;

		expect(
			await coordinator.loader({
				request: new Request(`${location.origin}/admin/collections/pages`),
			})
		).toBeNull();
		expect(window.fetch).not.toHaveBeenCalled();
	});

	test.each(["missing-context", "wrong-context", "malformed"])(
		"rejects %s navigation state",
		async (failure) => {
			history.replaceState({}, "", "/admin");
			const meta = document.createElement("meta");
			meta.name = "ridu-admin-build-id";
			meta.content = preparedState("/admin", "").buildId;
			document.head.append(meta);
			if (failure !== "missing-context") embedState(preparedState("/admin", ""));
			const coordinator = new AdminBootstrapCoordinator();
			coordinator.configure(createRuntime());
			await coordinator.prepareInitial();
			const state = navigationState(
				preparedState("/admin/collections/pages", "", listRoute("bad"))
			);
			if (failure === "wrong-context") state.contextKey = "abcdef0123456789abcdef0123456789";
			if (failure === "malformed") Reflect.deleteProperty(state.navigation, "collectionOperations");
			window.fetch = vi.fn(
				async () => new Response(JSON.stringify(state))
			) as unknown as typeof fetch;
			expect(
				await coordinator.loader({
					request: new Request(`${location.origin}/admin/collections/pages`),
				})
			).toBeNull();
			expect(window.fetch).toHaveBeenCalledTimes(1);
			expect(coordinator.routeData("/collections/pages")).toBeUndefined();
		}
	);

	test("rejects a snapshot produced by a different admin build", () => {
		history.replaceState({}, "", "/admin");
		const meta = document.createElement("meta");
		meta.name = "ridu-admin-build-id";
		meta.content = "abcdef0123456789abcdef01";
		document.head.append(meta);
		embedState(preparedState("/admin", ""));
		expect(new AdminBootstrapCoordinator().initialState).toBeUndefined();
	});
});

function embedState(state: AdminPreparedRouteStateV1) {
	const template = document.createElement("template");
	template.id = "ridu-admin-initial-state";
	template.content.append(document.createTextNode(JSON.stringify(state)));
	document.body.append(template);
}

function createRuntime(basePath = "/admin", client = {} as AdminClient) {
	const runtime = new AdminRuntime(client, {}, basePath);
	runtimes.push(runtime);
	return runtime;
}

function navigationState(state: PreparedState & { runtime: AdminPreparedRuntimeV1 }) {
	const { runtime, ...route } = state;
	return {
		...route,
		navigation: {
			collectionOperations: runtime.collectionOperations,
			globalOperations: runtime.globalOperations,
			contentLocale: runtime.contentLocale,
		},
	};
}

function preparedState(
	pathname: string,
	search: string,
	route: AdminPreparedRouteDataV1 = pathname === "/admin"
		? { kind: "dashboard" }
		: listRoute("empty")
): PreparedState & { runtime: AdminPreparedRuntimeV1 } {
	return {
		version: ADMIN_PREPARED_ROUTE_STATE_VERSION,
		outcome: "prepared",
		pathname,
		search,
		contextKey: "0123456789abcdef0123456789abcdef",
		fingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		buildId: "0123456789abcdef01234567",
		moduleGroups: pathname.includes("/create") ? ["entry", "document"] : ["entry"],
		runtime: preparedRuntime,
		route,
	};
}

function listRoute(title: string) {
	return {
		kind: "collection-list",
		data: {
			counts: {},
			query: { trash: false },
			page: { value: page(title) },
			preferences: { workspace: { value: null }, presets: { value: null } },
		},
	} satisfies AdminPreparedRouteDataV1;
}

function listData(
	coordinator: InstanceType<typeof AdminBootstrapCoordinator>,
	pathname: string,
	search = ""
) {
	const route = coordinator.routeData(pathname, search);
	return route?.kind === "collection-list" ? route.data : undefined;
}

function page(title: string) {
	return {
		access: { collection: access(true), documents: { "page-1": access(true) } },
		docs: [{ id: "page-1", title }],
		pagination: {
			page: 1,
			limit: 25,
			totalDocs: 1,
			totalPages: 1,
			hasNextPage: false,
			hasPrevPage: false,
		},
	};
}

function access(create: boolean) {
	return {
		operations: {
			admin: false,
			create,
			read: true,
			readVersions: false,
			update: false,
			delete: false,
			publish: false,
			unpublish: false,
			duplicate: false,
			selectAll: false,
			restoreDeleted: false,
			deletePermanent: false,
		},
		fields: {},
	};
}

const manifest: SchemaManifest = {
	version: SCHEMA_MANIFEST_VERSION,
	application: {
		name: "Prepared application",
		localization: {
			defaultLocale: "en",
			fallback: true,
			locales: [
				{ code: "en", label: "English" },
				{ code: "fr", label: "French" },
			],
		},
	},
	collections: [],
	plugins: [],
};

const preparedRuntime: AdminPreparedRuntimeV1 = {
	manifest,
	authBootstrapAvailable: false,
	collectionOperations: {},
	globalOperations: {},
	theme: "dark",
	contentLocale: "en",
	adminLanguage: "en",
	adminTimeZone: "UTC",
	preferences: {},
};

const errorPayload = {
	code: "internal" as const,
	status: 500,
	message: "bad",
	issues: [],
};
