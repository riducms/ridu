import { tick } from "svelte";
import { expect, it, vi } from "vitest";
import type { AccessCapabilitiesEnvelope, SchemaCollection, SchemaField } from "@riducms/protocol";
import { createAdminI18n } from "@riducms/translations";

import type { AdminDocument } from "@admin/core/api/admin-client";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { ReferenceBrowserWorkflow } from "@admin/features/reference-browser/reference-browser-workflow.svelte";
import { ownController } from "./controller-owner.svelte";

const titleField: SchemaField = {
	id: "title",
	name: "title",
	path: "title",
	type: "text",
	category: "scalar",
	required: false,
	unique: false,
	admin: { label: "Title" },
	text: {},
};
const collection: SchemaCollection = {
	id: "posts",
	slug: "posts",
	labels: { singular: "Post", plural: "Posts" },
	admin: { useAsTitle: "title" },
	capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
	fields: [titleField],
};
const access: AccessCapabilitiesEnvelope = {
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

function pendingEditor(collections?: readonly SchemaCollection[]) {
	const response = Promise.withResolvers<AdminDocument>();
	let signal: AbortSignal | undefined;
	const collectionAccess = vi.fn(async () => access);
	const find = vi.fn((_slug: string, _id: string, options: { signal: AbortSignal }) => {
		signal = options.signal;
		// Deliberately ignore cancellation to exercise stale-response protection too.
		return response.promise;
	});
	const runtime = {
		client: {
			find,
			collectionAccess,
			list: async () => ({
				docs: [],
				pagination: {
					page: 1,
					limit: 10,
					totalDocs: 0,
					totalPages: 0,
					hasNextPage: false,
					hasPrevPage: false,
				},
			}),
		},
		collectionOperations: { posts: { create: true }, users: { read: true } },
		i18n: createAdminI18n(),
	} as unknown as AdminRuntime;
	const controller = ownController(ReferenceBrowserWorkflow, {
		runtime,
		notifications: {} as NotificationCenter,
		field: titleField,
		collection,
		...(collections === undefined ? {} : { collections }),
		hasMany: false,
		selectedIDs: ["original"],
		readOnly: false,
		initialDocumentID: "original",
		onCommit: () => undefined,
		onClose: () => undefined,
		open: true,
		setOpen: () => undefined,
	});
	return { controller, response, collectionAccess, find, signal: () => signal };
}

it("opens the requested reference document while its editor still owns the load", async () => {
	const { controller, response } = pendingEditor();
	response.resolve({ id: "original", title: "Loaded title" });
	await expect.poll(() => controller.editorLoading).toBe(false);
	expect(controller.screen).toBe("document");
	expect(controller.editorDocument?.id).toBe("original");
	expect(controller.form.get("title")).toBe("Loaded title");
});

it("never reinterprets an explicit document ID as belonging to another readable collection", async () => {
	const { response, controller, find } = pendingEditor([
		collection,
		{ ...collection, id: "users", slug: "users" },
	]);
	expect(controller.collection.slug).toBe("posts");
	expect(find).toHaveBeenCalledWith("posts", "original", expect.anything());
	response.resolve({ id: "original", title: "Original collection" });
	await expect.poll(() => controller.editorLoading).toBe(false);
	expect(controller.form.get("title")).toBe("Original collection");
});

it.each(["success", "failure"] as const)(
	"keeps reference results open when an abandoned editor load completes with %s",
	async (outcome) => {
		const { controller, response, collectionAccess, signal } = pendingEditor();
		controller.requestBack();
		if (outcome === "success") response.resolve({ id: "original", title: "Late title" });
		else response.reject(new Error("Late load failure"));
		await response.promise.catch(() => undefined);
		// Let the lookup completion and its Svelte updates finish before checking the current screen.
		await tick();
		expect(controller.screen).toBe("list");
		expect(controller.editorDocument).toBeUndefined();
		expect(controller.editorError).toBeUndefined();
		expect(controller.editorLoading).toBe(false);
		expect(collectionAccess).not.toHaveBeenCalled();
		expect(signal()?.aborted).toBe(true);
	}
);

it("preserves a new reference draft when the preceding editor load completes late", async () => {
	const { controller, response, collectionAccess, signal } = pendingEditor();
	controller.requestBack();
	controller.openNewDocument();
	await expect.poll(() => controller.editorLoading).toBe(false);
	controller.form.set("title", "New unsaved draft");
	response.resolve({ id: "original", title: "Late original title" });
	await response.promise;
	await tick();
	expect(controller.screen).toBe("document");
	expect(controller.creating).toBe(true);
	expect(controller.form.get("title")).toBe("New unsaved draft");
	expect(controller.form.dirty).toBe(true);
	expect(collectionAccess).toHaveBeenCalledOnce();
	expect(signal()?.aborted).toBe(true);
});

it("guards drawer navigation until unsaved edits are discarded", async () => {
	const { controller, response } = pendingEditor();
	response.resolve({ id: "original", title: "Saved title" });
	await expect.poll(() => controller.editorLoading).toBe(false);
	controller.form.set("title", "Unsaved title");
	const navigate = vi.fn();

	controller.requestNavigation(navigate);
	expect(controller.confirmDiscard).toBe(true);
	expect(navigate).not.toHaveBeenCalled();
	controller.confirmDiscard = false;
	expect(controller.form.get("title")).toBe("Unsaved title");

	controller.requestNavigation(navigate);
	controller.discardChanges();
	expect(navigate).toHaveBeenCalledOnce();
	expect(controller.confirmDiscard).toBe(false);
});

it("keeps a created document selected for attachment retry without creating it again", async () => {
	const create = vi.fn(async () => ({ id: "new-post", title: "New post" }));
	const onCommit = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true);
	const setOpen = vi.fn();
	const runtime = {
		client: {
			collectionAccess: async () => access,
			create,
			list: async () => ({
				docs: [],
				pagination: { page: 1, limit: 10, totalDocs: 0, totalPages: 0 },
			}),
		},
		collectionOperations: { posts: { create: true } },
		documentsChanged: vi.fn(),
		i18n: createAdminI18n(),
	} as unknown as AdminRuntime;
	const controller = ownController(ReferenceBrowserWorkflow, {
		runtime,
		notifications: { success: vi.fn(), error: vi.fn() } as unknown as NotificationCenter,
		field: titleField,
		collection,
		hasMany: true,
		selectedIDs: ["existing"],
		readOnly: false,
		initialCreate: true,
		onCommit,
		onClose: vi.fn(),
		open: true,
		setOpen,
	});
	await expect.poll(() => controller.editorLoading).toBe(false);
	controller.form.set("title", "New post");

	expect(await controller.saveEditor()).toBe(true);
	expect(controller.screen).toBe("list");
	expect(controller.directEntry).toBe(false);
	expect(controller.workingSelection).toEqual(["existing", "new-post"]);
	expect(controller.canCommitSelection).toBe(true);
	expect(setOpen).not.toHaveBeenCalled();

	expect(await controller.commitSelection()).toBe(true);
	expect(onCommit).toHaveBeenLastCalledWith(["existing", "new-post"], "posts");
	expect(create).toHaveBeenCalledOnce();
	expect(setOpen).toHaveBeenCalledWith(false);
});

