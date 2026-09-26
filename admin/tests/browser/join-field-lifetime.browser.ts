import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { FieldAuthoringHost, FieldReferenceBrowserProps } from "@riducms/plugin";
import { SCHEMA_MANIFEST_VERSION } from "@riducms/protocol";
import type {
	JoinMutationEnvelope,
	JoinMutationInput,
	OperationCapabilities,
	SchemaCollection,
	SchemaField,
} from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { FormController } from "@admin/core/forms/form-controller.svelte";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const Harness = svelte`
	<script>
		import { MemoryRouter } from "@hvniel/svelte-router";
		import { setAdminI18n } from "@riducms/plugin";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { setNotificationCenter } from "../../src/core/notifications/notification-center.svelte";
		import JoinField from "../../src/fields/join/join-field.svelte";
		let { runtime, notifications, form, field, authoring } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
		setNotificationCenter(notifications);
	</script>
	<MemoryRouter><JoinField {field} {form} {authoring} /></MemoryRouter>
`;

const RendererHarness = svelte`
	<script>
		import { MemoryRouter } from "@hvniel/svelte-router";
		import { setAdminI18n } from "@riducms/plugin";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import { setNotificationCenter } from "../../src/core/notifications/notification-center.svelte";
		import FieldRenderer from "../../src/fields/field-renderer.svelte";
		let { runtime, notifications, form, field } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
		setNotificationCenter(notifications);
	</script>
	<MemoryRouter><FieldRenderer {field} {form} /></MemoryRouter>
`;

// The production field owns the mutation. This authoring seam retains its callback just as
// an asynchronous reference-browser session can, without duplicating the field's state logic.
const ReferenceBrowser = svelte`
	<script>
		let { selectedIDs, initialCreate, initialDocumentID, defaultValues } = $props();
	</script>
	<output data-testid="join-selection">{selectedIDs.join(",")}</output>
	<output data-testid="join-browser-mode">{initialCreate ? "create" : initialDocumentID ?? "manage"}</output>
	<output data-testid="join-default-values">{JSON.stringify(defaultValues)}</output>
`;

const operations: OperationCapabilities = {
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
};

const collection: SchemaCollection = {
	id: "posts",
	slug: "posts",
	labels: { singular: "Post", plural: "Posts" },
	admin: { useAsTitle: "title" },
	capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
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
		},
		{
			id: "author",
			name: "author",
			path: "author",
			type: "relationship",
			category: "scalar",
			required: false,
			unique: false,
			admin: { label: "Author" },
			relationship: { collectionId: "users", collectionSlug: "users", onDelete: "nullify" },
		},
		{
			id: "reviewer",
			name: "reviewer",
			path: "reviewer",
			type: "relationship",
			category: "scalar",
			required: false,
			unique: false,
			admin: { label: "Reviewer" },
			relationship: {
				targets: [{ collectionId: "users", collectionSlug: "users" }],
				polymorphic: true,
				onDelete: "nullify",
			},
		},
	],
};

const users: SchemaCollection = {
	id: "users",
	slug: "users",
	labels: { singular: "User", plural: "Users" },
	admin: { useAsTitle: "name" },
	capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
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
		},
	],
};

const field: SchemaField = {
	id: "related-posts",
	name: "posts",
	path: "links.posts",
	type: "join",
	category: "presentation",
	required: false,
	unique: false,
	admin: { label: "Related posts" },
	join: {
		collectionId: "posts",
		collectionSlug: "posts",
		on: "category",
		limit: 10,
		defaultColumns: ["title", "author", "reviewer"],
	},
};

function documentWith(id: string): AdminDocument {
	return { id: "source", links: { posts: [{ id, title: id }] } };
}

function mutationResult(id: string): JoinMutationEnvelope<AdminDocument> {
	return { doc: documentWith(id), added: 1, removed: 1 };
}

