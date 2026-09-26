import { createMemoryRouter } from "@hvniel/svelte-router";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import {
	SCHEMA_MANIFEST_VERSION,
	type APIKey,
	type AuthSession,
	type SchemaCollection,
} from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { AdminBootstrapCoordinator } from "@admin/core/bootstrap/admin-bootstrap";
import AccountRoute from "@admin/features/account/account-route.svelte";
import AccountSecurityRoute from "@admin/features/account/account-security-route.svelte";
import { AccountSecurityController } from "@admin/features/account/account-security-controller.svelte";

const translate = ((key: string) => key) as never;

it("adopts security data synchronously but reloads through the client", async () => {
	const sessions = vi.fn(async () => []);
	const apiKeys = vi.fn(async () => []);
	const controller = new AccountSecurityController(
		{ sessions, apiKeys } as unknown as AdminClient,
		true,
		translate
	);
	await controller.load({
		sessions: { value: [] },
		apiKeys: { value: [{ id: "key-one", name: "CLI", createdAt: "2026-09-15T00:00:00Z" }] },
	});
	expect(controller.status).toBe("ready");
	expect(controller.apiKeys[0]?.name).toBe("CLI");
	expect(sessions).not.toHaveBeenCalled();
	expect(apiKeys).not.toHaveBeenCalled();
	await controller.load();
	expect(controller.apiKeys).toEqual([]);
	expect(sessions).toHaveBeenCalledOnce();
	expect(apiKeys).toHaveBeenCalledOnce();
	controller.destroy();
});

it("a new security seed supersedes a pending client load and can recover from a prepared error", async () => {
	const result = Promise.withResolvers<[]>();
	const sessions = vi.fn(() => result.promise);
	const controller = new AccountSecurityController(
		{ sessions } as unknown as AdminClient,
		false,
		translate
	);
	const pending = controller.load();
	await controller.load({
		sessions: { error: { code: "access_denied", status: 403, message: "Denied", issues: [] } },
		apiKeys: { value: [] },
	});
	result.resolve([]);
	await pending;
	expect(controller.status).toBe("error");
	expect(controller.error).toBe("Denied");
	await controller.load();
	expect(controller.status).toBe("ready");
	expect(controller.error).toBeUndefined();
	expect(sessions).toHaveBeenCalledTimes(2);
	controller.destroy();
});

const RuntimeProvider = svelte`
	<script>
		import { RouterProvider } from "@hvniel/svelte-router";
		import { setAdminI18n } from "@riducms/plugin";
		import { setNotificationCenter } from "../../src/core/notifications/notification-center.svelte";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { AdminBootstrapCoordinator, setAdminBootstrapCoordinator } from "../../src/core/bootstrap/admin-bootstrap";
		let { router, runtime, notifications, bootstrap = new AdminBootstrapCoordinator() } = $props();
		bootstrap.configure(runtime);
		setAdminBootstrapCoordinator(bootstrap);
		setAdminRuntime(runtime);
		setNotificationCenter(notifications);
		setAdminI18n(runtime.i18n);
	</script>
	<RouterProvider {router} />
`;

it.each(["account", "security"] as const)(
	"renders the unreplaced %s route from explicit data without initial reads",
	async (kind) => {
		const client = {
			find: vi.fn(),
			collectionAccess: vi.fn(),
			sessions: vi.fn(),
			apiKeys: vi.fn(),
		} as unknown as AdminClient;
		const runtime = new AdminRuntime(client);
		const user = { id: "user-a", name: "Prepared profile" };
		runtime.manifest = {
			version: SCHEMA_MANIFEST_VERSION,
			application: {
				name: "Prepared account",
				admin: { userCollectionId: users.id, userCollectionSlug: users.slug },
			},
			collections: [users],
			plugins: [],
		};
		runtime.manifestRevision = 1;
		runtime.session = session("session-a", user);
		const path = kind === "account" ? "/account" : "/account/security";
		const bootstrap = new AdminBootstrapCoordinator();
		bootstrap.configure(runtime);
		bootstrap.stage(
			{
				version: 1,
				outcome: "prepared",
				pathname: `/admin${path}`,
				search: "",
				contextKey: "0123456789abcdef0123456789abcdef",
				fingerprint: "prepared-account",
				buildId: "test",
				moduleGroups: ["entry"],
				runtime: {
					manifest: runtime.manifest,
					session: runtime.session,
					authBootstrapAvailable: false,
					collectionOperations: {},
					globalOperations: {},
					theme: "light",
					adminLanguage: "en",
					adminTimeZone: "UTC",
					preferences: {},
				},
				route:
					kind === "account"
						? { kind, document: { document: { value: user }, access: { value: access } } }
						: { kind, security: { sessions: { value: [] }, apiKeys: { value: [] } } },
			},
			`/admin${path}`
		);
		const notifications = new NotificationCenter();
		const router = createMemoryRouter(
			[
				{
					path: path.slice(1),
					Component: kind === "account" ? AccountRoute : AccountSecurityRoute,
				},
			],
			{ initialEntries: [path] }
		);
		const screen = await render(RuntimeProvider, { router, runtime, notifications, bootstrap });
		try {
			if (kind === "account")
				await expect
					.element(screen.getByRole("textbox", { name: "Name", exact: true }))
					.toHaveValue("Prepared profile");
			else
				await expect
					.element(screen.getByRole("heading", { name: "Security", exact: true }))
					.toBeVisible();
			expect(client.find).not.toHaveBeenCalled();
			expect(client.collectionAccess).not.toHaveBeenCalled();
			expect(client.sessions).not.toHaveBeenCalled();
			expect(client.apiKeys).not.toHaveBeenCalled();
			expect(
				document.querySelector('[data-ridu-loading-surface],[data-slot="skeleton"]')
			).toBeNull();
		} finally {
			await screen.unmount();
			router.dispose();
			runtime.dispose();
			notifications.destroy();
		}
	}
);