it("switches collection ownership without accepting an old response or reusing selected IDs", async () => {
	const pagination = { page: 1, limit: 10, totalDocs: 1, totalPages: 1 };
	const previous = Promise.withResolvers<{
		docs: AdminDocument[];
		pagination: typeof pagination;
	}>();
	let previousSignal: AbortSignal | undefined;
	const users: SchemaCollection = {
		...collection,
		id: "users",
		slug: "users",
		labels: { singular: "User", plural: "Users" },
		admin: { useAsTitle: "name" },
		fields: [{ ...titleField, id: "name", name: "name", path: "name", admin: { label: "Name" } }],
	};
	const denied = { ...collection, id: "denied", slug: "denied" };
	const list = vi.fn(
		async (slug: string, options: { signal: AbortSignal; where?: Record<string, unknown> }) => {
			if (slug === "posts") {
				previousSignal = options.signal;
				return previous.promise;
			}
			return { docs: [{ id: "same", name: "Current user" }], pagination };
		}
	);
	const optionFilter = vi.fn((candidate: SchemaCollection) => ({
		field: candidate.slug === "posts" ? "title" : "name",
		operator: "equals" as const,
		value: candidate.slug === "posts" ? "Stale post" : "Current user",
	}));
	const onCommit = vi.fn();
	const controller = ownController(ReferenceBrowserWorkflow, {
		runtime: {
			client: { list },
			i18n: createAdminI18n(),
			collectionOperations: {
				posts: { read: true },
				users: { read: true },
				denied: { read: false },
			},
		} as unknown as AdminRuntime,
		notifications: {} as NotificationCenter,
		field: titleField,
		collection,
		collections: [collection, users, denied],
		hasMany: true,
		selectedIDs: ["same"],
		readOnly: false,
		optionFilter,
		open: true,
		setOpen: vi.fn(),
		onClose: vi.fn(),
		onCommit,
	});
	await expect.poll(() => list.mock.calls.length).toBe(1);
	controller.selectCollection("denied");
	expect(controller.collection.slug).toBe("posts");
	controller.query = "old query";
	controller.sort = "-title";
	controller.page = 3;
	controller.selectCollection("users");
	expect(previousSignal?.aborted).toBe(true);
	expect(controller.query).toBe("");
	expect(controller.sort).toBe("");
	expect(controller.page).toBe(1);
	expect(controller.workingSelection).toEqual([]);
	expect(controller.searchField?.path).toBe("name");
	expect(controller.columns.some((column) => column.path === "title")).toBe(false);
	await expect.poll(() => controller.docs[0]?.name).toBe("Current user");
	expect(optionFilter).toHaveBeenLastCalledWith(users);
	expect(list).toHaveBeenLastCalledWith(
		"users",
		expect.objectContaining({ where: { name: { equals: "Current user" } } })
	);
	previous.resolve({ docs: [{ id: "same", title: "Stale post" }], pagination });
	await previous.promise;
	await tick();
	expect(controller.docs[0]?.name).toBe("Current user");
	controller.toggleSelection("same");
	expect(controller.canCommitSelection).toBe(true);
	await controller.commitSelection();
	expect(onCommit).toHaveBeenCalledWith(["same"], "users");
});