async function joinField(openManage = true) {
	const mutations: {
		result: ReturnType<typeof Promise.withResolvers<JoinMutationEnvelope<AdminDocument>>>;
		signal?: AbortSignal;
	}[] = [];
	const refreshes: {
		result: ReturnType<typeof Promise.withResolvers<AdminDocument>>;
		signal?: AbortSignal;
	}[] = [];
	const labelLoads: {
		result: ReturnType<
			typeof Promise.withResolvers<{ docs: AdminDocument[]; pagination: { totalPages: number } }>
		>;
		signal?: AbortSignal;
	}[] = [];
	const client = {
		list: vi.fn((_collection: string, options?: { signal?: AbortSignal }) => {
			const result = Promise.withResolvers<{
				docs: AdminDocument[];
				pagination: { totalPages: number };
			}>();
			labelLoads.push({ result, signal: options?.signal });
			return result.promise;
		}),
		mutateJoin: vi.fn(
			(
				_collection: string,
				_id: string,
				_path: string,
				_input: JoinMutationInput,
				options?: { signal?: AbortSignal; locale?: string }
			) => {
				const result = Promise.withResolvers<JoinMutationEnvelope<AdminDocument>>();
				mutations.push({ result, signal: options?.signal });
				return result.promise;
			}
		),
		find: vi.fn((_collection: string, _id: string, options?: { signal?: AbortSignal }) => {
			const result = Promise.withResolvers<AdminDocument>();
			refreshes.push({ result, signal: options?.signal });
			return result.promise;
		}),
	};
	const runtime = new AdminRuntime(client as unknown as AdminClient);
	runtime.collectionOperations = { posts: operations };
	const form = new FormController({}, runtime.i18n);
	form.reset(documentWith("Original"), [field]);
	form.setResource({ collection: "categories", id: "source" });
	form.setLocalization("en");
	const notifications = new NotificationCenter();
	const success = vi.spyOn(notifications, "success").mockReturnValue(0);
	const error = vi.spyOn(notifications, "error").mockReturnValue(0);
	let commit: FieldReferenceBrowserProps["onCommit"] | undefined;
	let close: FieldReferenceBrowserProps["onClose"] | undefined;
	const browser: FieldAuthoringHost["referenceBrowser"] = (anchor, props) => {
		commit = props.onCommit;
		close = props.onClose;
		return ReferenceBrowser(anchor, props);
	};
	const authoring = {
		collections: [collection, users],
		referenceBrowser: browser,
	} as unknown as FieldAuthoringHost;
	const screen = await render(Harness, { runtime, notifications, form, field, authoring });
	if (openManage) {
		await screen.getByRole("button", { name: "Manage relationships", exact: true }).click();
		expect(commit).toBeDefined();
	}
	let mounted = true;
	const unmount = async () => {
		if (!mounted) return;
		mounted = false;
		await screen.unmount();
	};
	return {
		screen,
		form,
		runtime,
		client,
		success,
		error,
		mutations,
		refreshes,
		labelLoads,
		commit: (ids: string[]) => Promise.resolve(commit!(ids, collection.slug)),
		closeBrowser: () => close?.(),
		unmount,
		async dispose() {
			await unmount();
			for (const mutation of mutations) mutation.result.resolve(mutationResult("Disposed"));
			for (const load of labelLoads)
				load.result.resolve({ docs: [], pagination: { totalPages: 1 } });
			for (const refresh of refreshes) refresh.result.resolve(documentWith("Disposed"));
			form.disposeBindings();
			runtime.dispose();
			notifications.destroy();
		},
	};
}

