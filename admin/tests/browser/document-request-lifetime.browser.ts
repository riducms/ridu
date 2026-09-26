import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import {
	SCHEMA_MANIFEST_VERSION,
	type SchemaCollection,
	type AdminDocumentDataV1,
	type AdminCreateDataV1,
} from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { DocumentController } from "@admin/features/documents/document-controller.svelte";

const Editor = svelte`
	<script>
		let { Controller, runtime, notifications, navigate, id, editable, prepared, global, ready } = $props();
		// svelte-ignore state_referenced_locally
		const controller = new Controller({
			runtime, notifications, navigate,
			get documentID() { return id; },
			get slug() { return "posts"; },
			get global() { return global; },
			get prepared() { return prepared; },
			get locale() { return "en"; },
			get editable() { return editable; },
		});
		ready(controller);
	</script>
	<p>{controller.loading ? "Loading" : controller.form.get("title")}</p>
`;

const collection: SchemaCollection = {
	id: "posts",
	slug: "posts",
	labels: { singular: "Post", plural: "Posts" },
	admin: {},
	capabilities: { auth: false, upload: false, versions: true, trash: true, locking: false },
	fields: [
		{
			id: "title",
			name: "title",
			path: "title",
			type: "text",
			category: "scalar",
			required: false,
			unique: false,
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
		readVersions: true,
		update: true,
		delete: true,
		duplicate: true,
		publish: true,
		unpublish: true,
		restoreDeleted: true,
		deletePermanent: true,
		selectAll: true,
	},
	fields: {},
};

const document = (id: string, title = id): AdminDocument => ({ id, title, _revision: 2 });

async function editor(
	overrides: Partial<AdminClient> = {},
	schema: SchemaCollection = collection,
	editable = true,
	initial: { prepared?: AdminDocumentDataV1 | AdminCreateDataV1; id?: string; global?: boolean } = {
		id: "one",
	}
) {
	const client = {
		find: vi.fn(async (_slug: string, id: string) => document(id)),
		global: vi.fn(async () => document("posts")),
		globalAccess: vi.fn(async () => access),
		collectionAccess: vi.fn(async () => access),
		versions: vi.fn(async () => []),
		update: vi.fn(async (_slug: string, id: string) => document(id, "Saved")),
		...overrides,
	} as unknown as AdminClient;
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Request lifetime" },
		collections: [schema],
		globals: initial.global ? [schema] : [],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	const notifications = new NotificationCenter();
	const navigate = vi.fn();
	let controller!: DocumentController;
	const props = {
		Controller: DocumentController,
		runtime,
		notifications,
		navigate,
		id: initial.id,
		global: initial.global ?? false,
		prepared: initial.prepared,
		editable,
		ready: (value: DocumentController) => {
			controller = value;
		},
	};
	const screen = await render(Editor, props);
	return { screen, controller, props, client, runtime, notifications };
}

async function dispose(fixture: Awaited<ReturnType<typeof editor>>) {
	await fixture.screen.unmount();
	fixture.runtime.dispose();
	fixture.notifications.destroy();
}

it.each([false, true])(
	"starts a prepared document ready and refreshes through the ordinary client (global=%s)",
	async (global) => {
		const id = global ? "posts" : "one";
		const fixture = await editor({}, collection, true, {
			id,
			global,
			prepared: { document: { value: document(id, "Prepared") }, access: { value: access } },
		});
		try {
			expect(fixture.controller.loading).toBe(false);
			expect(fixture.controller.form.get("title")).toBe("Prepared");
			expect(fixture.client.find).not.toHaveBeenCalled();
			expect(fixture.client.global).not.toHaveBeenCalled();
			expect(fixture.client.collectionAccess).not.toHaveBeenCalled();
			expect(fixture.client.globalAccess).not.toHaveBeenCalled();
			await fixture.controller.refresh();
			expect(global ? fixture.client.global : fixture.client.find).toHaveBeenCalledOnce();
			expect(
				global ? fixture.client.globalAccess : fixture.client.collectionAccess
			).toHaveBeenCalledOnce();
			expect(fixture.controller.form.get("title")).toBe(id);
		} finally {
			await dispose(fixture);
		}
	}
);

it("adopts a new seed for a retained document before rendering and ignores the aborted refresh", async () => {
	const pending = Promise.withResolvers<AdminDocument>();
	const fixture = await editor(
		{ find: vi.fn(() => pending.promise) } as unknown as Partial<AdminClient>,
		collection,
		true,
		{
			id: "one",
			prepared: { document: { value: document("one", "First") }, access: { value: access } },
		}
	);
	try {
		const refresh = fixture.controller.refresh();
		await fixture.screen.rerender({
			...fixture.props,
			id: "two",
			prepared: { document: { value: document("two", "Second") }, access: { value: access } },
		});
		expect(fixture.controller.loading).toBe(false);
		expect(fixture.controller.form.get("title")).toBe("Second");
		pending.resolve(document("one", "Stale"));
		await refresh;
		expect(fixture.controller.form.get("title")).toBe("Second");
		expect(fixture.client.find).toHaveBeenCalledOnce();
	} finally {
		await dispose(fixture);
	}
});

it("retries a prepared error instead of replaying it", async () => {
	const fixture = await editor({}, collection, true, {
		id: "one",
		prepared: {
			document: { error: { code: "access_denied", status: 403, message: "Denied", issues: [] } },
			access: { value: access },
		},
	});
	try {
		expect(fixture.controller.loading).toBe(false);
		expect(fixture.controller.error).toBe("Denied");
		expect(fixture.client.find).not.toHaveBeenCalled();
		await fixture.controller.refresh();
		expect(fixture.controller.error).toBeUndefined();
		expect(fixture.client.find).toHaveBeenCalledOnce();
	} finally {
		await dispose(fixture);
	}
});

it.each([true, false])(
	"only reuses create access evaluated for its actual values (matches=%s)",
	async (matches) => {
		const fixture = await editor({}, collection, true, {
			prepared: {
				values: matches ? {} : { title: "Different defaults" },
				access: { value: access },
			},
		});
		try {
			await expect.poll(() => fixture.controller.loading).toBe(false);
			expect(fixture.client.find).not.toHaveBeenCalled();
			expect(fixture.client.collectionAccess).toHaveBeenCalledTimes(matches ? 0 : 1);
		} finally {
			await dispose(fixture);
		}
	}
);

it("lets a newer refresh supersede the initial load for the same document", async () => {
	const initial = Promise.withResolvers<AdminDocument>();
	const refresh = Promise.withResolvers<AdminDocument>();
	const fixture = await editor({
		find: vi
			.fn()
			.mockImplementationOnce(() => initial.promise)
			.mockImplementationOnce(() => refresh.promise),
	});
	try {
		const pendingRefresh = fixture.controller.refresh();
		refresh.resolve(document("one", "Fresh"));
		await pendingRefresh;
		expect(fixture.controller.form.get("title")).toBe("Fresh");
		initial.resolve(document("one", "Stale initial"));
		await expect.poll(() => fixture.controller.loading).toBe(false);
		expect(fixture.controller.form.get("title")).toBe("Fresh");
	} finally {
		await dispose(fixture);
	}
});

it("does not re-enter the route when form or loading state changes", async () => {
	const fixture = await editor();
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		expect(fixture.client.find).toHaveBeenCalledTimes(1);
		expect(fixture.client.collectionAccess).toHaveBeenCalledTimes(1);
		fixture.controller.form.set("title", "Local edit");
		fixture.controller.form.writeBlocked = true;
		fixture.controller.loading = true;
		fixture.controller.loading = false;
		await new Promise((resolve) => window.setTimeout(resolve, 0));
		expect(fixture.client.find).toHaveBeenCalledTimes(1);
		expect(fixture.client.collectionAccess).toHaveBeenCalledTimes(1);
		expect(fixture.controller.form.get("title")).toBe("Local edit");
	} finally {
		await dispose(fixture);
	}
});

