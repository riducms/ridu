import { createMemoryRouter } from "@hvniel/svelte-router";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { SCHEMA_MANIFEST_VERSION, type SchemaCollection } from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import DocumentRoute from "@admin/features/documents/document-route.svelte";

const Provider = svelte`
	<script>
		import { RouterProvider } from "@hvniel/svelte-router";
		import { setAdminI18n } from "@riducms/plugin";
		import { TooltipProvider } from "@riducms/ui";
		import { setNotificationCenter } from "../../src/core/notifications/notification-center.svelte";
		import { createAdminScroll } from "../../src/core/routing/admin-scroll.svelte";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { AdminBootstrapCoordinator, setAdminBootstrapCoordinator } from "../../src/core/bootstrap/admin-bootstrap";
		let { router, runtime, notifications } = $props();
		const bootstrap = new AdminBootstrapCoordinator();
		bootstrap.configure(runtime);
		setAdminBootstrapCoordinator(bootstrap);
		setAdminRuntime(runtime);
		setNotificationCenter(notifications);
		setAdminI18n(runtime.i18n);
		createAdminScroll();
	</script>
	<TooltipProvider><RouterProvider {router} /></TooltipProvider>
`;

for (const outcome of ["success", "failure"] as const) {
	it(`ignores stale force-unlock ${outcome} after the retained document route changes`, async () => {
		const forceUnlockResult = Promise.withResolvers<{ success: true }>();
		let settled = false;
		void forceUnlockResult.promise.then(
			() => {
				settled = true;
			},
			() => {
				settled = true;
			}
		);
		const forceUnlock = vi.fn(
			(_input: { collection: string; id: string }, _options?: { signal?: AbortSignal }) =>
				forceUnlockResult.promise
		);
		const fixture = await documentRouteFixture({
			auth: { forceUnlock },
		} as unknown as Partial<AdminClient>);
		try {
			await fixture.openAction("Force unlock");
			await fixture.router.navigate("/collections/users/two?locale=en");
			await expect
				.poll(() => fixture.client.find)
				.toHaveBeenCalledWith(
					"users",
					"two",
					expect.objectContaining({ signal: expect.any(AbortSignal) })
				);
			const signal = forceUnlock.mock.calls[0]?.[1]?.signal;
			expect(signal?.aborted).toBe(true);

			if (outcome === "success") forceUnlockResult.resolve({ success: true });
			else forceUnlockResult.reject(new Error("Old unlock failed"));
			await expect.poll(() => settled).toBe(true);
			expect(fixture.success).not.toHaveBeenCalled();
			expect(fixture.error).not.toHaveBeenCalled();
			await fixture.openMoreActions();
			await expect
				.element(fixture.screen.getByRole("button", { name: "Force unlock" }))
				.toBeEnabled();
		} finally {
			await fixture.destroy();
		}
	});
}

it("does not refresh the new route when an old locale copy completes", async () => {
	const copyResult = Promise.withResolvers<AdminDocument>();
	let settled = false;
	void copyResult.promise.then(() => {
		settled = true;
	});
	const copyLocale = vi.fn(
		(
			_collection: string,
			_id: string,
			_input: { from: string; to: string },
			_options?: { signal?: AbortSignal }
		) => copyResult.promise
	);
	const fixture = await documentRouteFixture({ copyLocale });
	try {
		await fixture.openMoreActions();
		const trigger = fixture.screen.getByRole("button", { name: "Copy localized values" });
		await expect.element(trigger).toBeVisible();
		await trigger.click();
		await fixture.screen.getByRole("option", { name: /French/ }).click();
		await expect.poll(() => copyLocale).toHaveBeenCalledOnce();

		await fixture.router.navigate("/collections/users/two?locale=en");
		await expect
			.poll(() => fixture.client.find)
			.toHaveBeenCalledWith(
				"users",
				"two",
				expect.objectContaining({ signal: expect.any(AbortSignal) })
			);
		const signal = copyLocale.mock.calls[0]?.[3]?.signal;
		expect(signal?.aborted).toBe(true);
		const readsBeforeCompletion = fixture.client.find.mock.calls.length;

		copyResult.resolve(document("one", "Copied"));
		await expect.poll(() => settled).toBe(true);
		expect(fixture.client.find).toHaveBeenCalledTimes(readsBeforeCompletion);
		expect(fixture.runtime.documentRevision).toBe(0);
		expect(fixture.success).not.toHaveBeenCalled();
		expect(fixture.error).not.toHaveBeenCalled();
	} finally {
		await fixture.destroy();
	}
});