it("derives joined documents from the form, retaining a committed override until the form changes", async () => {
	const fixture = await joinField();
	const selection = fixture.screen.getByTestId("join-selection");
	try {
		await expect.element(selection).toHaveTextContent("Original");
		fixture.form.set("links.posts", [{ id: "From form", title: "From form" }]);
		await expect.element(selection).toHaveTextContent("From form");

		const committing = fixture.commit(["Committed"]);
		fixture.mutations[0]!.result.resolve(mutationResult("Committed"));
		expect(await committing).toBe(true);
		await expect.element(selection).toHaveTextContent("Committed");
		expect(fixture.form.get("links.posts")).toEqual([{ id: "From form", title: "From form" }]);
		expect(fixture.success).toHaveBeenCalledOnce();
		expect(fixture.runtime.documentRevision).toBe(1);

		fixture.form.reset(documentWith("Reset"), [field]);
		await expect.element(selection).toHaveTextContent("Reset");
	} finally {
		await fixture.dispose();
	}
});

it("recovers the nested join using the failed mutation's source, locale and abort signal", async () => {
	const fixture = await joinField();
	try {
		const committing = fixture.commit(["Requested"]);
		fixture.mutations[0]!.result.reject(new Error("Mutation not confirmed"));
		await expect.poll(() => fixture.refreshes.length).toBe(1);
		expect(fixture.client.find).toHaveBeenCalledWith("categories", "source", {
			locale: "en",
			signal: fixture.mutations[0]!.signal,
		});
		fixture.refreshes[0]!.result.resolve(documentWith("Recovered"));
		expect(await committing).toBe(false);
		await expect
			.element(fixture.screen.getByTestId("join-selection"))
			.toHaveTextContent("Recovered");
		expect(fixture.error).toHaveBeenCalledOnce();
		expect(fixture.success).not.toHaveBeenCalled();
		await expect
			.element(fixture.screen.getByRole("button", { name: "Manage relationships", exact: true }))
			.toBeEnabled();
	} finally {
		await fixture.dispose();
	}
});

for (const outcome of ["success", "failure"] as const) {
	it(`ignores mutation ${outcome} after the join field unmounts`, async () => {
		const fixture = await joinField();
		try {
			const committing = fixture.commit(["Requested"]);
			await fixture.unmount();
			expect(fixture.mutations[0]!.signal?.aborted).toBe(true);
			if (outcome === "success") fixture.mutations[0]!.result.resolve(mutationResult("Late"));
			else fixture.mutations[0]!.result.reject(new Error("Late mutation failure"));
			expect(await committing).toBe(false);
			expect(await fixture.commit(["Retained callback"])).toBe(false);
			expect(fixture.client.mutateJoin).toHaveBeenCalledOnce();
			expect(fixture.client.find).not.toHaveBeenCalled();
			expect(fixture.success).not.toHaveBeenCalled();
			expect(fixture.error).not.toHaveBeenCalled();
			expect(fixture.runtime.documentRevision).toBe(0);
		} finally {
			await fixture.dispose();
		}
	});

	it(`ignores recovery ${outcome} after the join field unmounts`, async () => {
		const fixture = await joinField();
		try {
			const committing = fixture.commit(["Requested"]);
			fixture.mutations[0]!.result.reject(new Error("Mutation not confirmed"));
			await expect.poll(() => fixture.refreshes.length).toBe(1);
			await fixture.unmount();
			expect(fixture.refreshes[0]!.signal?.aborted).toBe(true);
			if (outcome === "success") fixture.refreshes[0]!.result.resolve(documentWith("Late"));
			else fixture.refreshes[0]!.result.reject(new Error("Late refresh failure"));
			expect(await committing).toBe(false);
			expect(fixture.success).not.toHaveBeenCalled();
			expect(fixture.error).not.toHaveBeenCalled();
		} finally {
			await fixture.dispose();
		}
	});

	it(`keeps the latest mutation pending when superseded mutation ${outcome} arrives`, async () => {
		const fixture = await joinField();
		try {
			const first = fixture.commit(["First"]);
			const second = fixture.commit(["Second"]);
			expect(fixture.mutations[0]!.signal?.aborted).toBe(true);
			if (outcome === "success") fixture.mutations[0]!.result.resolve(mutationResult("Late"));
			else fixture.mutations[0]!.result.reject(new Error("Late mutation failure"));
			expect(await first).toBe(false);
			await expect
				.element(fixture.screen.getByRole("button", { name: "Updating…" }))
				.toBeDisabled();
			await expect
				.element(fixture.screen.getByTestId("join-selection"))
				.toHaveTextContent("Original");
			expect(fixture.client.find).not.toHaveBeenCalled();
			expect(fixture.success).not.toHaveBeenCalled();
			expect(fixture.error).not.toHaveBeenCalled();
			fixture.mutations[1]!.result.resolve(mutationResult("Second"));
			expect(await second).toBe(true);
			await expect
				.element(fixture.screen.getByTestId("join-selection"))
				.toHaveTextContent("Second");
			expect(fixture.success).toHaveBeenCalledOnce();
		} finally {
			await fixture.dispose();
		}
	});
}