it("lets the latest of two refreshes own the document", async () => {
	const first = Promise.withResolvers<AdminDocument>();
	const second = Promise.withResolvers<AdminDocument>();
	const fixture = await editor();
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		vi.mocked(fixture.client.find)
			.mockImplementationOnce(() => first.promise)
			.mockImplementationOnce(() => second.promise);
		const firstRefresh = fixture.controller.refresh();
		const secondRefresh = fixture.controller.refresh();
		second.resolve(document("one", "Newest"));
		await secondRefresh;
		first.resolve(document("one", "Older"));
		await firstRefresh;
		expect(fixture.controller.form.get("title")).toBe("Newest");
	} finally {
		await dispose(fixture);
	}
});

it("does not let a pending refresh overwrite a newer successful save", async () => {
	const refresh = Promise.withResolvers<AdminDocument>();
	const fixture = await editor();
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		vi.mocked(fixture.client.find).mockImplementationOnce(() => refresh.promise);
		const pendingRefresh = fixture.controller.refresh();
		fixture.controller.form.set("title", "Edited");
		await expect(fixture.controller.save()).resolves.toBe(true);
		expect(fixture.controller.form.get("title")).toBe("Saved");
		refresh.resolve(document("one", "Stale refresh"));
		await pendingRefresh;
		expect(fixture.controller.form.get("title")).toBe("Saved");
	} finally {
		await dispose(fixture);
	}
});