it("does not browse or commit when none of the offered collections are readable", async () => {
	const list = vi.fn();
	const onCommit = vi.fn();
	const controller = ownController(ReferenceBrowserWorkflow, {
		runtime: {
			client: { list },
			i18n: createAdminI18n(),
			collectionOperations: { posts: { read: false, create: true } },
		} as unknown as AdminRuntime,
		notifications: {} as NotificationCenter,
		field: titleField,
		collection,
		collections: [collection],
		hasMany: false,
		selectedIDs: [],
		readOnly: false,
		open: true,
		setOpen: vi.fn(),
		onClose: vi.fn(),
		onCommit,
	});
	await tick();

	// A forbidden search would run on the next timer turn, after Svelte's effects.
	await new Promise<void>((resolve) => setTimeout(resolve, 0));
	expect(controller.browsingUnavailable).toBe(true);
	expect(controller.canCreateDocument).toBe(false);
	expect(list).not.toHaveBeenCalled();
	expect(await controller.commitSelection(["unreadable"])).toBe(false);
	expect(onCommit).not.toHaveBeenCalled();
});

it("fetches editable wire values instead of putting populated list cells into the form", async () => {
	const { controller, response, find } = pendingEditor();
	controller.requestBack();
	controller.openKnownDocument({
		id: "original",
		title: "Cached list title",
		author: { id: "user-1", name: "Author" },
	});
	response.resolve({ id: "original", title: "Current document title", author: "user-1" });
	await expect.poll(() => controller.editorLoading).toBe(false);

	expect(find).toHaveBeenLastCalledWith("posts", "original", expect.objectContaining({ depth: 0 }));
	expect(controller.editorDocument?.author).toBe("user-1");
	expect(controller.form.get("title")).toBe("Current document title");
});