it("cannot apply an old recovery or clear pending state after a newer mutation starts", async () => {
	const fixture = await joinField();
	try {
		const first = fixture.commit(["First"]);
		fixture.mutations[0]!.result.reject(new Error("First mutation not confirmed"));
		await expect.poll(() => fixture.refreshes.length).toBe(1);
		const second = fixture.commit(["Second"]);
		expect(fixture.refreshes[0]!.signal?.aborted).toBe(true);
		fixture.refreshes[0]!.result.resolve(documentWith("Late recovery"));
		expect(await first).toBe(false);
		await expect.element(fixture.screen.getByRole("button", { name: "Updating…" })).toBeDisabled();
		await expect
			.element(fixture.screen.getByTestId("join-selection"))
			.toHaveTextContent("Original");
		expect(fixture.error).not.toHaveBeenCalled();
		fixture.mutations[1]!.result.resolve(mutationResult("Second"));
		expect(await second).toBe(true);
		await expect.element(fixture.screen.getByTestId("join-selection")).toHaveTextContent("Second");
		expect(fixture.success).toHaveBeenCalledOnce();
	} finally {
		await fixture.dispose();
	}
});

it("lets a no-op selection supersede a pending mutation without dispatching another write", async () => {
	const fixture = await joinField();
	try {
		const first = fixture.commit(["First"]);
		expect(await fixture.commit(["Original"])).toBe(true);
		expect(fixture.mutations[0]!.signal?.aborted).toBe(true);
		fixture.mutations[0]!.result.resolve(mutationResult("Late"));
		expect(await first).toBe(false);
		await expect
			.element(fixture.screen.getByTestId("join-selection"))
			.toHaveTextContent("Original");
		await expect
			.element(fixture.screen.getByRole("button", { name: "Manage relationships", exact: true }))
			.toBeEnabled();
		expect(fixture.client.mutateJoin).toHaveBeenCalledOnce();
		expect(fixture.success).not.toHaveBeenCalled();
		expect(fixture.error).not.toHaveBeenCalled();
	} finally {
		await fixture.dispose();
	}
});

for (const outcome of ["success", "failure"] as const) {
	it(`discards a ${outcome} response when a retained join field changes source document`, async () => {
		const fixture = await joinField();
		try {
			const first = fixture.commit(["Requested"]);
			const request = fixture.mutations[0]!;
			fixture.form.setResource({ collection: "categories", id: "next" });
			fixture.form.reset({ links: { posts: [{ id: "Next source", title: "Next source" }] } }, [
				field,
			]);
			await expect.poll(() => request.signal?.aborted).toBe(true);
			await expect
				.element(fixture.screen.getByRole("button", { name: "Open Next source" }))
				.toBeVisible();
			if (outcome === "success") request.result.resolve(mutationResult("Stale"));
			else request.result.reject(new Error("Stale mutation"));
			expect(await first).toBe(false);
			await expect
				.element(fixture.screen.getByRole("button", { name: "Open Next source" }))
				.toBeVisible();
			await expect
				.element(fixture.screen.getByRole("button", { name: "Manage relationships", exact: true }))
				.toBeEnabled();
			expect(fixture.client.find).not.toHaveBeenCalled();
			expect(fixture.success).not.toHaveBeenCalled();
			expect(fixture.error).not.toHaveBeenCalled();
		} finally {
			await fixture.dispose();
		}
	});
}