it("keeps an account unlock active when only the document view changes", async () => {
	const result = Promise.withResolvers<{ success: true }>();
	const forceUnlock = vi.fn(
		(_input: { collection: string; id: string }, _options?: { signal?: AbortSignal }) =>
			result.promise
	);
	const fixture = await documentRouteFixture({
		auth: { forceUnlock },
	} as unknown as Partial<AdminClient>);
	try {
		await fixture.openAction("Force unlock");
		await fixture.router.navigate("/collections/users/one/api?locale=en");
		expect(forceUnlock.mock.calls[0]?.[1]?.signal?.aborted).toBe(false);
		result.resolve({ success: true });
		await expect.poll(() => fixture.success).toHaveBeenCalledOnce();
		expect(fixture.error).not.toHaveBeenCalled();
	} finally {
		await fixture.destroy();
	}
});

it("keeps locale-copy choices live across repeated locale navigation", async () => {
	const copyLocale = vi.fn(async () => document("one", "Copied"));
	const fixture = await documentRouteFixture({ copyLocale });
	try {
		for (const locale of ["fr", "en"]) {
			await fixture.router.navigate(`/collections/users/one?locale=${locale}`);
			await expect
				.poll(() => fixture.client.find)
				.toHaveBeenLastCalledWith("users", "one", expect.objectContaining({ locale }));
		}
		await fixture.openMoreActions();
		const trigger = fixture.screen.getByRole("button", { name: "Copy localized values" });
		await expect.element(trigger).toBeEnabled();
		await trigger.click();
		await fixture.screen.getByRole("option", { name: /French/ }).click();
		await expect
			.poll(() => copyLocale)
			.toHaveBeenCalledWith(
				"users",
				"one",
				{ from: "fr", to: "en" },
				expect.objectContaining({ signal: expect.any(AbortSignal) })
			);
		await expect.poll(() => fixture.success).toHaveBeenCalledOnce();
	} finally {
		await fixture.destroy();
	}
});