it("does not let a pending refresh overwrite edits made after it started", async () => {
	const refresh = Promise.withResolvers<AdminDocument>();
	const fixture = await editor();
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		vi.mocked(fixture.client.find).mockImplementationOnce(() => refresh.promise);
		const pendingRefresh = fixture.controller.refresh();
		fixture.controller.form.set("title", "Newer edit");
		refresh.resolve(document("one", "Refresh snapshot"));
		await pendingRefresh;
		expect(fixture.controller.form.get("title")).toBe("Newer edit");
	} finally {
		await dispose(fixture);
	}
});

it("does not let an earlier visit to A overwrite a later A visit", async () => {
	const firstVisit = Promise.withResolvers<AdminDocument>();
	const fixture = await editor({
		find: vi.fn(async (_slug, id) => {
			if (id === "one" && firstVisit.promise !== undefined) return firstVisit.promise;
			return document(id);
		}),
	} as never);
	try {
		await fixture.screen.rerender({ ...fixture.props, id: "two" });
		await expect.poll(() => fixture.controller.currentDocument?.id).toBe("two");
		vi.mocked(fixture.client.find).mockResolvedValueOnce(document("one", "Current A"));
		await fixture.screen.rerender(fixture.props);
		await expect.poll(() => fixture.controller.form.get("title")).toBe("Current A");
		fixture.controller.form.set("title", "Current A edit");
		firstVisit.resolve(document("one", "First A"));
		await new Promise((resolve) => window.setTimeout(resolve, 0));
		expect(fixture.controller.form.get("title")).toBe("Current A edit");
	} finally {
		await dispose(fixture);
	}
});

it("stops a superseded load before acquiring its document lock", async () => {
	const oldAccess = Promise.withResolvers<typeof access>();
	const acquireDocumentLock = vi.fn(async () => ({ owned: false, lock: { ownerLabel: "Other" } }));
	const lockingCollection = {
		...collection,
		capabilities: { ...collection.capabilities, locking: true },
	};
	const fixture = await editor(
		{
			collectionAccess: vi.fn(async (_slug, options) =>
				options?.id === "one" ? oldAccess.promise : access
			),
			acquireDocumentLock,
			releaseDocumentLock: vi.fn(async () => undefined),
		} as never,
		lockingCollection
	);
	try {
		await expect.poll(() => vi.mocked(fixture.client.collectionAccess).mock.calls.length).toBe(1);
		await fixture.screen.rerender({ ...fixture.props, id: "two" });
		await expect.poll(() => fixture.controller.currentDocument?.id).toBe("two");
		oldAccess.resolve(access);
		await new Promise((resolve) => window.setTimeout(resolve, 0));
		expect(acquireDocumentLock).not.toHaveBeenCalledWith(
			"posts",
			"one",
			expect.anything(),
			expect.anything()
		);
		expect(fixture.client.versions).not.toHaveBeenCalled();
	} finally {
		await dispose(fixture);
	}
});

it("keeps writes blocked after a lock failure and enables them after retry", async () => {
	const acquireDocumentLock = vi
		.fn()
		.mockRejectedValueOnce(new Error("Lock service unavailable"))
		.mockResolvedValueOnce({ owned: true, lock: { ownerLabel: "Me" } });
	const fixture = await editor(
		{ acquireDocumentLock, releaseDocumentLock: vi.fn(async () => undefined) } as never,
		{ ...collection, capabilities: { ...collection.capabilities, locking: true } }
	);
	try {
		await expect.poll(() => fixture.controller.lock.error).toBe("Lock service unavailable");
		expect(fixture.controller.form.writeBlocked).toBe(true);

		await fixture.controller.lock.retry();
		expect(fixture.controller.lock.error).toBeUndefined();
		expect(fixture.controller.form.writeBlocked).toBe(false);
		expect(acquireDocumentLock).toHaveBeenCalledTimes(2);
	} finally {
		await dispose(fixture);
	}
});

it("acquires a document lock only while the retained route is editable", async () => {
	const acquireDocumentLock = vi.fn(async () => ({ owned: true, lock: { ownerLabel: "Me" } }));
	const releaseDocumentLock = vi.fn(async () => undefined);
	const lockingCollection = {
		...collection,
		capabilities: { ...collection.capabilities, locking: true },
	};
	const fixture = await editor(
		{ acquireDocumentLock, releaseDocumentLock } as never,
		lockingCollection,
		false
	);
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		const releasesBeforeEdit = releaseDocumentLock.mock.calls.length;
		expect(acquireDocumentLock).not.toHaveBeenCalled();

		await fixture.screen.rerender({ ...fixture.props, editable: true });
		await expect.poll(() => acquireDocumentLock).toHaveBeenCalledTimes(1);
		await fixture.screen.rerender({ ...fixture.props, editable: false });
		await expect
			.poll(() => releaseDocumentLock.mock.calls.length)
			.toBeGreaterThan(releasesBeforeEdit);
		expect(acquireDocumentLock).toHaveBeenCalledTimes(1);
	} finally {
		await dispose(fixture);
	}
});