it("opens the related create drawer with the source backlink and refreshes after closing", async () => {
	const fixture = await joinField(false);
	try {
		await fixture.screen.getByRole("button", { name: "Add new" }).click();
		await expect
			.element(fixture.screen.getByTestId("join-browser-mode"))
			.toHaveTextContent("create");
		await expect
			.element(fixture.screen.getByTestId("join-default-values"))
			.toHaveTextContent('{"category":"source"}');
		fixture.closeBrowser();
		await expect.poll(() => fixture.refreshes.length).toBe(1);
		fixture.refreshes[0]!.result.resolve(documentWith("Created"));
		await expect
			.element(fixture.screen.getByRole("button", { name: "Open Created" }))
			.toBeVisible();
	} finally {
		await fixture.dispose();
	}
});

it("confirms a created backlink without requiring permission to update existing related documents", async () => {
	const fixture = await joinField(false);
	try {
		fixture.runtime.collectionOperations = {
			posts: { ...operations, update: false },
		};
		await fixture.screen.getByRole("button", { name: "Add new" }).click();
		const committing = fixture.commit(["Original", "Created"]);
		await expect.poll(() => fixture.refreshes.length).toBe(1);
		fixture.refreshes[0]!.result.resolve({
			id: "source",
			links: {
				posts: [
					{ id: "Original", title: "Original" },
					{ id: "Created", title: "Created" },
				],
			},
		});
		expect(await committing).toBe(true);
		expect(fixture.client.mutateJoin).not.toHaveBeenCalled();
		await expect
			.element(fixture.screen.getByRole("button", { name: "Open Created" }))
			.toBeVisible();
	} finally {
		await fixture.dispose();
	}
});

it("diffs a created backlink against the refreshed join before any fallback mutation", async () => {
	const fixture = await joinField(false);
	try {
		await fixture.screen.getByRole("button", { name: "Add new" }).click();
		const committing = fixture.commit(["Original", "Created", "Missing"]);
		await expect.poll(() => fixture.refreshes.length).toBe(1);
		fixture.refreshes[0]!.result.resolve({
			id: "source",
			links: {
				posts: [
					{ id: "Original", title: "Original" },
					{ id: "Created", title: "Created" },
					{ id: "Concurrent", title: "Concurrent" },
				],
			},
		});
		await expect.poll(() => fixture.mutations.length).toBe(1);
		expect(fixture.client.mutateJoin).toHaveBeenCalledWith(
			"categories",
			"source",
			"links.posts",
			{ additions: ["Missing"], removals: [] },
			expect.objectContaining({ locale: "en" })
		);
		fixture.mutations[0]!.result.resolve(mutationResult("Missing"));
		expect(await committing).toBe(true);
	} finally {
		await fixture.dispose();
	}
});

it("opens a joined row in the related edit drawer and discards a stale close refresh", async () => {
	const fixture = await joinField(false);
	try {
		await fixture.screen.getByRole("button", { name: "Open Original" }).click();
		await expect
			.element(fixture.screen.getByTestId("join-browser-mode"))
			.toHaveTextContent("Original");
		fixture.closeBrowser();
		await expect.poll(() => fixture.refreshes.length).toBe(1);
		fixture.form.setResource({ collection: "categories", id: "next" });
		fixture.form.reset(documentWith("Next source"), [field]);
		await expect.poll(() => fixture.refreshes[0]!.signal?.aborted).toBe(true);
		fixture.refreshes[0]!.result.resolve(documentWith("Stale"));
		await expect
			.element(fixture.screen.getByRole("button", { name: "Open Next source" }))
			.toBeVisible();
		await expect
			.element(fixture.screen.getByRole("button", { name: "Open Stale" }))
			.not.toBeInTheDocument();
	} finally {
		await fixture.dispose();
	}
});

