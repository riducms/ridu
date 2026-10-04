import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import {
	SCHEMA_MANIFEST_VERSION,
	type AccessCapabilitiesEnvelope,
	type SchemaCollection,
} from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import {
	clearFormDraft,
	formDraftBase,
	peekFormDraft,
	saveFormDraft,
	sameFormDraftBase,
} from "@admin/core/forms/form-draft-recovery";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { DocumentController } from "@admin/features/documents/document-controller.svelte";
import { RiduError } from "@riducms/sdk";

const Editor = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import Recovery from "../../src/features/documents/document-draft-recovery.svelte";
		let { Controller, runtime, notifications, id, locale, prepared, ready } = $props();
		setAdminI18n(runtime.i18n);
		// svelte-ignore state_referenced_locally
		const controller = new Controller({
			runtime, notifications, navigate: () => {},
			get documentID() { return id; },
			get slug() { return "recovery"; },
			get global() { return false; },
			get prepared() { return prepared; },
			get locale() { return locale; },
			get editable() { return true; },
		});
		ready(controller);
		let title = $state();
	</script>
	<input bind:this={title} aria-label="Title" value={controller.form.get("title") ?? ""} />
	{#if controller.recoveryConflict !== undefined}
		<Recovery {controller} onResolved={() => title.focus()} />
	{/if}
`;

const textField = (name: string) => ({
	id: name,
	name,
	path: name,
	type: "text" as const,
	category: "scalar" as const,
	required: false,
	unique: false,
	admin: { label: name },
	text: {},
});
const collection: SchemaCollection = {
	id: "recovery",
	slug: "recovery",
	labels: { singular: "Recovery", plural: "Recovery" },
	admin: {},
	capabilities: { auth: false, upload: false, versions: true, trash: false, locking: false },
	fields: [textField("title"), textField("note")],
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
const base: AdminDocument = { id: "one", title: "Saved title", note: "Saved note", _revision: 2 };
const latest: AdminDocument = {
	...base,
	title: "New server title",
	note: "New server note",
	_revision: 3,
};

function checkpoint(schema = collection) {
	clearFormDraft(schema.id, "one");
	saveFormDraft(
		schema,
		"one",
		{ title: "Your unsaved title", note: "Saved note" },
		{ title: "Saved title", note: "Saved note" },
		formDraftBase(base)
	);
}

async function editor(
	document = base,
	options: {
		prepared?: boolean;
		access?: AccessCapabilitiesEnvelope;
		schema?: SchemaCollection;
		find?: AdminClient["find"];
		update?: AdminClient["update"];
		create?: boolean;
		locale?: string;
		preparedValues?: Record<string, unknown>;
	} = {}
) {
	const resolvedAccess = options.access ?? access;
	const client = {
		find: options.find ?? vi.fn(async (_slug: string, id: string) => ({ ...document, id })),
		update: options.update,
		collectionAccess: vi.fn(async () => resolvedAccess),
		countVersions: vi.fn(async () => ({ totalDocs: 0 })),
		unpublish: vi.fn(async () => ({ ...document, _status: "draft", _revision: 4 })),
	} as unknown as AdminClient;
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: {
			name: "Recovery",
			...(options.locale === undefined
				? {}
				: {
						localization: {
							defaultLocale: "en",
							fallback: true,
							locales: [
								{ code: "en", label: "English" },
								{ code: "fr", label: "French" },
							],
						},
					}),
		},
		collections: [options.schema ?? collection],
		globals: [],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	const notifications = new NotificationCenter();
	const success = vi.spyOn(notifications, "success");
	const error = vi.spyOn(notifications, "error");
	const warning = vi.spyOn(notifications, "warning");
	const dismiss = vi.spyOn(notifications, "dismiss");
	let controller!: DocumentController;
	const props = {
		Controller: DocumentController,
		runtime,
		notifications,
		id: options.create ? undefined : "one",
		locale: options.locale,
		prepared: options.prepared
			? options.create
				? { values: options.preparedValues ?? {}, access: { value: resolvedAccess } }
				: { document: { value: document }, access: { value: resolvedAccess } }
			: undefined,
		ready: (value: DocumentController) => {
			controller = value;
		},
	};
	const screen = await render(Editor, props);
	await expect.poll(() => controller.loading).toBe(false);
	const dispose = async () => {
		await screen.unmount();
		runtime.dispose();
		notifications.destroy();
		clearFormDraft(collection.id, props.id, options.locale);
	};
	return { screen, controller, runtime, props, success, error, warning, dismiss, client, dispose };
}

const rowCollection: SchemaCollection = {
	...collection,
	versionSettings: { drafts: true, maxPerDocument: 10, autosaveIntervalSeconds: 0 },
	fields: [
		...collection.fields,
		{
			id: "rows",
			name: "rows",
			path: "rows",
			type: "array",
			category: "nested",
			required: false,
			unique: false,
			admin: { label: "Rows" },
			nested: { fields: [textField("text")] },
		},
	],
};

it("keeps a confirmed creation conflict retryable after correcting input", async () => {
	const fixture = await editor(base, { create: true, prepared: true });
	const create = vi
		.fn()
		.mockRejectedValueOnce(
			new RiduError({
				code: "conflict",
				status: 409,
				message: "Unique value already exists",
				issues: [],
			})
		)
		.mockResolvedValueOnce({ ...base, title: "Available title" });
	fixture.client.create = create;
	try {
		fixture.controller.form.set("title", "Taken title");
		expect(await fixture.controller.save()).toBe(false);
		expect(fixture.controller.serverSaveConflict).toBe(false);
		expect(fixture.controller.saveOutcomeUncertain).toBe(false);
		fixture.controller.form.set("title", "Available title");
		expect(fixture.controller.canSave).toBe(true);
		expect(await fixture.controller.save()).toBe(true);
		expect(create).toHaveBeenCalledTimes(2);
	} finally {
		await fixture.dispose();
	}
});

it.each([false, true])(
	"preserves a late-edited server-removed row for recovery (storage fails=%s)",
	async (storageFails) => {
		const original = { ...base, rows: [{ _key: "row", text: "Original" }] };
		const saved = { ...original, rows: [], _revision: 3 };
		const response = Promise.withResolvers<AdminDocument>();
		const update = vi.fn(() => response.promise);
		const fixture = await editor(original, { schema: rowCollection, update });
		const blockedStorage = storageFails
			? vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
					throw new Error("Storage unavailable");
				})
			: undefined;
		try {
			fixture.controller.form.set("rows.0.text", "Submitted");
			const saving = fixture.controller.save({ silent: true });
			await expect.poll(() => update).toHaveBeenCalledOnce();
			fixture.controller.form.set("rows.0.text", "Typed later");
			response.resolve(saved);
			await saving;
			if (storageFails) {
				expect(fixture.controller.form.get("rows")).toEqual([{ _key: "row", text: "Typed later" }]);
				expect(fixture.controller.serverSaveConflict).toBe(true);
				expect(fixture.controller.recoveryConflict).toBeUndefined();
				expect(fixture.error).toHaveBeenCalledWith(
					expect.objectContaining({ title: expect.stringContaining("checkpointed") })
				);
			} else {
				const checkpoint = peekFormDraft(rowCollection.id, "one");
				expect(checkpoint?.values.rows).toEqual([{ _key: "row", text: "Typed later" }]);
				expect(checkpoint?.original.rows).toEqual([{ _key: "row", text: "Submitted" }]);
				expect(fixture.controller.recoveryConflict).toEqual(checkpoint);
				expect(fixture.controller.form.get("rows")).toEqual([]);
				expect(fixture.controller.canSave).toBe(false);
				await expect
					.element(fixture.screen.getByRole("dialog", { name: "Review unsaved changes" }))
					.toBeVisible();
				await fixture.screen.getByRole("button", { name: "Keep yours", exact: true }).click();
				expect(fixture.controller.form.get("rows")).toEqual([{ _key: "row", text: "Typed later" }]);
			}
		} finally {
			blockedStorage?.mockRestore();
			await fixture.dispose();
		}
	}
);

it.each([false, true])(
	"restores a matching base and its notification discards saved recovery (prepared=%s)",
	async (prepared) => {
		checkpoint();
		const fixture = await editor(base, { prepared });
		try {
			expect(fixture.controller.form.get("title")).toBe("Your unsaved title");
			expect(fixture.controller.form.original.title).toBe("Saved title");
			fixture.controller.checkpointDraft();
			expect(peekFormDraft(collection.id, "one")?.base.revision).toBe(2);
			fixture.success.mock.calls.at(-1)![0].action!.onClick();
			expect(fixture.controller.form.get("title")).toBe("Saved title");
			expect(fixture.controller.form.dirty).toBe(false);
			expect(peekFormDraft(collection.id, "one")).toBeUndefined();
			fixture.controller.checkpointDraft();
			expect(peekFormDraft(collection.id, "one")).toBeUndefined();
		} finally {
			await fixture.dispose();
		}
	}
);

it.each([false, true])(
	"requires a choice for a changed base and keeps latest unrelated fields (prepared=%s)",
	async (prepared) => {
		checkpoint();
		const fixture = await editor(latest, { prepared });
		try {
			expect(fixture.controller.form.values).toEqual({
				title: "New server title",
				note: "New server note",
			});
			expect(fixture.controller.canSave).toBe(false);
			expect(fixture.controller.form.canWrite("title")).toBe(false);
			expect(fixture.success).not.toHaveBeenCalled();
			const dialog = fixture.screen.getByRole("dialog", { name: "Review unsaved changes" });
			await expect.element(dialog).toBeVisible();
			// Opening on load must not put a destructive choice under a stray Enter.
			await expect
				.poll(() => document.activeElement?.classList.contains("ridu-recovery-comparison"))
				.toBe(true);
			await expect.poll(() => dialog.element().textContent).toContain("Saved title");
			await expect.poll(() => dialog.element().textContent).toContain("Your unsaved title");
			await expect.poll(() => dialog.element().textContent).toContain("New server title");
			fixture.controller.checkpointDraft();
			expect(peekFormDraft(collection.id, "one")?.base.revision).toBe(2);
			await fixture.screen.getByRole("button", { name: "Keep yours", exact: true }).click();
			expect(fixture.controller.form.values).toEqual({
				title: "Your unsaved title",
				note: "New server note",
			});
			await expect
				.poll(() => document.activeElement)
				.toBe(fixture.screen.getByRole("textbox", { name: "Title" }).element());
			expect(fixture.controller.currentRevision).toBe(3);
			expect(fixture.controller.form.canWrite("title")).toBe(true);
			fixture.controller.checkpointDraft();
			expect(peekFormDraft(collection.id, "one")?.base.revision).toBe(3);
			fixture.controller.discardChanges();
			expect(fixture.controller.form.values).toEqual({
				title: "New server title",
				note: "New server note",
			});
		} finally {
			await fixture.dispose();
		}
	}
);

it("never exposes checkpoint fields revoked by current read access or restores denied writes", async () => {
	checkpoint();
	const denied = {
		...access,
		fields: {
			title: { read: false, create: false, update: false },
			note: { read: true, create: false, update: false },
		},
	};
	const fixture = await editor({ ...latest, title: undefined }, { access: denied });
	try {
		const dialog = fixture.screen.getByRole("dialog", { name: "Review unsaved changes" });
		await expect.element(dialog).toBeVisible();
		expect(dialog.element().textContent).not.toContain("Your unsaved title");
		expect(dialog.element().textContent).not.toContain("Saved title");
		fixture.controller.keepRecoveredChanges();
		expect(fixture.controller.form.get("title")).toBeUndefined();
		expect(fixture.controller.form.get("note")).toBe("New server note");
		expect(fixture.controller.form.dirty).toBe(false);
	} finally {
		await fixture.dispose();
	}
});

it("a denied update leaves Keep yours disabled and its command cannot adopt the checkpoint", async () => {
	checkpoint();
	const fixture = await editor(latest, {
		access: { ...access, operations: { ...access.operations, update: false } },
	});
	try {
		await expect
			.element(fixture.screen.getByRole("button", { name: "Keep yours", exact: true }))
			.toBeDisabled();
		fixture.controller.keepRecoveredChanges();
		expect(fixture.controller.form.get("title")).toBe("New server title");
		expect(peekFormDraft(collection.id, "one")).toBeDefined();
	} finally {
		await fixture.dispose();
	}
});

it.each([false, true])(
	"a matching checkpoint survives denied update access and restores after permission returns (prepared=%s)",
	async (prepared) => {
		checkpoint();
		const fixture = await editor(base, {
			prepared,
			access: { ...access, operations: { ...access.operations, update: false } },
		});
		try {
			expect(fixture.controller.form.get("title")).toBe("Saved title");
			expect(fixture.controller.form.dirty).toBe(false);
			expect(fixture.success).not.toHaveBeenCalled();
			expect(peekFormDraft(collection.id, "one")?.values.title).toBe("Your unsaved title");
			fixture.controller.checkpointDraft();
			expect(peekFormDraft(collection.id, "one")?.values.title).toBe("Your unsaved title");

			vi.mocked(fixture.client.collectionAccess).mockResolvedValue(access);
			await fixture.controller.refresh();
			expect(fixture.controller.form.get("title")).toBe("Your unsaved title");
			expect(fixture.controller.form.original.title).toBe("Saved title");
			expect(fixture.controller.form.dirty).toBe(true);
		} finally {
			await fixture.dispose();
		}
	}
);

it("reports removed schema edits while recovering compatible fields", async () => {
	const previous = { ...collection, fields: [...collection.fields, textField("removed")] };
	saveFormDraft(
		previous,
		"one",
		{ title: "Your unsaved title", note: "Saved note", removed: "Detached edit" },
		{ title: "Saved title", note: "Saved note", removed: "Old value" },
		formDraftBase(base)
	);
	const fixture = await editor();
	try {
		expect(fixture.controller.form.get("title")).toBe("Your unsaved title");
		expect(fixture.controller.form.get("removed")).toBeUndefined();
		expect(fixture.success).not.toHaveBeenCalled();
		expect(fixture.warning).toHaveBeenCalledWith(
			expect.objectContaining({
				title: fixture.runtime.i18n.t("documents:draftFieldsRestored", { count: 1 }),
				message: fixture.runtime.i18n.t("documents:incompatibleDraftValues", { count: 1 }),
			})
		);
	} finally {
		await fixture.dispose();
	}
});

it("keeps edits recoverable after a failed refresh", async () => {
	const find = vi
		.fn<AdminClient["find"]>()
		.mockResolvedValueOnce(base)
		.mockRejectedValueOnce(new Error("Network unavailable"));
	const fixture = await editor(base, { find: find as AdminClient["find"] });
	try {
		fixture.controller.form.set("title", "Edit made before the refresh failed");
		await fixture.controller.refresh();
		expect(fixture.controller.error).toBe("Network unavailable");
		expect(fixture.controller.form.access).toBeUndefined();
		fixture.controller.checkpointDraft();
		expect(peekFormDraft(collection.id, "one")?.values.title).toBe(
			"Edit made before the refresh failed"
		);
	} finally {
		await fixture.dispose();
	}
});

it("a denied document read never consumes or applies its checkpoint", async () => {
	checkpoint();
	const fixture = await editor(base, {
		find: vi.fn(async () => {
			throw new Error("Read denied");
		}),
	});
	try {
		expect(fixture.controller.error).toBe("Read denied");
		expect(fixture.controller.recoveryConflict).toBeUndefined();
		expect(fixture.controller.form.get("title")).toBeUndefined();
		fixture.controller.checkpointDraft();
		expect(peekFormDraft(collection.id, "one")).toBeDefined();
	} finally {
		await fixture.dispose();
	}
});

it("a recovered notification cannot discard edits belonging to the next route", async () => {
	checkpoint();
	const fixture = await editor();
	try {
		const discard = fixture.success.mock.calls.at(-1)![0].action!.onClick;
		await fixture.screen.rerender({ ...fixture.props, id: "two" });
		await expect.poll(() => fixture.controller.currentDocument?.id).toBe("two");
		fixture.controller.form.set("title", "Next document edit");
		discard();
		expect(fixture.controller.form.get("title")).toBe("Next document edit");
		expect(fixture.dismiss).toHaveBeenCalled();
	} finally {
		await fixture.dispose();
	}
});

it("compares the checkpoint base by revision, then updatedAt", () => {
	checkpoint();
	const draft = peekFormDraft(collection.id, "one")!;
	expect(sameFormDraftBase(draft, base)).toBe(true);
	expect(sameFormDraftBase(draft, latest)).toBe(false);
	const timestamped = { ...draft, base: { updatedAt: "2026-10-01T10:00:00Z" } };
	expect(
		sameFormDraftBase(timestamped, { ...base, _revision: 0, updatedAt: "2026-10-01T10:00:00Z" })
	).toBe(true);
	expect(
		sameFormDraftBase(timestamped, { ...base, _revision: 0, updatedAt: "2026-10-01T11:00:00Z" })
	).toBe(false);
	expect(sameFormDraftBase({ ...draft, base: {} }, { id: "one", title: "Saved title" })).toBe(
		false
	);
	clearFormDraft(collection.id, "one");
});

it("storage denial does not break checkpointing, reads or discard", async () => {
	const fixture = await editor();
	const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
		throw new DOMException("Denied", "SecurityError");
	});
	const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
		throw new DOMException("Denied", "SecurityError");
	});
	const remove = vi.spyOn(Storage.prototype, "removeItem").mockImplementation(() => {
		throw new DOMException("Denied", "SecurityError");
	});
	try {
		fixture.controller.form.set("title", "Unsaved");
		expect(() => fixture.controller.checkpointDraft()).not.toThrow();
		expect(peekFormDraft(collection.id, "one")).toBeUndefined();
		fixture.controller.discardChanges();
		expect(fixture.controller.form.get("title")).toBe("Saved title");
	} finally {
		set.mockRestore();
		get.mockRestore();
		remove.mockRestore();
		await fixture.dispose();
	}
});

it("does not refresh a save conflict from an older checkpoint when current edits cannot be stored", async () => {
	const fixture = await editor();
	try {
		expect(
			saveFormDraft(
				collection,
				"one",
				{ title: "Older edit", note: "Saved note" },
				{ title: "Saved title", note: "Saved note" },
				formDraftBase(base)
			)
		).toBe(true);
		fixture.controller.form.set("title", "Current unsaved edit");
		fixture.controller.serverSaveConflict = true;
		const reads = vi.mocked(fixture.client.find).mock.calls.length;
		const denied = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
			throw new DOMException("Denied", "SecurityError");
		});
		try {
			await fixture.controller.reviewServerConflict();
		} finally {
			denied.mockRestore();
		}
		expect(vi.mocked(fixture.client.find).mock.calls.length).toBe(reads);
		expect(fixture.controller.form.get("title")).toBe("Current unsaved edit");
		expect(peekFormDraft(collection.id, "one")?.values.title).toBe("Older edit");
		expect(fixture.controller.serverSaveConflict).toBe(true);
	} finally {
		await fixture.dispose();
	}
});

it("a denied refresh invalidates prior access without consuming a pending checkpoint", async () => {
	checkpoint();
	const reload = Promise.withResolvers<AdminDocument>();
	const find = vi
		.fn<AdminClient["find"]>()
		.mockResolvedValueOnce(latest)
		.mockImplementationOnce(() => reload.promise);
	const fixture = await editor(latest, { find: find as AdminClient["find"] });
	try {
		expect(fixture.controller.recoveryComparison).toHaveLength(2);
		const pending = fixture.controller.refresh();
		expect(fixture.controller.recoveryComparison).toEqual([]);
		reload.reject(new Error("Read denied"));
		await pending;
		expect(fixture.controller.error).toBe("Read denied");
		expect(fixture.controller.form.access).toBeUndefined();
		expect(fixture.controller.recoveryComparison).toEqual([]);
		expect(fixture.controller.canKeepRecoveredChanges).toBe(false);
		fixture.controller.checkpointDraft();
		expect(peekFormDraft(collection.id, "one")?.values.title).toBe("Your unsaved title");
	} finally {
		await fixture.dispose();
	}
});

it("does not expose or replace a denied child in a recovered container", async () => {
	const schema: SchemaCollection = {
		...collection,
		fields: [
			{
				...textField("details"),
				type: "group",
				category: "nested",
				nested: {
					fields: [
						{ ...textField("public"), path: "details.public" },
						{ ...textField("secret"), path: "details.secret" },
					],
				},
			},
		],
	};
	saveFormDraft(
		schema,
		"one",
		{ details: { public: "Unsaved public", secret: "Old private secret" } },
		{ details: { public: "Old public", secret: "Old private secret" } },
		{ revision: 2 }
	);
	const fixture = await editor(
		{ id: "one", details: { public: "Latest public" }, _revision: 3 },
		{
			schema,
			access: {
				...access,
				fields: { "details.secret": { read: false, update: false, create: false } },
			},
		}
	);
	try {
		const dialog = fixture.screen.getByRole("dialog", { name: "Review unsaved changes" });
		await expect.element(dialog).toBeVisible();
		expect(dialog.element().textContent).not.toContain("Old private secret");
		fixture.controller.keepRecoveredChanges();
		expect(fixture.controller.form.get("details")).toEqual({ public: "Latest public" });
	} finally {
		await fixture.dispose();
	}
});

it.each([false, true])(
	"create recovery waits for create field access (prepared=%s)",
	async (prepared) => {
		const values = { title: "Private draft", note: "Allowed draft" };
		saveFormDraft(collection, undefined, values, {});
		const fixture = await editor(base, {
			create: true,
			prepared,
			preparedValues: values,
			access: { ...access, fields: { title: { read: false, create: false, update: false } } },
		});
		try {
			expect(fixture.controller.form.get("title")).toBeUndefined();
			expect(fixture.controller.form.get("note")).toBe("Allowed draft");
			expect(fixture.controller.form.canWrite("title")).toBe(false);
			if (prepared) expect(fixture.client.collectionAccess).not.toHaveBeenCalled();
			else
				expect(fixture.client.collectionAccess).toHaveBeenCalledWith(
					"recovery",
					expect.objectContaining({ data: values })
				);
		} finally {
			await fixture.dispose();
		}
	}
);

it("a denied create operation preserves its unapplied draft for another reload", async () => {
	saveFormDraft(collection, undefined, { title: "Private draft" }, {});
	const fixture = await editor(base, {
		create: true,
		access: { ...access, operations: { ...access.operations, create: false } },
	});
	try {
		expect(fixture.controller.form.get("title")).toBeUndefined();
		fixture.controller.checkpointDraft();
		expect(peekFormDraft(collection.id, undefined)?.values.title).toBe("Private draft");
	} finally {
		await fixture.dispose();
	}
});

it("create recovery survives the temporary edit gate during a schema access refresh", async () => {
	saveFormDraft(collection, undefined, { title: "Unsaved create draft" }, {});
	const fixture = await editor(base, {
		create: true,
		access: { ...access, operations: { ...access.operations, create: false } },
	});
	try {
		expect(peekFormDraft(collection.id, undefined)?.values.title).toBe("Unsaved create draft");
		const reload = Promise.withResolvers<AccessCapabilitiesEnvelope>();
		vi.mocked(fixture.client.collectionAccess).mockImplementationOnce(() => reload.promise);
		fixture.runtime.manifestRevision += 1;
		await expect.poll(() => fixture.controller.form.writeBlocked).toBe(true);
		expect(peekFormDraft(collection.id, undefined)?.values.title).toBe("Unsaved create draft");

		reload.resolve(access);
		await expect.poll(() => fixture.controller.loading).toBe(false);
		expect(fixture.controller.form.get("title")).toBe("Unsaved create draft");
		expect(fixture.controller.form.original.title).toBeUndefined();
		expect(fixture.controller.form.canWrite("title")).toBe(true);
	} finally {
		await fixture.dispose();
	}
});

it.each([false, true])(
	"create recovery preserves occurrence access evaluated against recovered values (prepared=%s)",
	async (prepared) => {
		const schema: SchemaCollection = {
			...collection,
			fields: [
				{
					...textField("items"),
					type: "array",
					category: "nested",
					nested: { fields: [{ ...textField("text"), path: "items.text" }] },
				},
			],
		};
		const values = {
			items: [{ _key: "private" }, { _key: "public", text: "Unsaved public copy" }],
		};
		saveFormDraft(schema, undefined, values, { items: [] });
		const fixture = await editor(base, {
			create: true,
			prepared,
			preparedValues: values,
			schema,
			access: {
				...access,
				fields: { "items.0.text": { read: false, create: false, update: false } },
			},
		});
		try {
			expect(fixture.controller.form.get("items")).toEqual(values.items);
			expect(fixture.controller.form.canRead("items.0.text")).toBe(false);
			expect(fixture.controller.form.hasWriteAccess("items.0.text")).toBe(false);
			expect(fixture.controller.form.canWrite("items.0.text")).toBe(false);
			expect(fixture.controller.form.canRead("items.1.text")).toBe(true);
			expect(fixture.controller.form.hasWriteAccess("items.1.text")).toBe(true);
			const permissions = Promise.withResolvers<AccessCapabilitiesEnvelope>();
			const accessReads = vi.mocked(fixture.client.collectionAccess);
			const before = accessReads.mock.calls.length;
			accessReads.mockImplementationOnce(() => permissions.promise);
			fixture.runtime.manifestRevision += 1;
			await expect.poll(() => accessReads.mock.calls.length).toBe(before + 1);
			expect(accessReads.mock.calls.at(-1)?.[1]?.data).toEqual(values);
			fixture.controller.form.setRows("items", [...values.items].reverse());
			permissions.resolve({
				...access,
				fields: { "items.0.text": { read: false, create: false, update: false } },
			});
			await expect.poll(() => fixture.controller.loading).toBe(false);
			expect(fixture.controller.form.get("items")).toEqual([...values.items].reverse());
			expect(fixture.controller.form.canRead("items.1.text")).toBe(false);
			expect(fixture.controller.form.hasWriteAccess("items.1.text")).toBe(false);
			expect(fixture.controller.form.canRead("items.0.text")).toBe(true);
			expect(fixture.controller.form.hasWriteAccess("items.0.text")).toBe(true);
		} finally {
			await fixture.dispose();
		}
	}
);

it("checkpoints stay with their content locale even at the same document revision", async () => {
	saveFormDraft(
		collection,
		"one",
		{ title: "Unsaved English" },
		{ title: "Saved English" },
		{ revision: 2 },
		"en"
	);
	const fixture = await editor({ ...base, title: "French server copy" }, { locale: "fr" });
	try {
		expect(fixture.controller.form.get("title")).toBe("French server copy");
		expect(fixture.controller.recoveryConflict).toBeUndefined();
		fixture.controller.form.set("title", "Unsaved French");
		fixture.controller.checkpointDraft();
		expect(peekFormDraft(collection.id, "one", "fr")?.values.title).toBe("Unsaved French");
		fixture.controller.discardChanges();
		expect(peekFormDraft(collection.id, "one", "fr")).toBeUndefined();
		expect(peekFormDraft(collection.id, "one", "en")?.values.title).toBe("Unsaved English");
	} finally {
		clearFormDraft(collection.id, "one", "en");
		await fixture.dispose();
	}
});

it.each([false, true])(
	"reordered checkpoint rows retain occurrence access (redacted=%s)",
	async (redacted) => {
		const schema: SchemaCollection = {
			...collection,
			fields: [
				{
					...textField("items"),
					type: "array",
					category: "nested",
					nested: { fields: [{ ...textField("text"), path: "items.text" }] },
				},
			],
		};
		const original = {
			items: [
				{ _key: "public", text: "Public copy" },
				{ _key: "private", ...(redacted ? {} : { text: "Private secret" }) },
			],
		};
		const values = { items: [original.items[1], original.items[0]] };
		saveFormDraft(schema, "one", values, original, { revision: 1 });
		const fixture = await editor(
			{
				id: "one",
				_revision: 2,
				items: [{ _key: "public", text: "Public copy" }, { _key: "private" }],
			},
			{
				schema,
				access: {
					...access,
					fields: { "items.1.text": { read: false, create: false, update: false } },
				},
			}
		);
		try {
			expect(JSON.stringify(fixture.controller.recoveryComparison)).not.toContain("Private secret");
			fixture.controller.keepRecoveredChanges();
			const saved = [{ _key: "public", text: "Public copy" }, { _key: "private" }];
			expect(fixture.controller.form.get("items")).toEqual(redacted ? values.items : saved);
			const privatePath = `items.${redacted ? 0 : 1}.text`;
			const publicPath = `items.${redacted ? 1 : 0}.text`;
			expect(fixture.controller.form.canRead(privatePath)).toBe(false);
			expect(fixture.controller.form.hasWriteAccess(privatePath)).toBe(false);
			expect(fixture.controller.form.canWrite(privatePath)).toBe(false);
			expect(fixture.controller.form.canRead(publicPath)).toBe(true);
			expect(fixture.controller.form.hasWriteAccess(publicPath)).toBe(true);
			fixture.controller.discardChanges();
			expect(fixture.controller.form.get("items")).toEqual(saved);
			expect(fixture.controller.form.canRead("items.1.text")).toBe(false);
			expect(fixture.controller.form.hasWriteAccess("items.1.text")).toBe(false);
			expect(fixture.controller.form.canRead("items.0.text")).toBe(true);
			expect(fixture.controller.form.hasWriteAccess("items.0.text")).toBe(true);
		} finally {
			await fixture.dispose();
		}
	}
);

it("unpublish cannot erase an unresolved checkpoint before a recovery choice", async () => {
	checkpoint();
	const fixture = await editor({ ...latest, _status: "published" });
	try {
		expect(fixture.controller.canUnpublish).toBe(false);
		await fixture.controller.changePublication("draft");
		expect(fixture.client.unpublish).not.toHaveBeenCalled();
		fixture.controller.checkpointDraft();
		expect(peekFormDraft(collection.id, "one")?.values.title).toBe("Your unsaved title");
		expect(fixture.controller.recoveryConflict).toBeDefined();
	} finally {
		await fixture.dispose();
	}
});

it.each(["reorder", "remove", "revoke", "publication", "schema"])(
	"refresh rebases evaluated access onto retained recovery edits and Discard (%s)",
	async (transition) => {
		const schema: SchemaCollection = {
			...collection,
			fields: [
				{
					...textField("items"),
					type: "array",
					category: "nested",
					nested: { fields: [{ ...textField("text"), path: "items.text" }] },
				},
			],
		};
		const original = {
			items: [{ _key: "public", text: "Public copy" }, { _key: "private" }],
		};
		const document = { id: "one", _revision: 2, ...original };
		const denied = { read: false, create: false, update: false };
		const evaluatedAccess = { ...access, fields: { "items.1.text": denied } };
		saveFormDraft(schema, "one", { items: [...original.items].reverse() }, original, {
			revision: 1,
		});
		const reload = Promise.withResolvers<AdminDocument>();
		const permissions = Promise.withResolvers<AccessCapabilitiesEnvelope>();
		const find = vi
			.fn<AdminClient["find"]>()
			.mockResolvedValueOnce(document)
			.mockImplementationOnce(() => reload.promise);
		const fixture = await editor(document, {
			schema,
			access: evaluatedAccess,
			find: find as AdminClient["find"],
		});
		try {
			fixture.controller.keepRecoveredChanges();
			expect(fixture.controller.form.canRead("items.0.text")).toBe(false);
			if (transition === "remove") fixture.controller.form.setRows("items", [original.items[0]!]);
			vi.mocked(fixture.client.collectionAccess).mockImplementationOnce(() => permissions.promise);
			const pending =
				transition === "schema"
					? undefined
					: transition === "publication"
						? fixture.controller.changePublication("draft")
						: fixture.controller.refresh();
			if (transition === "schema") {
				fixture.runtime.manifestRevision += 1;
				await expect
					.poll(() => vi.mocked(fixture.client.collectionAccess).mock.calls.length)
					.toBe(2);
			}
			if (transition === "publication") {
				await expect
					.poll(() => vi.mocked(fixture.client.collectionAccess).mock.calls.length)
					.toBe(2);
				fixture.controller.form.setRows("items", [...original.items].reverse());
			}
			const publicIndex = transition === "remove" ? 0 : 1;
			if (transition !== "schema")
				fixture.controller.form.set(`items.${publicIndex}.text`, "Edit made during refresh");
			reload.resolve(document);
			permissions.resolve({
				...evaluatedAccess,
				fields: {
					...evaluatedAccess.fields,
					...(transition === "revoke" ? { "items.0.text": denied } : {}),
				},
			});
			await pending;
			await expect.poll(() => fixture.controller.loading).toBe(false);
			expect(fixture.controller.form.get(`items.${publicIndex}.text`)).toBe(
				transition === "schema" ? "Public copy" : "Edit made during refresh"
			);
			expect(fixture.controller.form.canRead(`items.${publicIndex}.text`)).toBe(
				transition !== "revoke"
			);
			expect(fixture.controller.form.hasWriteAccess(`items.${publicIndex}.text`)).toBe(
				transition !== "revoke"
			);
			if (transition !== "remove") {
				expect(fixture.controller.form.canRead("items.0.text")).toBe(false);
				expect(fixture.controller.form.hasWriteAccess("items.0.text")).toBe(false);
			}
			fixture.controller.discardChanges();
			expect(fixture.controller.form.get("items")).toEqual(original.items);
			expect(fixture.controller.form.canRead("items.1.text")).toBe(false);
			expect(fixture.controller.form.hasWriteAccess("items.1.text")).toBe(false);
			expect(fixture.controller.form.canRead("items.0.text")).toBe(transition !== "revoke");
			expect(fixture.controller.form.hasWriteAccess("items.0.text")).toBe(transition !== "revoke");
		} finally {
			await fixture.dispose();
		}
	}
);

it.each([false, true])(
	"reordered plugin payloads retain occurrence access (redacted=%s)",
	async (redacted) => {
		const schema: SchemaCollection = {
			...collection,
			fields: [
				{
					...textField("body"),
					type: "plugin",
					category: "plugin",
					plugin: {
						key: "outline",
						config: {},
						embeddedTrees: [
							{
								version: 1,
								key: "widgets",
								root: ["outline"],
								children: "items",
								tag: "kind",
								cases: [
									{
										tagValue: "widget",
										payload: "content",
										discriminator: "schema",
										identity: "uid",
										types: [
											{
												slug: "card",
												labels: { singular: "Card", plural: "Cards" },
												fields: [{ ...textField("title"), path: "body.title" }],
											},
										],
									},
								],
							},
						],
					},
				},
			],
		};
		const row = (uid: string, title?: string) => ({
			kind: "widget",
			content: { schema: "card", uid, ...(title === undefined ? {} : { title }) },
		});
		const original = {
			body: {
				outline: [
					row("public", "Public copy"),
					row("private", redacted ? undefined : "Private secret"),
				],
			},
		};
		saveFormDraft(
			schema,
			"one",
			{ body: { outline: [original.body.outline[1], original.body.outline[0]] } },
			original,
			{ revision: 1 }
		);
		const document = {
			id: "one",
			_revision: 2,
			body: { outline: [row("public", "Public copy"), row("private")] },
		};
		const fixture = await editor(document, {
			schema,
			access: {
				...access,
				fields: { "body.outline.1.content.title": { read: false, create: false, update: false } },
			},
		});
		try {
			expect(JSON.stringify(fixture.controller.recoveryComparison)).not.toContain("Private secret");
			fixture.controller.keepRecoveredChanges();
			expect(fixture.controller.form.get("body")).toEqual(
				redacted ? { outline: [original.body.outline[1], original.body.outline[0]] } : document.body
			);
			const privatePath = `body.outline.${redacted ? 0 : 1}.content.title`;
			const publicPath = `body.outline.${redacted ? 1 : 0}.content.title`;
			expect(fixture.controller.form.canRead(privatePath)).toBe(false);
			expect(fixture.controller.form.hasWriteAccess(privatePath)).toBe(false);
			expect(fixture.controller.form.canWrite(privatePath)).toBe(false);
			expect(fixture.controller.form.canRead(publicPath)).toBe(true);
			expect(fixture.controller.form.hasWriteAccess(publicPath)).toBe(true);
			fixture.controller.discardChanges();
			expect(fixture.controller.form.get("body")).toEqual(document.body);
			expect(fixture.controller.form.canRead("body.outline.1.content.title")).toBe(false);
			expect(fixture.controller.form.hasWriteAccess("body.outline.1.content.title")).toBe(false);
			expect(fixture.controller.form.canRead("body.outline.0.content.title")).toBe(true);
			expect(fixture.controller.form.hasWriteAccess("body.outline.0.content.title")).toBe(true);
		} finally {
			await fixture.dispose();
		}
	}
);

it.each([false, true])(
	"unmatched recovered rows require an unrestricted field contract (restricted=%s)",
	async (restricted) => {
		const schema: SchemaCollection = {
			...collection,
			fields: [
				{
					...textField("items"),
					type: "array",
					category: "nested",
					nested: { fields: [{ ...textField("text"), path: "items.text" }] },
				},
			],
		};
		const original = { items: [{ _key: "saved", text: "Saved row" }] };
		const values = { items: [...original.items, { _key: "new", text: "Unsaved row" }] };
		saveFormDraft(schema, "one", values, original, { revision: 1 });
		const fixture = await editor(
			{ id: "one", _revision: 2, ...original },
			{
				schema,
				access: {
					...access,
					fields: restricted ? { "items.text": { read: true, create: true, update: true } } : {},
				},
			}
		);
		try {
			if (restricted)
				expect(JSON.stringify(fixture.controller.recoveryComparison)).not.toContain("Unsaved row");
			fixture.controller.keepRecoveredChanges();
			expect(fixture.controller.form.get("items")).toEqual(
				restricted ? original.items : values.items
			);
		} finally {
			await fixture.dispose();
		}
	}
);
