import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { SCHEMA_MANIFEST_VERSION, type SchemaCollection } from "@riducms/protocol";
import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { DocumentController } from "@admin/features/documents/document-controller.svelte";

const Editor = svelte`
	<script>
		let { Controller, runtime, notifications, navigate, id, ready } = $props();
		// svelte-ignore state_referenced_locally
		const controller = new Controller({
			runtime, notifications, navigate,
			get documentID() { return id; },
			get slug() { return "posts"; },
			get global() { return false; },
			get locale() { return "en"; },
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

const actions = {
	publish: (controller: DocumentController) => controller.changePublication("published"),
	duplicate: (controller: DocumentController) => controller.duplicate(),
	delete: (controller: DocumentController) => controller.remove(),
};

async function editor() {
	const mutation = Promise.withResolvers<AdminDocument>();
	const client = {
		find: vi.fn(async (_slug: string, id: string) => ({ id, title: id, _revision: 2 })),
		collectionAccess: vi.fn(async () => access),
		publish: vi.fn(() => mutation.promise),
		duplicate: vi.fn(() => mutation.promise),
		delete: vi.fn(() => mutation.promise),
	} as unknown as AdminClient;
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Operation ownership" },
		collections: [collection],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	const notifications = new NotificationCenter();
	const success = vi.spyOn(notifications, "success").mockReturnValue(0);
	const error = vi.spyOn(notifications, "error").mockReturnValue(0);
	const navigate = vi.fn();
	let controller!: DocumentController;
	const props = {
		Controller: DocumentController,
		runtime,
		notifications,
		navigate,
		id: "one",
		ready: (value: DocumentController) => {
			controller = value;
		},
	};
	const screen = await render(Editor, props);
	await expect.poll(() => controller.loading).toBe(false);
	return { screen, controller, props, mutation, client, navigate, success, error };
}

for (const [name, act] of Object.entries(actions)) {
	it(`ignores a completed ${name} after the editor changes document`, async () => {
		const fixture = await editor();
		try {
			const pending = act(fixture.controller);
			await fixture.screen.rerender({ ...fixture.props, id: "two" });
			await expect.poll(() => fixture.controller.currentDocument?.id).toBe("two");
			fixture.mutation.resolve({ id: "one", title: "Old completion", _revision: 3 });
			await pending;
			expect(fixture.controller.currentDocument?.id).toBe("two");
			expect(fixture.controller.form.get("title")).toBe("two");
			expect(fixture.navigate).not.toHaveBeenCalled();
			expect(fixture.success).not.toHaveBeenCalled();
			expect(fixture.error).not.toHaveBeenCalled();
		} finally {
			await fixture.screen.unmount();
		}
	});
}

it("does not navigate when duplication completes after unmount", async () => {
	const fixture = await editor();
	const pending = fixture.controller.duplicate();
	await fixture.screen.unmount();
	fixture.mutation.resolve({ id: "copy", title: "Copy" });
	await pending;
	expect(fixture.navigate).not.toHaveBeenCalled();
	expect(fixture.success).not.toHaveBeenCalled();
});

it("ignores failure from an operation belonging to the previous document", async () => {
	const fixture = await editor();
	try {
		const pending = fixture.controller.changePublication("published");
		await fixture.screen.rerender({ ...fixture.props, id: "two" });
		await expect.poll(() => fixture.controller.currentDocument?.id).toBe("two");
		fixture.mutation.reject(new Error("Old publication failed"));
		await pending;
		expect(fixture.error).not.toHaveBeenCalled();
	} finally {
		await fixture.screen.unmount();
	}
});

it("applies a publication result while its document is still active", async () => {
	const fixture = await editor();
	try {
		const pending = fixture.controller.changePublication("published");
		fixture.mutation.resolve({ id: "one", title: "Published", _revision: 3, _status: "published" });
		await pending;
		expect(fixture.controller.form.get("title")).toBe("Published");
		expect(fixture.controller.currentRevision).toBe(3);
		expect(fixture.success).toHaveBeenCalledOnce();
		expect(fixture.error).not.toHaveBeenCalled();
	} finally {
		await fixture.screen.unmount();
	}
});

it("does not publish while document writes are blocked", async () => {
	const fixture = await editor();
	try {
		fixture.controller.form.writeBlocked = true;
		expect(fixture.controller.canPublish).toBe(false);
		await fixture.controller.changePublication("published");
		expect(fixture.client.publish).not.toHaveBeenCalled();
		expect(fixture.controller.publicationOperation).toBe(false);
	} finally {
		await fixture.screen.unmount();
	}
});

it("revokes access when its post-publication access refresh fails", async () => {
	const fixture = await editor();
	try {
		vi.mocked(fixture.client.collectionAccess).mockRejectedValueOnce(
			new Error("Access could not be evaluated")
		);
		const pending = fixture.controller.changePublication("published");
		fixture.mutation.resolve({ id: "one", title: "Published", _revision: 3, _status: "published" });
		await pending;
		expect(fixture.controller.form.get("title")).toBe("Published");
		expect(fixture.controller.form.access).toBeUndefined();
		expect(fixture.controller.error).toBe("Access could not be evaluated");
		expect(fixture.success).toHaveBeenCalledOnce();
		expect(fixture.error).not.toHaveBeenCalled();
	} finally {
		await fixture.screen.unmount();
	}
});

it("does not apply a previous visit's result after returning to the same document", async () => {
	const fixture = await editor();
	try {
		const pending = fixture.controller.changePublication("published");
		await fixture.screen.rerender({ ...fixture.props, id: "two" });
		await expect.poll(() => fixture.controller.currentDocument?.id).toBe("two");
		await fixture.screen.rerender(fixture.props);
		await expect.poll(() => fixture.controller.currentDocument?.id).toBe("one");
		fixture.controller.form.set("title", "New visit edits");
		fixture.mutation.resolve({ id: "one", title: "Previous visit result", _revision: 3 });
		await pending;
		expect(fixture.controller.form.get("title")).toBe("New visit edits");
		expect(fixture.success).not.toHaveBeenCalled();
	} finally {
		await fixture.screen.unmount();
	}
});