it("resolves a related document's display label in a join column", async () => {
	const fixture = await joinField(false);
	try {
		fixture.form.set("links.posts", [{ id: "post", title: "Post", author: "user-id" }]);
		await expect.poll(() => fixture.labelLoads.length).toBe(1);
		expect(fixture.client.list).toHaveBeenCalledWith("users", {
			where: { id: { in: ["user-id"] } },
			limit: 100,
			page: 1,
			depth: 0,
			locale: "en",
			signal: fixture.labelLoads[0]!.signal,
		});
		fixture.labelLoads[0]!.result.resolve({
			docs: [{ id: "user-id", name: "Alice" }],
			pagination: { totalPages: 1 },
		});
		await expect.element(fixture.screen.getByRole("cell", { name: "Alice" })).toBeVisible();
		await expect
			.element(fixture.screen.getByRole("cell", { name: "user-id" }))
			.not.toBeInTheDocument();
	} finally {
		await fixture.dispose();
	}
});

it("displays a populated polymorphic reference and hydrates its wrapped ID when unpopulated", async () => {
	const fixture = await joinField(false);
	try {
		fixture.form.set("links.posts", [
			{
				id: "post",
				title: "Post",
				reviewer: { relationTo: "users", id: { id: "user-id", name: "Alice" } },
			},
		]);
		await expect.element(fixture.screen.getByRole("cell", { name: "Alice" })).toBeVisible();
		expect(fixture.client.list).not.toHaveBeenCalled();

		fixture.form.set("links.posts", [
			{
				id: "post",
				title: "Post",
				reviewer: { relationTo: "users", id: { id: "user-id" } },
			},
		]);
		await expect.poll(() => fixture.labelLoads.length).toBe(1);
		expect(fixture.client.list).toHaveBeenCalledWith("users", {
			where: { id: { in: ["user-id"] } },
			limit: 100,
			page: 1,
			depth: 0,
			locale: "en",
			signal: fixture.labelLoads[0]!.signal,
		});
		fixture.labelLoads[0]!.result.resolve({
			docs: [{ id: "user-id", name: "Alice" }],
			pagination: { totalPages: 1 },
		});
		await expect.element(fixture.screen.getByRole("cell", { name: "Alice" })).toBeVisible();
	} finally {
		await fixture.dispose();
	}
});

it("toggles join table columns from the shared animated column picker", async () => {
	const fixture = await joinField(false);
	try {
		await fixture.screen.getByRole("button", { name: "Columns" }).click();
		await expect
			.element(fixture.screen.getByRole("button", { name: "Columns" }))
			.toHaveAttribute("aria-expanded", "true");
		await fixture.screen.getByRole("button", { name: "Author", exact: true }).click();
		await expect
			.element(fixture.screen.getByRole("columnheader", { name: /Author/ }))
			.not.toBeInTheDocument();
		await fixture.screen.getByRole("button", { name: "Author", exact: true }).click();
		await expect
			.element(fixture.screen.getByRole("columnheader", { name: /Author/ }))
			.toBeVisible();
	} finally {
		await fixture.dispose();
	}
});