for (const change of ["schema", "unmount"] as const) {
	it(`cancels an account unlock on ${change} without stale notifications`, async () => {
		const result = Promise.withResolvers<{ success: true }>();
		const forceUnlock = vi.fn(
			(_input: { collection: string; id: string }, _options?: { signal?: AbortSignal }) =>
				result.promise
		);
		const fixture = await documentRouteFixture({
			auth: { forceUnlock },
		} as unknown as Partial<AdminClient>);
		let destroyed = false;
		try {
			await fixture.openAction("Force unlock");
			if (change === "schema") fixture.runtime.manifestRevision += 1;
			else {
				await fixture.destroy();
				destroyed = true;
			}
			await expect.poll(() => forceUnlock.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
			result.resolve({ success: true });
			await result.promise;
			expect(fixture.success).not.toHaveBeenCalled();
			expect(fixture.error).not.toHaveBeenCalled();
		} finally {
			if (!destroyed) await fixture.destroy();
		}
	});
}

async function documentRouteFixture(
	overrides: Partial<AdminClient>,
	collection = usersCollection,
	initialEntry = "/collections/users/one?locale=en"
) {
	const client = {
		find: vi.fn(async (_slug: string, id: string) => document(id)),
		collectionAccess: vi.fn(async () => access),
		...overrides,
	} as unknown as AdminClient & { find: ReturnType<typeof vi.fn> };
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: {
			name: "Route operation ownership",
			localization: {
				defaultLocale: "en",
				fallback: true,
				locales: [
					{ code: "en", label: "English" },
					{ code: "fr", label: "French" },
				],
			},
		},
		collections: [collection],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	runtime.contentLocale = "en";
	const notifications = new NotificationCenter();
	const success = vi.spyOn(notifications, "success").mockReturnValue(0);
	const error = vi.spyOn(notifications, "error").mockReturnValue(0);
	const router = createMemoryRouter(
		[
			{ path: "/collections/:collection/create/api", Component: DocumentRoute },
			{ path: "/collections/:collection/create", Component: DocumentRoute },
			{ path: "/collections/:collection/:document/:view?", Component: DocumentRoute },
		],
		{ initialEntries: [initialEntry] }
	);
	const screen = await render(Provider, { router, runtime, notifications });
	await expect
		.poll(() => client.find)
		.toHaveBeenCalledWith(
			"users",
			"one",
			expect.objectContaining({ signal: expect.any(AbortSignal) })
		);

	return {
		screen,
		router,
		runtime,
		client,
		success,
		error,
		async openMoreActions() {
			const button = screen.getByRole("button", { name: "More actions" });
			await expect.element(button).toBeVisible();
			await button.click();
		},
		async openAction(name: string) {
			await this.openMoreActions();
			await screen.getByRole("button", { name }).click();
		},
		async destroy() {
			await screen.unmount();
			router.dispose();
			runtime.dispose();
		},
	};
}

const usersCollection: SchemaCollection = {
	id: "users",
	slug: "users",
	labels: { singular: "User", plural: "Users" },
	admin: { useAsTitle: "email" },
	capabilities: { auth: true, upload: false, versions: false, trash: false, locking: false },
	authSettings: {
		identityField: "email",
		sessionDurationSeconds: 3_600,
		passwordMinLength: 8,
		passwordMaxBytes: 72,
		passwordBcryptCost: 12,
		maxLoginAttempts: 5,
		lockDurationSeconds: 600,
		passwordReset: true,
		passwordResetTokenDurationSeconds: 3_600,
		verifyEmail: false,
		verificationTokenDurationSeconds: 3_600,
		apiKeys: false,
	},
	fields: [
		{
			id: "email",
			name: "email",
			path: "email",
			type: "text",
			category: "scalar",
			required: true,
			unique: true,
			admin: { label: "Email" },
			text: {},
		},
		{
			id: "title",
			name: "title",
			path: "title",
			type: "text",
			category: "scalar",
			required: false,
			unique: false,
			localized: true,
			admin: { label: "Title" },
			text: {},
		},
	],
};

const access = {
	operations: {
		admin: true,
		create: true,
		read: true,
		readVersions: false,
		update: true,
		delete: false,
		duplicate: false,
		publish: false,
		unpublish: false,
		restoreDeleted: false,
		deletePermanent: false,
		selectAll: true,
	},
	fields: {
		email: { read: true, create: true, update: true },
		title: { read: true, create: true, update: true },
	},
};

function document(id: string, title = id): AdminDocument {
	return {
		id,
		email: `${id}@example.test`,
		title,
		_revision: 2,
		createdAt: "2026-09-13T00:00:00Z",
		updatedAt: "2026-09-13T00:00:00Z",
	};
}

it("keeps an Edit link on create/API and does not show an empty More menu on create", async () => {
	const fixture = await documentRouteFixture({});
	try {
		await fixture.router.navigate("/collections/users/create/api?locale=en");
		await expect
			.element(fixture.screen.getByRole("link", { name: "Edit", exact: true }))
			.toBeVisible();
		await fixture.screen.getByRole("link", { name: "Edit", exact: true }).click();
		await expect
			.element(fixture.screen.getByRole("button", { name: "More actions" }))
			.not.toBeInTheDocument();
	} finally {
		await fixture.destroy();
	}
});

it("keeps Save draft available to an update-only author", async () => {
	const collection: SchemaCollection = {
		...usersCollection,
		capabilities: { ...usersCollection.capabilities, versions: true },
		versionSettings: { drafts: true, maxPerDocument: 10, autosaveIntervalSeconds: 0 },
	};
	const fixture = await documentRouteFixture(
		{
			find: vi.fn(async (_slug: string, id: string) => ({
				...document(id),
				_status: "draft" as const,
			})) as unknown as AdminClient["find"],
			versions: vi.fn(async () => []),
		},
		collection
	);
	try {
		await fixture.screen.getByRole("textbox", { name: "Email" }).fill("updated@example.test");
		await expect
			.element(fixture.screen.getByRole("button", { name: "Save draft", exact: true }))
			.toBeEnabled();
		await expect
			.element(fixture.screen.getByRole("button", { name: "Publish changes", exact: true }))
			.not.toBeInTheDocument();
	} finally {
		await fixture.destroy();
	}
});

it("closes the schedule drawer when the document changes", async () => {
	const scheduledPublications = vi.fn(async () => []);
	const fixture = await documentRouteFixture(
		{
			scheduledPublications,
			collectionAccess: vi.fn(async () => ({
				...access,
				operations: { ...access.operations, publish: true },
			})),
		},
		{ ...usersCollection, capabilities: { ...usersCollection.capabilities, versions: true } }
	);
	try {
		await fixture.screen.getByRole("button", { name: "Schedule", exact: true }).click();
		await fixture.screen.getByRole("button", { name: "Schedule publication", exact: true }).click();
		await expect.poll(() => scheduledPublications).toHaveBeenCalledOnce();
		await fixture.router.navigate("/collections/users/two?locale=en");
		await expect.element(fixture.screen.getByRole("dialog")).not.toBeInTheDocument();
		expect(scheduledPublications).toHaveBeenCalledTimes(1);
	} finally {
		await fixture.destroy();
	}
});

it("does not offer scheduling when a draft can only be unpublished", async () => {
	const fixture = await documentRouteFixture(
		{
			find: vi.fn(async (_slug: string, id: string) => ({
				...document(id),
				_status: "draft" as const,
			})) as unknown as AdminClient["find"],
			collectionAccess: vi.fn(async () => ({
				...access,
				operations: { ...access.operations, publish: false, unpublish: true },
			})),
		},
		{ ...usersCollection, capabilities: { ...usersCollection.capabilities, versions: true } }
	);
	try {
		await expect
			.element(fixture.screen.getByRole("button", { name: "Schedule", exact: true }))
			.not.toBeInTheDocument();
	} finally {
		await fixture.destroy();
	}
});

it("keeps the API inspector usable when the document read fails", async () => {
	const fetch = vi.spyOn(window, "fetch").mockResolvedValue(
		new Response(
			JSON.stringify({
				error: {
					code: "access_denied",
					status: 403,
					message: "API read denied",
					issues: [],
				},
			}),
			{ status: 403 }
		)
	);
	const fixture = await documentRouteFixture({
		find: vi.fn().mockRejectedValue(new Error("Document read denied")),
	});
	try {
		await fixture.router.navigate("/collections/users/one/api?locale=en");
		await expect
			.element(fixture.screen.getByRole("region", { name: "API", exact: true }))
			.toBeVisible();
		await expect.element(fixture.screen.getByRole("spinbutton", { name: "Depth" })).toBeVisible();
		await expect
			.element(fixture.screen.getByText('"API read denied"', { exact: true }))
			.toBeVisible();
		await expect.element(fixture.screen.getByRole("alert")).toHaveTextContent("API read denied");
		await expect
			.element(fixture.screen.getByText("Document read denied", { exact: true }))
			.not.toBeInTheDocument();
		await expect
			.element(fixture.screen.getByRole("checkbox", { name: "Authenticated" }))
			.toBeEnabled();
	} finally {
		await fixture.destroy();
		fetch.mockRestore();
	}
});

it("mounts the API inspector without waiting for the editor read", async () => {
	const editorRead = Promise.withResolvers<AdminDocument>();
	const find = vi.fn(() => editorRead.promise) as unknown as AdminClient["find"];
	const fetch = vi
		.spyOn(window, "fetch")
		.mockResolvedValue(new Response(JSON.stringify({ doc: document("one", "API response") })));
	const fixture = await documentRouteFixture(
		{ find },
		usersCollection,
		"/collections/users/one/api?locale=en"
	);
	try {
		await expect
			.element(fixture.screen.getByRole("region", { name: "API", exact: true }))
			.toBeVisible();
		await expect.element(fixture.screen.getByRole("spinbutton", { name: "Depth" })).toBeVisible();
		await expect.poll(() => fetch).toHaveBeenCalledOnce();
	} finally {
		editorRead.resolve(document("one"));
		await fixture.destroy();
		fetch.mockRestore();
	}
});

it("hides an open live preview while the retained route shows the API inspector", async () => {
	const fetch = vi
		.spyOn(window, "fetch")
		.mockResolvedValue(new Response(JSON.stringify({ doc: document("one", "API response") })));
	const fixture = await documentRouteFixture(
		{
			createPreviewToken: vi.fn(async () => ({
				token: "preview-token",
				resource: "collection" as const,
				slug: "users",
				documentId: "one",
				expiresAt: "2099-01-01T00:00:00Z",
			})),
			revokePreviewToken: vi.fn(async () => ({ success: true })),
		},
		{
			...usersCollection,
			admin: { ...usersCollection.admin, livePreview: { url: "https://preview.example.test" } },
		}
	);
	try {
		await fixture.screen.getByRole("button", { name: "Live preview", exact: true }).click();
		await expect
			.element(fixture.screen.getByRole("region", { name: "Live preview", exact: true }))
			.toBeVisible();
		expect(globalThis.document.querySelector(".ridu-document-body")?.classList).toContain(
			"ridu-document-body--preview"
		);

		await fixture.router.navigate("/collections/users/one/api?locale=en");
		await expect
			.element(fixture.screen.getByRole("region", { name: "API", exact: true }))
			.toBeVisible();
		await expect
			.element(fixture.screen.getByRole("region", { name: "Live preview", exact: true }))
			.not.toBeInTheDocument();
		expect(globalThis.document.querySelector(".ridu-document-body")?.classList).not.toContain(
			"ridu-document-body--preview"
		);
	} finally {
		await fixture.destroy();
		fetch.mockRestore();
	}
});
