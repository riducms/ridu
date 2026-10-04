import { describe, expect, test, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { en, fr } from "@riducms/translations";
import {
	SCHEMA_MANIFEST_VERSION,
	type AccessCapabilitiesEnvelope,
	type AuthSession,
	type OperationCapabilities,
	type SchemaCollection,
	type SchemaManifest,
} from "@riducms/protocol";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";

const { AdminRuntime } = await import("@admin/core/runtime/admin-runtime.svelte");

const LocalizedCollections = svelte`
	<script>
		let { runtime } = $props();
	</script>
	<p role="status">{runtime.localizedCollections.map(collection =>
		[collection.labels.plural, collection.fields[0]?.admin.label].join(": ")
	).join(", ")}</p>
`;

test("shares collection localization and refreshes readers after manifest and translator changes", async () => {
	const preferenceKey = "ridu:admin-language";
	const storedLanguage = localStorage.getItem(preferenceKey);
	const runtime = new AdminRuntime(
		{
			...fixtureClient(),
			setPreference: async <Value>(_key: string, value: Value) => value,
		},
		{ languages: [en, fr] }
	);
	const posts: SchemaCollection = {
		...collection("posts", false),
		labels: { singular: "Post", plural: "Posts", pluralTranslations: { fr: "Articles" } },
		fields: [
			{
				id: "status",
				name: "status",
				path: "status",
				type: "text",
				category: "scalar",
				required: false,
				unique: false,
				admin: { label: "Status", labelTranslations: { fr: "Statut" } },
			},
		],
	};
	runtime.manifest = { ...manifest, collections: [posts] };
	const localize = vi.spyOn(runtime.i18n, "text");
	try {
		await runtime.i18n.setLanguage("en");
		const screen = await render(LocalizedCollections, { runtime });
		await expect.element(screen.getByRole("status")).toHaveTextContent("Posts: Status");
		const calls = localize.mock.calls.length;
		expect(calls).toBeGreaterThan(0);
		expect(runtime.localizedCollections[0]?.labels.plural).toBe("Posts");
		expect(runtime.localizedCollections[0]?.fields[0]?.admin.label).toBe("Status");
		expect(localize.mock.calls).toHaveLength(calls);

		await runtime.i18n.setLanguage("fr");
		await expect.element(screen.getByRole("status")).toHaveTextContent("Articles: Statut");
		runtime.manifest = {
			...manifest,
			collections: [
				{ ...posts, labels: { ...posts.labels, pluralTranslations: { fr: "Publications" } } },
			],
		};
		await expect.element(screen.getByRole("status")).toHaveTextContent("Publications: Statut");
		await runtime.i18n.setLanguage("en");
		await expect.element(screen.getByRole("status")).toHaveTextContent("Posts: Status");

		// The translator can change its fallback without changing the active interface language.
		runtime.i18n.configure({
			languages: [en, fr].map(({ code, label }) => ({ code, label })),
			defaultLanguage: "fr",
		});
		expect(runtime.i18n.language).toBe("en");
		await expect.element(screen.getByRole("status")).toHaveTextContent("Publications: Statut");
		expect(posts.labels.plural).toBe("Posts");
		expect(posts.fields[0]?.admin.label).toBe("Status");
		await screen.unmount();
	} finally {
		localize.mockRestore();
		runtime.dispose();
		if (storedLanguage === null) localStorage.removeItem(preferenceKey);
		else localStorage.setItem(preferenceKey, storedLanguage);
	}
});

describe("route content locale", () => {
	test("prefers a supported URL locale, then the active locale, then the default", () => {
		const runtime = new AdminRuntime(fixtureClient());
		runtime.manifest = {
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
		};
		try {
			expect(runtime.resolveContentLocale("fr")).toBe("fr");
			for (const requested of [null, undefined, "unknown"])
				expect(runtime.resolveContentLocale(requested)).toBe("en");

			runtime.contentLocale = "fr";
			expect(runtime.resolveContentLocale("en")).toBe("en");
			for (const requested of [null, undefined, "unknown"])
				expect(runtime.resolveContentLocale(requested)).toBe("fr");
			expect(runtime.contentLocale).toBe("fr");
		} finally {
			runtime.dispose();
		}
	});

	test("does not invent a locale when localization is disabled", () => {
		const runtime = new AdminRuntime(fixtureClient());
		try {
			runtime.manifest = {
				...manifest,
				application: { ...manifest.application, localization: undefined },
			};
			expect(runtime.resolveContentLocale("fr")).toBeUndefined();
		} finally {
			runtime.dispose();
		}
	});
});

describe("admin runtime access refresh", () => {
	test("keeps a valid session and complete capability snapshot when bootstrap access fails", async () => {
		const previousCollections = {
			users: operations(true),
			posts: operations(false),
		};
		const previousGlobals = { settings: operations(false) };
		let logoutCalls = 0;
		const runtime = new AdminRuntime(
			fixtureClient({
				collectionAccess: async (slug) => {
					if (slug === "posts") throw new DOMException("Access request aborted", "AbortError");
					return access(true);
				},
				logout: async () => {
					logoutCalls += 1;
					return { loggedOut: true };
				},
			})
		);
		runtime.collectionOperations = previousCollections;
		runtime.globalOperations = previousGlobals;

		await runtime.bootstrap();

		expect(runtime.session).toEqual(session);
		expect(runtime.collectionOperations).toBe(previousCollections);
		expect(runtime.globalOperations).toBe(previousGlobals);
		expect(logoutCalls).toBe(0);
		expect(runtime.error).toBe("Access request aborted");
	});

	test("logs out when a complete access refresh explicitly denies admin access", async () => {
		let logoutCalls = 0;
		const runtime = new AdminRuntime(
			fixtureClient({
				collectionAccess: async () => access(false),
				logout: async () => {
					logoutCalls += 1;
					return { loggedOut: true };
				},
			})
		);

		await runtime.bootstrap();

		expect(runtime.collectionOperations.users?.admin).toBe(false);
		expect(runtime.session).toBeUndefined();
		expect(logoutCalls).toBe(1);
		expect(runtime.error).toBeUndefined();
	});

	test("ignores a failed refresh after a newer complete snapshot wins", async () => {
		let calls = 0;
		let rejectSuperseded!: (error: Error) => void;
		const runtime = new AdminRuntime(
			fixtureClient({
				collectionAccess: () => {
					calls += 1;
					if (calls > 1) return Promise.resolve(access(true));
					return new Promise((_, reject) => {
						rejectSuperseded = reject;
					});
				},
				logout: async () => ({ loggedOut: true }),
			})
		);
		runtime.manifest = { ...manifest, collections: [collection("users", true)] };

		const superseded = runtime.refreshAccess();
		expect(await runtime.refreshAccess()).toBe(true);
		rejectSuperseded(new DOMException("Superseded request aborted", "AbortError"));

		expect(await superseded).toBe(false);
		expect(runtime.collectionOperations.users?.admin).toBe(true);
	});
});

const session: AuthSession<AdminDocument> = {
	id: "session-1",
	collection: "users",
	user: { id: "user-1", email: "editor@riducms.test" },
	expiresAt: "2030-01-01T00:00:00Z",
};

const manifest: SchemaManifest = {
	version: SCHEMA_MANIFEST_VERSION,
	application: {
		name: "Runtime test",
		admin: { userCollectionId: "users", userCollectionSlug: "users" },
	},
	collections: [collection("users", true), collection("posts", false)],
	plugins: [],
};

function collection(slug: string, auth: boolean): SchemaCollection {
	return {
		id: slug,
		slug,
		labels: { singular: slug, plural: slug },
		admin: {},
		capabilities: { auth, upload: false, versions: false, trash: false, locking: false },
		fields: [],
	};
}

function operations(admin: boolean): OperationCapabilities {
	return {
		admin,
		create: true,
		read: true,
		readVersions: true,
		update: true,
		delete: true,
		duplicate: true,
		publish: true,
		unpublish: true,
		restoreDeleted: true,
		deletePermanent: true,
		selectAll: true,
	};
}

function access(admin: boolean): AccessCapabilitiesEnvelope {
	return { operations: operations(admin), fields: {} };
}

function fixtureClient(
	options: {
		collectionAccess: (slug: string) => Promise<AccessCapabilitiesEnvelope>;
		logout: () => Promise<{ loggedOut: true }>;
	} = {
		collectionAccess: async () => access(true),
		logout: async () => ({ loggedOut: true }),
	}
): AdminClient {
	return {
		schema: async () => manifest,
		auth: {
			bootstrap: async () => ({ available: false }),
			getSession: async () => session,
			logout: options.logout,
		},
		collectionAccess: options.collectionAccess,
	} as unknown as AdminClient;
}