for (const explicitReadOnly of [false, true]) {
	it(`renders a readable computed join with source updates denied and explicit read-only ${explicitReadOnly}`, async () => {
		const schema: SchemaField = explicitReadOnly
			? { ...field, admin: { ...field.admin, readOnly: true } }
			: field;
		const runtime = new AdminRuntime({} as AdminClient);
		runtime.manifest = {
			version: SCHEMA_MANIFEST_VERSION,
			application: { name: "Join access fixture" },
			collections: [collection, users],
			plugins: [],
		};
		runtime.collectionOperations = { posts: operations };
		const form = new FormController({}, runtime.i18n);
		form.reset(documentWith("Original"), [schema]);
		form.setResource({ collection: "categories", id: "source" });
		form.setAccess(
			{
				operations: { ...operations, update: false },
				fields: { [schema.path]: { read: true, create: false, update: false } },
			},
			"update"
		);
		expect(form.canRead(schema.path)).toBe(true);
		expect(form.canWrite(schema.path)).toBe(false);
		const notifications = new NotificationCenter();
		const screen = await render(RendererHarness, { runtime, notifications, form, field: schema });
		try {
			const addNew = screen.getByRole("button", { name: "Add new" });
			const manage = screen.getByRole("button", { name: "Manage relationships" });
			if (explicitReadOnly) {
				await expect.element(addNew).not.toBeInTheDocument();
				await expect.element(manage).not.toBeInTheDocument();
				await expect.element(screen.getByText("Read only")).toBeVisible();
			} else {
				await expect.element(addNew).toBeEnabled();
				await expect.element(manage).toBeEnabled();
			}
		} finally {
			await screen.unmount();
			form.disposeBindings();
			runtime.dispose();
			notifications.destroy();
		}
	});
}

it("batches more than 50 labels and retains cached labels when columns change", async () => {
	const fixture = await joinField(false);
	try {
		fixture.form.set(
			"links.posts",
			Array.from({ length: 120 }, (_, index) => ({
				id: `post-${index}`,
				title: `Post ${index}`,
				author: `user-${index}`,
			}))
		);
		await expect.poll(() => fixture.labelLoads.length).toBe(1);
		expect(fixture.client.list.mock.calls[0]?.[1]).toMatchObject({
			where: { id: { in: Array.from({ length: 100 }, (_, index) => `user-${index}`) } },
		});
		fixture.labelLoads[0]!.result.resolve({
			docs: Array.from({ length: 100 }, (_, index) => ({
				id: `user-${index}`,
				name: `Author ${index}`,
			})),
			pagination: { totalPages: 1 },
		});
		await expect.poll(() => fixture.labelLoads.length).toBe(2);
		fixture.labelLoads[1]!.result.resolve({
			docs: Array.from({ length: 20 }, (_, index) => ({
				id: `user-${index + 100}`,
				name: `Author ${index + 100}`,
			})),
			pagination: { totalPages: 1 },
		});
		await expect
			.element(fixture.screen.getByRole("cell", { name: "Author 119", exact: true }))
			.toBeVisible();
		await fixture.screen.getByRole("button", { name: "Columns", exact: true }).click();
		await fixture.screen.getByRole("button", { name: "Author", exact: true }).click();
		await fixture.screen.getByRole("button", { name: "Author", exact: true }).click();
		await expect
			.element(fixture.screen.getByRole("cell", { name: "Author 119", exact: true }))
			.toBeVisible();
		expect(fixture.client.list).toHaveBeenCalledTimes(2);
	} finally {
		await fixture.dispose();
	}
});

it("discards label responses from an old locale and loads the current locale", async () => {
	const fixture = await joinField(false);
	try {
		fixture.form.set("links.posts", [{ id: "post", title: "Post", author: "user-id" }]);
		await expect.poll(() => fixture.labelLoads.length).toBe(1);
		fixture.form.setLocalization("fr");
		await expect.poll(() => fixture.labelLoads.length).toBe(2);
		expect(fixture.labelLoads[0]!.signal?.aborted).toBe(true);
		fixture.labelLoads[0]!.result.resolve({
			docs: [{ id: "user-id", name: "Stale" }],
			pagination: { totalPages: 1 },
		});
		fixture.labelLoads[1]!.result.resolve({
			docs: [{ id: "user-id", name: "Current" }],
			pagination: { totalPages: 1 },
		});
		await expect
			.element(fixture.screen.getByRole("cell", { name: "Current", exact: true }))
			.toBeVisible();
		await expect
			.element(fixture.screen.getByRole("cell", { name: "Stale", exact: true }))
			.not.toBeInTheDocument();
		expect(fixture.client.list.mock.calls[1]?.[1]).toMatchObject({ locale: "fr" });
	} finally {
		await fixture.dispose();
	}
});