it("preserves pending and completed security mutations through same-owner query navigation", async () => {
	const created = Promise.withResolvers<APIKey>();
	const client = {
		sessions: vi.fn(async () => []),
		apiKeys: vi.fn(async () => []),
		createAPIKey: vi.fn(() => created.promise),
	} as unknown as AdminClient;
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: {
			name: "Security lifetime",
			admin: { userCollectionId: users.id, userCollectionSlug: users.slug },
		},
		collections: [
			{
				...users,
				authSettings: {
					identityField: "email",
					sessionDurationSeconds: 3600,
					passwordMinLength: 8,
					passwordMaxBytes: 72,
					passwordBcryptCost: 10,
					maxLoginAttempts: 5,
					lockDurationSeconds: 60,
					passwordReset: false,
					passwordResetTokenDurationSeconds: 3600,
					verifyEmail: false,
					verificationTokenDurationSeconds: 3600,
					apiKeys: true,
				},
			},
		],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	runtime.session = session("session-a", { id: "user-a" });
	const notifications = new NotificationCenter();
	const router = createMemoryRouter(
		[{ path: "account/security", Component: AccountSecurityRoute }],
		{ initialEntries: ["/account/security?x=1&y=2"] }
	);
	const screen = await render(RuntimeProvider, { router, runtime, notifications });
	try {
		await screen.getByRole("textbox", { name: "Name", exact: true }).fill("Automation");
		await screen.getByRole("button", { name: "Create key", exact: true }).click();
		await expect.poll(() => client.createAPIKey).toHaveBeenCalledOnce();
		await router.navigate("/account/security?y=2&x=1");
		await router.navigate("/account/security?x=3");
		created.resolve({
			id: "key-one",
			name: "Automation",
			key: "only-shown-once",
			createdAt: "2026-09-15T00:00:00Z",
		});
		await expect.element(screen.getByText("only-shown-once")).toBeVisible();
		await router.navigate("/account/security?x=4");
		await expect.element(screen.getByText("only-shown-once")).toBeVisible();
		expect(client.sessions).toHaveBeenCalledOnce();
		expect(client.apiKeys).toHaveBeenCalledOnce();
	} finally {
		await screen.unmount();
		router.dispose();
		runtime.dispose();
		notifications.destroy();
	}
});

it("does not complete a password change for a destroyed account-security owner", async () => {
	const response = Promise.withResolvers<{ success: true }>();
	const changePassword = vi.fn(
		(_currentPassword: string, _password: string, options?: { signal?: AbortSignal }) =>
			response.promise
	);
	const controller = new AccountSecurityController(
		{ changePassword } as unknown as AdminClient,
		false,
		translate
	);

	const pending = controller.changePassword("old password", "new password");
	controller.destroy();
	response.resolve({ success: true });

	await expect(pending).resolves.toBe(false);
	expect(changePassword.mock.calls[0]?.[2]?.signal?.aborted).toBe(true);
});