function saveableEditor(upload = false) {
	const document = { id: "original", title: "Saved title", filename: "asset.pdf", _revision: 1 };
	const collectionAccess = vi.fn(async () => access);
	const update = vi.fn(async () => ({ ...document, title: "Updated title", _revision: 2 }));
	const runtime = {
		client: {
			collectionAccess,
			update,
			updateUpload: update,
			list: async () => ({
				docs: [],
				pagination: { page: 1, limit: 10, totalDocs: 0, totalPages: 0 },
			}),
		},
		collectionOperations: { posts: { create: true } },
		documentsChanged: vi.fn(),
		i18n: createAdminI18n(),
	} as unknown as AdminRuntime;
	const controller = ownController(ReferenceBrowserWorkflow, {
		runtime,
		notifications: { success: vi.fn(), error: vi.fn() } as unknown as NotificationCenter,
		field: titleField,
		collection: { ...collection, capabilities: { ...collection.capabilities, upload } },
		hasMany: false,
		selectedIDs: [document.id],
		readOnly: false,
		initialDocument: document,
		onCommit: vi.fn(),
		onClose: vi.fn(),
		open: true,
		setOpen: vi.fn(),
	});
	return { controller, collectionAccess, update };
}

it("refreshes value-dependent access after saving before allowing further edits", async () => {
	const { controller, collectionAccess } = saveableEditor();
	await expect.poll(() => controller.editorLoading).toBe(false);
	const refreshedAccess = Promise.withResolvers<AccessCapabilitiesEnvelope>();
	collectionAccess.mockReturnValueOnce(refreshedAccess.promise);
	controller.form.set("title", "Updated title");

	expect(await controller.saveEditor()).toBe(true);
	expect(controller.editorLoading).toBe(true);
	expect(controller.form.access).toBeUndefined();
	expect(controller.canSave).toBe(false);
	refreshedAccess.resolve({ ...access, operations: { ...access.operations, update: false } });
	await expect.poll(() => controller.editorLoading).toBe(false);
	controller.form.set("title", "Another edit");
	expect(controller.canSave).toBe(false);
});

it("returns uncertain upload updates to the list instead of retrying the stale draft", async () => {
	const { controller, update } = saveableEditor(true);
	await expect.poll(() => controller.editorLoading).toBe(false);
	controller.form.set("title", "Updated title");
	update.mockRejectedValueOnce(new Error("Connection lost after upload"));

	expect(await controller.saveEditor()).toBe(false);
	expect(controller.screen).toBe("list");
	expect(controller.editorDocument).toBeUndefined();
	expect(controller.editorDirty).toBe(false);
	expect(controller.canSave).toBe(false);
	expect(await controller.saveEditor()).toBe(false);
	expect(update).toHaveBeenCalledOnce();
});

it("searches upload filenames while retaining the field's option restrictions", async () => {
	const list = vi.fn(async () => ({
		docs: [],
		pagination: { page: 1, limit: 10, totalDocs: 0, totalPages: 0 },
	}));
	const controller = ownController(ReferenceBrowserWorkflow, {
		runtime: { client: { list }, i18n: createAdminI18n() } as unknown as AdminRuntime,
		notifications: {} as NotificationCenter,
		field: titleField,
		collection: {
			...collection,
			capabilities: { ...collection.capabilities, upload: true },
			fields: [
				titleField,
				{
					...titleField,
					id: "filename",
					name: "filename",
					path: "filename",
					admin: { label: "File Name" },
				},
			],
		},
		hasMany: false,
		selectedIDs: [],
		readOnly: false,
		optionFilter: { field: "kind", operator: "equals", value: "image" },
		onCommit: vi.fn(),
		onClose: vi.fn(),
		open: true,
		setOpen: vi.fn(),
	});
	controller.query = "field-notes";

	await expect.poll(() => list.mock.calls.length).toBeGreaterThan(0);
	expect(list).toHaveBeenLastCalledWith(
		"posts",
		expect.objectContaining({
			where: { and: [{ filename: { like: "field-notes" } }, { kind: { equals: "image" } }] },
			includeAccess: true,
			depth: 1,
		})
	);
});
