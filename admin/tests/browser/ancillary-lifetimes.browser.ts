import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { createMemoryRouter } from "@hvniel/svelte-router";
import {
	SCHEMA_MANIFEST_VERSION,
	type PreviewToken,
	type SchemaCollection,
} from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";

import App from "@admin/app.svelte";
import type { AdminClient, AdminDocument, AdminVersion } from "@admin/core/api/admin-client";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { VersionHistoryController } from "@admin/features/versions/version-history-controller.svelte";
import VersionHistoryRoute from "@admin/features/versions/version-history-route.svelte";

const PreviewHarness = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { TooltipProvider } from "@riducms/ui";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import LivePreviewPanel from "../../src/features/documents/live-preview-panel.svelte";
		let { runtime, documentID, visible = true } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<TooltipProvider>
		{#if visible}
			<LivePreviewPanel
				preview={{ url: "/preview/{id}" }}
				collection="posts"
				{documentID}
				resource="collection"
				values={{ title: documentID }}
				onclose={() => {}}
			/>
		{/if}
	</TooltipProvider>
`;

const RuntimeProvider = svelte`
	<script>
		import { RouterProvider } from "@hvniel/svelte-router";
		import { createAdminScroll } from "../../src/core/routing/admin-scroll.svelte";
		import { setAdminI18n } from "@riducms/plugin";
		import { setNotificationCenter } from "../../src/core/notifications/notification-center.svelte";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { AdminBootstrapCoordinator, setAdminBootstrapCoordinator } from "../../src/core/bootstrap/admin-bootstrap";
		let { router, runtime, notifications } = $props();
		createAdminScroll();
		const bootstrap = new AdminBootstrapCoordinator();
		bootstrap.configure(runtime);
		setAdminBootstrapCoordinator(bootstrap);
		setAdminRuntime(runtime);
		setNotificationCenter(notifications);
		setAdminI18n(runtime.i18n);
	</script>
	<RouterProvider {router} />
`;

it("revokes a preview token returned after its document request was superseded", async () => {
	const first = Promise.withResolvers<PreviewToken>();
	const second = Promise.withResolvers<PreviewToken>();
	const createPreviewToken = vi
		.fn()
		.mockImplementationOnce(() => first.promise)
		.mockImplementationOnce(() => second.promise);
	const revokePreviewToken = vi.fn(async () => ({ success: true }));
	const runtime = new AdminRuntime({
		createPreviewToken,
		revokePreviewToken,
	} as unknown as AdminClient);
	const props = { runtime, documentID: "one", visible: true };
	const screen = await render(PreviewHarness, props);
	try {
		await expect.poll(() => createPreviewToken).toHaveBeenCalledOnce();
		await screen.rerender({ ...props, documentID: "two" });
		await expect.poll(() => createPreviewToken).toHaveBeenCalledTimes(2);

		second.resolve(previewToken("current-token", "two"));
		await expect
			.element(screen.getByTitle("Live preview"))
			.toHaveAttribute("src", expect.stringContaining("current-token"));
		first.resolve(previewToken("stale-token", "one"));

		await expect
			.poll(() => revokePreviewToken)
			.toHaveBeenCalledWith("stale-token", {
				keepalive: true,
			});
		expect(screen.getByTitle("Live preview").element().getAttribute("src")).toContain(
			"current-token"
		);
	} finally {
		await screen.unmount();
		runtime.dispose();
	}
});

it("revokes a preview token that succeeds after the panel unmounts", async () => {
	const authorization = Promise.withResolvers<PreviewToken>();
	const revokePreviewToken = vi.fn(async () => ({ success: true }));
	const runtime = new AdminRuntime({
		createPreviewToken: () => authorization.promise,
		revokePreviewToken,
	} as unknown as AdminClient);
	const screen = await render(PreviewHarness, { runtime, documentID: "one", visible: true });

	await screen.rerender({ runtime, documentID: "one", visible: false });
	authorization.resolve(previewToken("late-token", "one"));
	await expect
		.poll(() => revokePreviewToken)
		.toHaveBeenCalledWith("late-token", { keepalive: true });
	await screen.unmount();
	runtime.dispose();
});

it("revokes the active preview token when the panel unmounts", async () => {
	const revokePreviewToken = vi.fn(async () => ({ success: true }));
	const runtime = new AdminRuntime({
		createPreviewToken: async () => previewToken("active-token", "one"),
		revokePreviewToken,
	} as unknown as AdminClient);
	const props = { runtime, documentID: "one", visible: true };
	const screen = await render(PreviewHarness, props);

	await expect.element(screen.getByTitle("Live preview")).toBeVisible();
	await screen.rerender({ ...props, visible: false });
	await expect
		.poll(() => revokePreviewToken)
		.toHaveBeenCalledWith("active-token", {
			keepalive: true,
		});
	await screen.unmount();
	runtime.dispose();
});

it("removes the runtime's system-theme listener on disposal", () => {
	const addEventListener = vi.fn();
	const removeEventListener = vi.fn();
	const mediaQuery = {
		matches: false,
		media: "(prefers-color-scheme: dark)",
		onchange: null,
		addEventListener,
		removeEventListener,
		addListener: vi.fn(),
		removeListener: vi.fn(),
		dispatchEvent: vi.fn(() => true),
	};
	const matchMedia = vi.spyOn(window, "matchMedia").mockReturnValue(mediaQuery);
	const runtime = new AdminRuntime({} as AdminClient);
	expect(addEventListener).toHaveBeenCalledOnce();
	const listener = addEventListener.mock.calls[0]?.[1];
	runtime.dispose();
	expect(removeEventListener).toHaveBeenCalledWith("change", listener);
	matchMedia.mockRestore();
});

it("disposes the runtime owned by the mounted app", async () => {
	const dispose = vi.spyOn(AdminRuntime.prototype, "dispose");
	const destroyNotifications = vi.spyOn(NotificationCenter.prototype, "destroy");
	const clientFactory = () =>
		({ schema: () => new Promise(() => undefined) }) as unknown as AdminClient;
	const screen = await render(App, { clientFactory });
	try {
		await screen.unmount();
		expect(dispose).toHaveBeenCalledOnce();
		expect(destroyNotifications).toHaveBeenCalledOnce();
	} finally {
		dispose.mockRestore();
		destroyNotifications.mockRestore();
	}
});

it("ignores restore completion after the retained version route changes document", async () => {
	const restore = Promise.withResolvers<AdminDocument>();
	let restoreSettled = false;
	void restore.promise.finally(() => {
		restoreSettled = true;
	});
	const client = versionClient(restore.promise);
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Version ownership" },
		collections: [versionedCollection],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	const notifications = new NotificationCenter();
	const success = vi.spyOn(notifications, "success").mockReturnValue(0);
	const error = vi.spyOn(notifications, "error").mockReturnValue(0);
	const router = createMemoryRouter(
		[
			{
				path: "/collections/:collection/:document/versions/:revision",
				Component: VersionHistoryRoute,
			},
		],
		{ initialEntries: ["/collections/posts/one/versions/1"] }
	);
	const screen = await render(RuntimeProvider, { router, runtime, notifications });
	try {
		await expect
			.element(screen.getByRole("button", { name: "Restore this version" }))
			.toBeVisible();
		await screen.getByRole("button", { name: "Restore this version" }).click();
		await screen.getByRole("button", { name: "Confirm", exact: true }).click();
		await router.navigate("/collections/posts/two/versions/1");
		await expect
			.poll(() => client.find)
			.toHaveBeenCalledWith(
				"posts",
				"two",
				expect.objectContaining({ signal: expect.any(AbortSignal) })
			);
		await expect
			.element(screen.getByRole("button", { name: "Restore this version" }))
			.toBeEnabled();

		restore.resolve({ id: "one", title: "Restored", _revision: 3 });
		await expect.poll(() => restoreSettled).toBe(true);
		await expect
			.poll(() => router.state.location.pathname)
			.toBe("/collections/posts/two/versions/1");
		expect(runtime.documentRevision).toBe(0);
		expect(success).not.toHaveBeenCalled();
		expect(error).not.toHaveBeenCalled();
	} finally {
		await screen.unmount();
		router.dispose();
		runtime.dispose();
	}
});

it("ignores restore failure after the retained version route changes document", async () => {
	const restore = Promise.withResolvers<AdminDocument>();
	let restoreSettled = false;
	void restore.promise.catch(() => {
		restoreSettled = true;
	});
	const client = versionClient(restore.promise);
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Version ownership" },
		collections: [versionedCollection],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	const notifications = new NotificationCenter();
	const success = vi.spyOn(notifications, "success").mockReturnValue(0);
	const error = vi.spyOn(notifications, "error").mockReturnValue(0);
	const router = createMemoryRouter(
		[
			{
				path: "/collections/:collection/:document/versions/:revision",
				Component: VersionHistoryRoute,
			},
		],
		{ initialEntries: ["/collections/posts/one/versions/1"] }
	);
	const screen = await render(RuntimeProvider, { router, runtime, notifications });
	try {
		await expect
			.element(screen.getByRole("button", { name: "Restore this version" }))
			.toBeVisible();
		await screen.getByRole("button", { name: "Restore this version" }).click();
		await screen.getByRole("button", { name: "Confirm", exact: true }).click();
		await router.navigate("/collections/posts/two/versions/1");
		await expect
			.poll(() => client.find)
			.toHaveBeenCalledWith(
				"posts",
				"two",
				expect.objectContaining({ signal: expect.any(AbortSignal) })
			);

		restore.reject(new Error("Old restore failed"));
		await expect.poll(() => restoreSettled).toBe(true);
		expect(runtime.documentRevision).toBe(0);
		expect(success).not.toHaveBeenCalled();
		expect(error).not.toHaveBeenCalled();
	} finally {
		await screen.unmount();
		router.dispose();
		runtime.dispose();
	}
});

it("clears the preceding version document before a retained route loads another document", async () => {
	const nextHistory = Promise.withResolvers<AdminVersion[]>();
	const fixture = await versionRouteFixture("/collections/posts/one/versions/1", {
		versions: (async (_slug: string, id: string) =>
			id === "one" ? [version] : nextHistory.promise) as AdminClient["versions"],
	});
	try {
		const restore = fixture.screen.getByRole("button", {
			name: "Restore this version",
			exact: true,
		});
		await expect.element(restore).toBeVisible();
		await fixture.router.navigate("/collections/posts/two/versions/1");
		await expect
			.poll(() => fixture.client.find)
			.toHaveBeenCalledWith("posts", "two", expect.any(Object));
		await expect.element(restore).not.toBeInTheDocument();

		nextHistory.reject(new Error("Destination history unavailable"));
		await expect.element(fixture.screen.getByText("Destination history unavailable")).toBeVisible();
		await expect.element(restore).not.toBeInTheDocument();
		await expect
			.element(fixture.screen.getByRole("button", { name: "Restore as draft" }))
			.not.toBeInTheDocument();
		expect(fixture.client.restore).not.toHaveBeenCalled();
	} finally {
		await fixture.destroy();
	}
});

it("clears stale history when a fallback refresh fails", async () => {
	const nextHistory = Promise.withResolvers<AdminVersion[]>();
	const versions = vi.fn().mockResolvedValueOnce([version]).mockReturnValue(nextHistory.promise);
	const fixture = await versionRouteFixture("/collections/posts/one/versions", {
		versions,
	});
	try {
		const revisionLink = fixture.screen.getByRole("link", { name: /September/ });
		await expect.element(revisionLink).toBeVisible();

		fixture.runtime.manifestRevision += 1;
		await expect.poll(() => versions).toHaveBeenCalledTimes(2);
		nextHistory.reject(new Error("Refreshed history unavailable"));
		await expect.element(fixture.screen.getByText("Refreshed history unavailable")).toBeVisible();
		await expect.element(revisionLink).not.toBeInTheDocument();
	} finally {
		await fixture.destroy();
	}
});

it.each(["not-a-revision", "0", "-1", "1.5"])(
	"treats malformed revision %s as history without requesting exact detail",
	async (revision) => {
		const fixture = await versionRouteFixture(`/collections/posts/one/versions/${revision}`, {});
		try {
			await expect
				.element(fixture.screen.getByRole("table", { name: "Version history" }))
				.toBeVisible();
			expect(fixture.client.version).not.toHaveBeenCalled();
		} finally {
			await fixture.destroy();
		}
	}
);

it("renders an unavailable state when an exact version no longer exists", async () => {
	const fixture = await versionRouteFixture("/collections/posts/one/versions/99", {
		version: vi.fn().mockRejectedValue(
			new RiduError({
				code: "not_found",
				status: 404,
				message: "Version not found",
				issues: [],
			})
		),
	});
	try {
		await expect.element(fixture.screen.getByText("This version is unavailable.")).toBeVisible();
		await expect.element(fixture.screen.getByText("Version not found")).not.toBeInTheDocument();
	} finally {
		await fixture.destroy();
	}
});

it("updates comparison fields and reference labels after their asynchronous reads", async () => {
	const reference = Promise.withResolvers<AdminDocument>();
	const before = {
		...version,
		Revision: 1,
		Snapshot: { id: "one", title: "Before", author: "author" },
	};
	const current = {
		...version,
		Revision: 2,
		Snapshot: { id: "one", title: "After", author: "author" },
	};
	const collection = {
		...versionedCollection,
		admin: { useAsTitle: "title" },
		fields: [
			{ name: "title", path: "title", type: "text", admin: { label: "Title" } },
			{
				name: "author",
				path: "author",
				type: "relationship",
				admin: { label: "Author" },
				relationship: { collectionSlug: "posts" },
			},
		],
	} as SchemaCollection;
	const fixture = await versionRouteFixture(
		"/collections/posts/one/versions/2?modifiedOnly=false",
		{
			versions: vi.fn(async () => [current, before]) as AdminClient["versions"],
			version: vi.fn(async (_slug, _id, revision) =>
				revision === current.Revision ? current : before
			) as AdminClient["version"],
			find: vi.fn(async (_slug: string, id: string) =>
				id === "author" ? reference.promise : { id, title: "Current", _revision: 2 }
			) as AdminClient["find"],
		},
		collection
	);
	try {
		await expect.element(fixture.screen.getByText("Before", { exact: true })).toBeVisible();
		reference.resolve({ id: "author", title: "Resolved author" });
		await expect
			.element(fixture.screen.getByText("Resolved author", { exact: true }).first())
			.toBeVisible();
		await fixture.router.navigate("/collections/posts/one/versions/2?compare=2&modifiedOnly=false");
		await expect.element(fixture.screen.getByText("Before", { exact: true })).toBeVisible();
		await expect
			.element(fixture.screen.getByText("No modified fields in this comparison."))
			.not.toBeInTheDocument();
	} finally {
		await fixture.destroy();
	}
});

it("keeps the prepared comparison visible while its exact localized snapshot loads", async () => {
	const exact = Promise.withResolvers<AdminVersion>();
	const before = {
		...version,
		Revision: 1,
		Snapshot: { id: "one", title: "Prepared" },
	};
	const selected = {
		...version,
		Revision: 2,
		Snapshot: { id: "one", title: "Selected" },
	};
	const client = versionClient(Promise.resolve({ id: "one", _revision: 3 }));
	client.version = vi.fn(() => exact.promise) as AdminClient["version"];
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: {
			name: "Localized versions",
			localization: {
				defaultLocale: "en",
				fallback: true,
				locales: [
					{ code: "en", label: "English" },
					{ code: "fr", label: "French" },
				],
			},
		},
		collections: [versionedCollection],
		plugins: [],
	};
	const notifications = new NotificationCenter();
	const controller = new VersionHistoryController(runtime, notifications);
	const access = await client.collectionAccess("posts", { id: "one" });
	try {
		controller.sync({ slug: "posts", id: "one", global: false, revision: 2 }, 1, {
			history: { value: [selected, before] },
			detail: { value: selected },
			document: { document: { value: { id: "one", _revision: 2 } }, access: { value: access } },
		});
		const loading = controller.syncComparison(before);
		expect(controller.comparisonState).toEqual({ revision: 1, status: "loading" });
		expect(client.version).toHaveBeenCalledWith(
			"posts",
			"one",
			1,
			expect.objectContaining({ locale: "all", signal: expect.any(AbortSignal) })
		);

		const localized = {
			...before,
			Snapshot: { id: "one", title: { en: "Prepared", fr: "Préparé" } },
		};
		exact.resolve(localized);
		await loading;
		expect(controller.comparisonState).toEqual({
			revision: 1,
			status: "ready",
			version: localized,
		});
	} finally {
		controller.dispose();
		runtime.dispose();
		notifications.destroy();
	}
});

it("renders a valid selected-locale diff before the all-locale comparison arrives", async () => {
	const comparison = Promise.withResolvers<AdminVersion>();
	const before = {
		...version,
		Revision: 1,
		Snapshot: { id: "one", title: "Before" },
	};
	const currentSummary = {
		...version,
		Revision: 2,
		Snapshot: { id: "one", title: "After" },
	};
	const current = {
		...currentSummary,
		Snapshot: { id: "one", title: { en: "After", fr: "Après" } },
	};
	const collection = {
		...versionedCollection,
		fields: [
			{
				name: "title",
				path: "title",
				type: "text",
				localized: true,
				admin: { label: "Title" },
			},
		],
	} as SchemaCollection;
	const fixture = await versionRouteFixture(
		"/collections/posts/one/versions/2?modifiedOnly=false",
		{
			versions: vi.fn(async () => [currentSummary, before]) as AdminClient["versions"],
			version: vi.fn((_slug, _id, revision) =>
				revision === current.Revision ? Promise.resolve(current) : comparison.promise
			) as AdminClient["version"],
		},
		collection,
		true
	);
	try {
		await expect.element(fixture.screen.getByText("Before", { exact: true })).toBeVisible();
		await expect.element(fixture.screen.getByText("After", { exact: true })).toBeVisible();
		await expect
			.element(fixture.screen.getByText("Avant", { exact: true }))
			.not.toBeInTheDocument();

		comparison.resolve({
			...before,
			Snapshot: { id: "one", title: { en: "Before", fr: "Avant" } },
		});
		await expect.element(fixture.screen.getByText("Avant", { exact: true })).toBeVisible();
		await expect.element(fixture.screen.getByText("Après", { exact: true })).toBeVisible();
	} finally {
		await fixture.destroy();
	}
});

it.each([undefined, 0, -1, Number.NaN])(
	"does not offer an unguarded restore for current revision %s",
	async (revision) => {
		const restore = vi.fn(async () => ({ id: "one", _revision: 3 }));
		const fixture = await versionRouteFixture("/collections/posts/one/versions/1", {
			find: vi.fn(async () => ({ id: "one", _revision: revision })) as AdminClient["find"],
			restore,
		});
		try {
			await expect
				.element(fixture.screen.getByRole("heading", { name: "Compare Versions" }))
				.toBeVisible();
			await expect
				.element(fixture.screen.getByRole("button", { name: "Restore this version" }))
				.not.toBeInTheDocument();
			expect(restore).not.toHaveBeenCalled();
		} finally {
			await fixture.destroy();
		}
	}
);

it("falls back to an exact detail read when prepared history omits it", async () => {
	const client = versionClient(Promise.resolve({ id: "one", _revision: 3 }));
	const runtime = new AdminRuntime(client);
	const notifications = new NotificationCenter();
	const controller = new VersionHistoryController(runtime, notifications);
	const access = await client.collectionAccess("posts", { id: "one" });
	try {
		controller.sync({ slug: "posts", id: "one", global: false, revision: 1 }, 1, {
			history: { value: [version] },
			document: { document: { value: { id: "one", _revision: 2 } }, access: { value: access } },
		});
		await expect
			.poll(() => client.version)
			.toHaveBeenCalledWith("posts", "one", 1, expect.anything());
		await expect.poll(() => controller.selectedVersion?.Revision).toBe(1);
	} finally {
		controller.dispose();
		runtime.dispose();
		notifications.destroy();
	}
});

it("adopts prepared history when its exact version is no longer available", async () => {
	const client = versionClient(Promise.resolve({ id: "one", _revision: 3 }));
	const runtime = new AdminRuntime(client);
	const notifications = new NotificationCenter();
	const controller = new VersionHistoryController(runtime, notifications);
	const access = await client.collectionAccess("posts", { id: "one" });
	try {
		controller.sync({ slug: "posts", id: "one", global: false, revision: 99 }, 1, {
			history: { value: [version] },
			detail: {
				error: {
					code: "not_found",
					status: 404,
					message: "Version not found",
					issues: [],
				},
			},
			document: { document: { value: { id: "one", _revision: 2 } }, access: { value: access } },
		});
		expect(controller.loading).toBe(false);
		expect(controller.error).toBeUndefined();
		expect(controller.selectedVersion).toBeUndefined();
		expect(controller.versions).toEqual([version]);
		expect(controller.document?.id).toBe("one");
		expect(client.version).not.toHaveBeenCalled();
	} finally {
		controller.dispose();
		runtime.dispose();
		notifications.destroy();
	}
});

it("authorizes published and draft restores by their actual operations", async () => {
	const client = versionClient(Promise.resolve({ id: "one", _revision: 3 }));
	const access = await client.collectionAccess("posts", { id: "one" });
	access.operations.update = false;
	access.operations.publish = true;
	access.operations.unpublish = false;
	const runtime = new AdminRuntime(client);
	const notifications = new NotificationCenter();
	const controller = new VersionHistoryController(runtime, notifications);
	try {
		controller.sync({ slug: "posts", id: "one", global: false, revision: 1 }, 1, {
			history: { value: [version] },
			detail: { value: version },
			document: { document: { value: { id: "one", _revision: 2 } }, access: { value: access } },
		});
		expect(controller.canRestore(false)).toBe(true);
		expect(controller.canRestore(true)).toBe(false);
		await controller.restore(true, () => {});
		expect(client.restore).not.toHaveBeenCalled();
		await controller.restore(false, () => {});
		expect(client.restore).toHaveBeenCalledWith(
			"posts",
			"one",
			1,
			expect.objectContaining({ revision: 2, draft: false })
		);
	} finally {
		controller.dispose();
		runtime.dispose();
		notifications.destroy();
	}
});

function previewToken(token: string, documentId: string): PreviewToken {
	return {
		token,
		resource: "collection",
		slug: "posts",
		documentId,
		expiresAt: "2030-01-01T00:00:00Z",
	};
}

const versionedCollection: SchemaCollection = {
	id: "posts",
	slug: "posts",
	labels: { singular: "Post", plural: "Posts" },
	admin: {},
	capabilities: { auth: false, upload: false, versions: true, trash: false, locking: false },
	versionSettings: { drafts: true, maxPerDocument: 10, autosaveIntervalSeconds: 0 },
	fields: [],
};

const version: AdminVersion = {
	ID: "version-one",
	DocumentID: "one",
	Revision: 1,
	Status: "published",
	Snapshot: { id: "one", title: "One" },
	CreatedAt: "2026-09-13T00:00:00Z",
};

function versionClient(restore: Promise<AdminDocument>) {
	const operations = {
		admin: true,
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
	return {
		versions: vi.fn(async () => [version]),
		version: vi.fn(async () => version),
		find: vi.fn(async (_slug: string, id: string) => ({ id, title: id, _revision: 2 })),
		collectionAccess: vi.fn(async () => ({ operations, fields: {} })),
		scheduledPublications: vi.fn(async () => []),
		restore: vi.fn(() => restore),
	} as unknown as AdminClient & { find: ReturnType<typeof vi.fn> };
}

async function versionRouteFixture(
	path: string,
	overrides: Partial<AdminClient>,
	collection = versionedCollection,
	localized = false
) {
	const restore = new Promise<AdminDocument>(() => undefined);
	const client = { ...versionClient(restore), ...overrides } as AdminClient & {
		find: ReturnType<typeof vi.fn>;
	};
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: {
			name: "Version ownership",
			...(localized
				? {
						localization: {
							defaultLocale: "en",
							fallback: true,
							locales: [
								{ code: "en", label: "English" },
								{ code: "fr", label: "French" },
							],
						},
					}
				: {}),
		},
		collections: [collection],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	const notifications = new NotificationCenter();
	const success = vi.spyOn(notifications, "success").mockReturnValue(0);
	const error = vi.spyOn(notifications, "error").mockReturnValue(0);
	const router = createMemoryRouter(
		[
			{
				path: "/collections/:collection/:document/versions/:revision?",
				Component: VersionHistoryRoute,
			},
		],
		{ initialEntries: [path] }
	);
	const screen = await render(RuntimeProvider, { router, runtime, notifications });
	await expect.poll(() => client.find).toHaveBeenCalled();
	return {
		screen,
		router,
		runtime,
		client,
		success,
		error,
		async destroy() {
			await screen.unmount();
			router.dispose();
			runtime.dispose();
		},
	};
}