it("does not expose an API key result to a destroyed account-security owner", async () => {
	const response = Promise.withResolvers<APIKey>();
	const createAPIKey = vi.fn(
		(_input: { name: string; expiresAt?: string }, _options?: { signal?: AbortSignal }) =>
			response.promise
	);
	const controller = new AccountSecurityController(
		{ createAPIKey } as unknown as AdminClient,
		true,
		translate
	);
	const pending = controller.createAPIKey("Automation");
	controller.destroy();
	response.resolve({
		id: "key-one",
		name: "Automation",
		key: "secret",
		createdAt: "2026-09-13T00:00:00Z",
	});

	await expect(pending).resolves.toBe(false);
	expect(controller.createdAPIKey).toBeUndefined();
	expect(controller.apiKeys).toEqual([]);
	expect(createAPIKey.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
});

it("invalidates an account-security operation when a retained controller loads a new session", async () => {
	const password = Promise.withResolvers<{ success: true }>();
	const changePassword = vi.fn(
		(_currentPassword: string, _password: string, _options?: { signal: AbortSignal }) =>
			password.promise
	);
	const controller = new AccountSecurityController(
		{
			changePassword,
			sessions: vi.fn(async () => []),
		} as unknown as AdminClient,
		false,
		translate
	);

	const pending = controller.changePassword("old password", "new password");
	await controller.load();
	password.resolve({ success: true });

	await expect(pending).resolves.toBe(false);
	expect(changePassword.mock.calls[0]?.[2]?.signal.aborted).toBe(true);
	expect(controller.status).toBe("ready");
});

it("does not let user A's profile save replace user B's session", async () => {
	const update = Promise.withResolvers<AdminDocument>();
	const userA = { id: "user-a", name: "User A" };
	const userB = { id: "user-b", name: "User B" };
	const client = {
		find: vi.fn(async (_slug: string, id: string) => (id === userA.id ? userA : userB)),
		collectionAccess: vi.fn(async () => access),
		update: vi.fn(() => update.promise),
	} as unknown as AdminClient;
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: {
			name: "Account lifetime",
			admin: { userCollectionId: users.id, userCollectionSlug: users.slug },
		},
		collections: [users],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	runtime.session = session("session-a", userA);
	const notifications = new NotificationCenter();
	const success = vi.spyOn(notifications, "success").mockReturnValue(0);
	const router = createMemoryRouter([{ path: "account", Component: AccountRoute }], {
		initialEntries: ["/account"],
	});
	const screen = await render(RuntimeProvider, { router, runtime, notifications });
	try {
		const name = screen.getByRole("textbox", { name: "Name" });
		await expect.element(name).toHaveValue("User A");
		await name.fill("Edited A");
		await screen.getByRole("button", { name: "Save profile" }).click();
		await expect.poll(() => vi.mocked(client.update)).toHaveBeenCalledOnce();

		runtime.session = session("session-b", userB);
		await expect.element(name).toHaveValue("User B");
		update.resolve({ id: userA.id, name: "Saved A" });
		await new Promise((resolve) => window.setTimeout(resolve, 0));

		expect(runtime.session?.id).toBe("session-b");
		expect(runtime.session?.user.id).toBe(userB.id);
		expect(success).not.toHaveBeenCalled();
	} finally {
		await screen.unmount();
		router.dispose();
		runtime.dispose();
		notifications.destroy();
	}
});

it("finishes a profile save without reloading when the session owner stays the same", async () => {
	const user = { id: "user-a", name: "User A" };
	const saved = Promise.withResolvers<AdminDocument>();
	const find = vi.fn(async () => user);
	const client = {
		find,
		collectionAccess: vi.fn(async () => access),
		update: vi.fn(() => saved.promise),
	} as unknown as AdminClient;
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: {
			name: "Account lifetime",
			admin: { userCollectionId: users.id, userCollectionSlug: users.slug },
		},
		collections: [users],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	runtime.session = session("session-a", user);
	const notifications = new NotificationCenter();
	const success = vi.spyOn(notifications, "success").mockReturnValue(0);
	const router = createMemoryRouter([{ path: "account", Component: AccountRoute }], {
		initialEntries: ["/account"],
	});
	const screen = await render(RuntimeProvider, { router, runtime, notifications });
	try {
		const name = screen.getByRole("textbox", { name: "Name" });
		await expect.element(name).toHaveValue("User A");
		await name.fill("Edited A");
		await router.navigate("/account?locale=en&x=1");
		await expect.element(name).toHaveValue("Edited A");
		await screen.getByRole("button", { name: "Save profile" }).click();
		await router.navigate("/account?x=1&locale=en");
		await router.navigate("/account?x=2&locale=en");
		await expect.element(name).toHaveValue("Edited A");
		saved.resolve({ id: user.id, name: "Saved A" });
		await expect.element(name).toHaveValue("Saved A");

		expect(runtime.session?.id).toBe("session-a");
		expect(runtime.session?.user.name).toBe("Saved A");
		expect(find).toHaveBeenCalledOnce();
		expect(success).toHaveBeenCalledOnce();
	} finally {
		await screen.unmount();
		router.dispose();
		runtime.dispose();
		notifications.destroy();
	}
});

const users: SchemaCollection = {
	id: "users",
	slug: "users",
	labels: { singular: "User", plural: "Users" },
	admin: {},
	capabilities: { auth: true, upload: false, versions: false, trash: false, locking: false },
	fields: [
		{
			id: "name",
			name: "name",
			path: "name",
			type: "text",
			category: "scalar",
			required: false,
			unique: false,
			admin: { label: "Name" },
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
		delete: true,
		duplicate: true,
		publish: false,
		unpublish: false,
		restoreDeleted: false,
		deletePermanent: true,
		selectAll: true,
	},
	fields: {},
};

function session(id: string, user: AdminDocument): AuthSession<AdminDocument> {
	return { id, collection: users.slug, user, expiresAt: "2030-01-01T00:00:00Z" };
}