it("does not attach a rejected lock request to the next document", async () => {
	const first = Promise.withResolvers<{ owned: boolean; lock: { ownerLabel: string } }>();
	const acquireDocumentLock = vi
		.fn()
		.mockImplementationOnce(() => first.promise)
		.mockResolvedValueOnce({ owned: true, lock: { ownerLabel: "Me" } });
	const fixture = await editor(
		{ acquireDocumentLock, releaseDocumentLock: vi.fn(async () => undefined) } as never,
		{ ...collection, capabilities: { ...collection.capabilities, locking: true } }
	);
	try {
		await expect.poll(() => acquireDocumentLock).toHaveBeenCalledTimes(1);
		await fixture.screen.rerender({ ...fixture.props, id: "two" });
		await expect.poll(() => acquireDocumentLock).toHaveBeenCalledTimes(2);
		first.reject(new Error("Old lock failed"));
		await new Promise((resolve) => window.setTimeout(resolve, 0));

		expect(fixture.controller.currentDocument?.id).toBe("two");
		expect(fixture.controller.lock.error).toBeUndefined();
		expect(fixture.controller.form.writeBlocked).toBe(false);
	} finally {
		await dispose(fixture);
	}
});

it("ignores an initial load that completes after unmount", async () => {
	const pending = Promise.withResolvers<AdminDocument>();
	const fixture = await editor({ find: vi.fn(() => pending.promise) } as never);
	await fixture.screen.unmount();
	pending.resolve(document("one", "After unmount"));
	await new Promise((resolve) => window.setTimeout(resolve, 0));
	expect(fixture.controller.currentDocument).toBeUndefined();
	fixture.runtime.dispose();
	fixture.notifications.destroy();
});

it("keeps an old heartbeat from changing the new document lock", async () => {
	const heartbeat = Promise.withResolvers<{ owned: boolean; lock: { ownerLabel: string } }>();
	const callbacks: Array<() => Promise<void>> = [];
	const setInterval = vi.spyOn(window, "setInterval").mockImplementation((callback) => {
		callbacks.push(callback as () => Promise<void>);
		return callbacks.length as never;
	});
	const clearInterval = vi.spyOn(window, "clearInterval").mockImplementation(() => undefined);
	const fixture = await editor(
		{
			acquireDocumentLock: vi
				.fn()
				.mockResolvedValueOnce({ owned: true, lock: { ownerLabel: "Me" } })
				.mockImplementationOnce(() => heartbeat.promise)
				.mockResolvedValue({ owned: false, lock: { ownerLabel: "Other" } }),
			releaseDocumentLock: vi.fn(async () => undefined),
		} as never,
		{ ...collection, capabilities: { ...collection.capabilities, locking: true } }
	);
	try {
		await expect.poll(() => callbacks.length).toBeGreaterThan(0);
		const pendingHeartbeat = callbacks.at(-1)!();
		await fixture.screen.rerender({ ...fixture.props, id: "two" });
		await expect.poll(() => fixture.controller.lock.lockedByAnotherEditor).toBe(true);
		heartbeat.resolve({ owned: true, lock: { ownerLabel: "Me" } });
		await pendingHeartbeat;
		expect(fixture.controller.lock.lockedByAnotherEditor).toBe(true);
	} finally {
		await dispose(fixture);
		setInterval.mockRestore();
		clearInterval.mockRestore();
	}
});

it("allows only one heartbeat request in flight for a lock owner", async () => {
	const heartbeat = Promise.withResolvers<{ owned: boolean; lock: { ownerLabel: string } }>();
	let callback!: () => Promise<void>;
	const setInterval = vi.spyOn(window, "setInterval").mockImplementation((value) => {
		callback = value as () => Promise<void>;
		return 1 as never;
	});
	const clearInterval = vi.spyOn(window, "clearInterval").mockImplementation(() => undefined);
	const acquireDocumentLock = vi
		.fn()
		.mockResolvedValueOnce({ owned: true, lock: { ownerLabel: "Me" } })
		.mockImplementation(() => heartbeat.promise);
	const fixture = await editor(
		{ acquireDocumentLock, releaseDocumentLock: vi.fn(async () => undefined) } as never,
		{ ...collection, capabilities: { ...collection.capabilities, locking: true } }
	);
	try {
		await expect.poll(() => callback).toBeTypeOf("function");
		const first = callback();
		const second = callback();
		expect(acquireDocumentLock).toHaveBeenCalledTimes(2);
		heartbeat.resolve({ owned: true, lock: { ownerLabel: "Me" } });
		await Promise.all([first, second]);
	} finally {
		await dispose(fixture);
		setInterval.mockRestore();
		clearInterval.mockRestore();
	}
});
