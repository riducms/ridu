import type {
	AccessCapabilitiesEnvelope,
	AdminCollectionListDataV1,
	SchemaCollection,
} from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { RiduError } from "@riducms/sdk";
import type { AdminClient } from "@admin/core/api/admin-client";
import type { CollectionListController } from "@admin/features/collections/collection-list-controller.svelte";

const operations: AccessCapabilitiesEnvelope["operations"] = {
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
	deletePermanent: false,
	selectAll: true,
};
function access(titleUpdate = true): AccessCapabilitiesEnvelope {
	return { operations, fields: { title: { read: true, create: true, update: titleUpdate } } };
}
function data(id = "one"): AdminCollectionListDataV1 {
	return {
		query: { trash: false, where: { title: { equals: "server-selected" } } },
		page: {
			value: {
				docs: [{ id, title: id, status: "draft" }],
				pagination: {
					page: 1,
					limit: 25,
					totalDocs: 2,
					totalPages: 1,
					hasNextPage: false,
					hasPrevPage: false,
				},
				access: { collection: access(), documents: { [id]: access() } },
			},
		},
		counts: { "": { value: 2 }, draft: { value: 1 } },
	};
}
function clientFixture() {
	const pageRead = vi.fn(async () => data());
	const countRead = vi.fn(async (): Promise<AdminCollectionListDataV1> => ({
		query: { trash: false },
		counts: { "": { value: 2 }, draft: { value: 1 } },
	}));
	const selection = vi.fn(async () => ({ items: [{ id: "one", access: access() }] }));
	const bulkUpdate = vi.fn(async () => []);
	const emptyTrash = vi.fn(async () => []);
	const read = vi.fn(
		(_slug: string, _query: string, part: string, _options?: { signal?: AbortSignal }) =>
			part === "counts" ? countRead() : pageRead()
	);
	return {
		client: {
			adminCollectionList: read,
			resolveFilteredSelection: selection,
			bulkUpdate,
			emptyTrash,
		} as unknown as AdminClient,
		pageRead,
		countRead,
		selection,
		bulkUpdate,
		emptyTrash,
		read,
	};
}
const ControllerOwner = svelte`
 <script>
  let {client, capture, query, locale, trashOnly, prepared, counts, canLoad, collection, slug, notifications} = $props();
  import { CollectionListController } from '../../src/features/collections/collection-list-controller.svelte';
  import { createAdminI18n } from '@riducms/translations';
  const controller = new CollectionListController({
   client, notifications, i18n:createAdminI18n(),
   get slug(){return slug},
   get collection(){return collection},
   get query(){return query}, get locale(){return locale}, get trashOnly(){return trashOnly},
   get ready(){return canLoad}, get prepared(){return prepared},
  });
  capture(controller, (nextQuery) => {query = nextQuery});
 </script>
 <span data-status>{controller.status}</span>
 {#each controller.docs as doc}<span>{doc.id}</span>{/each}
`;
async function fixture(
	client: AdminClient,
	options: {
		query?: string;
		prepared?: AdminCollectionListDataV1;
		counts?: boolean;
		canLoad?: boolean;
		locale?: string;
		trashOnly?: boolean;
		slug?: string;
	} = {}
) {
	let controller!: CollectionListController;
	let changeQuery!: (query: string) => void;
	const notifications = { success: vi.fn(), error: vi.fn() };
	const props = {
		client,
		notifications,
		slug: "posts",
		capture: (value: CollectionListController, navigate: (query: string) => void) => {
			controller = value;
			changeQuery = navigate;
		},
		query: "page=1",
		locale: undefined as string | undefined,
		trashOnly: false,
		prepared: undefined as AdminCollectionListDataV1 | undefined,
		counts: false,
		canLoad: true,
		collection: {
			id: "posts",
			slug: "posts",
			labels: { singular: "Post", plural: "Posts" },
			fields: [],
			admin: {},
			capabilities: { auth: false, upload: false, versions: false, trash: true, locking: false },
		} satisfies SchemaCollection,
		...options,
	};
	const screen = await render(ControllerOwner, props);
	if (props.canLoad) await expect.poll(() => controller.status).toBe("ready");
	return { controller, changeQuery, notifications, props, screen };
}

it("empties trash in the displayed content locale and refreshes the list", async () => {
	const network = clientFixture();
	const { controller } = await fixture(network.client, { locale: "fr", trashOnly: true });
	controller.togglePage(true);
	await controller.emptyTrash();
	expect(network.emptyTrash).toHaveBeenCalledWith("posts", { locale: "fr" });
	expect(controller.selectedIDs.size).toBe(0);
	expect(network.pageRead).toHaveBeenCalledTimes(2);
	expect(controller.bulkPending).toBe(false);
});

it("adopts explicit seeds before rendering, including retained pages and errors, without SDK reads", async () => {
	const network = clientFixture();
	const { controller, props, screen } = await fixture(network.client, { prepared: data() });
	expect(network.read).not.toHaveBeenCalled();
	controller.togglePage(true);
	await screen.rerender({ ...props, query: "page=2", prepared: data("two") });
	expect(controller.docs[0]?.id).toBe("two");
	expect([...controller.selectedIDs]).toEqual(["one"]);
	await screen.rerender({
		...props,
		query: "page=3",
		prepared: {
			counts: {},
			query: { trash: false },
			page: { error: { code: "internal", status: 503, message: "List failed", issues: [] } },
		},
	});
	expect(controller.status).toBe("failed");
	expect(controller.docs[0]?.id).toBe("two");
	expect(controller.canCreate).toBe(false);
	expect(network.read).not.toHaveBeenCalled();
	network.pageRead.mockResolvedValue(data("retry"));
	await controller.retry();
	expect(controller.docs[0]?.id).toBe("retry");
	expect(network.read).toHaveBeenCalledTimes(1);
});

it("retains selection for presentation changes, but clears selection for changed predicates", async () => {
	const network = clientFixture();
	const { controller, props, screen } = await fixture(network.client, { counts: true });
	await expect.poll(() => controller.selectionControlsReady).toBe(true);
	controller.togglePage(true);
	for (const query of [
		"page=2",
		"page=2&sort=-title",
		"page=2&view=hierarchy",
		"page=2&columns=author",
	]) {
		const reads = network.pageRead.mock.calls.length;
		Object.assign(props, { query });
		await screen.rerender(props);
		await expect.poll(() => network.pageRead.mock.calls.length).toBe(reads + 1);
		await expect.poll(() => controller.selectionControlsReady).toBe(true);
		expect(controller.selectedIDs.has("one")).toBe(true);
		expect(network.countRead).not.toHaveBeenCalled();
	}
	for (const query of [
		'filters=[{"field":"title","operator":"equals","value":"One"}]',
		"q=One",
		"status=draft",
		"locale=fr",
		"trash=true",
	]) {
		controller.togglePage(true);
		network.pageRead.mockResolvedValue({
			...data(),
			query: { trash: false, where: { title: { equals: query } } },
		});
		Object.assign(props, { query });
		await screen.rerender(props);
		await expect.poll(() => controller.selectedIDs.size).toBe(0);
		await expect.poll(() => controller.selectionControlsReady).toBe(true);
	}
});

it("retains selection when different URLs resolve to the same predicates", async () => {
	const network = clientFixture();
	const { controller, props, screen } = await fixture(network.client, {
		counts: true,
		query: "q=Ada",
	});
	await expect.poll(() => controller.selectionControlsReady).toBe(true);
	controller.togglePage(true);
	await screen.rerender({ ...props, query: "q=+Ada+" });
	await expect.poll(() => network.pageRead.mock.calls.length).toBe(2);
	await expect.poll(() => controller.selectionControlsReady).toBe(true);
	expect(controller.selectedIDs.has("one")).toBe(true);
	expect(network.countRead).not.toHaveBeenCalled();
});

it("refreshes page when the live collection schema changes without changing the URL", async () => {
	const network = clientFixture();
	const { controller, props, screen } = await fixture(network.client, { counts: true });
	await expect.poll(() => controller.selectionControlsReady).toBe(true);
	controller.togglePage(true);
	await screen.rerender({
		...props,
		collection: { ...props.collection, admin: { ...props.collection.admin, useAsTitle: "name" } },
	});
	await expect.poll(() => network.pageRead.mock.calls.length).toBe(2);
	expect(network.countRead).not.toHaveBeenCalled();
	expect(controller.selectedIDs.size).toBe(0);
});

it("debounces search but not pagination", async () => {
	const network = clientFixture();
	const { props, screen } = await fixture(network.client);
	Object.assign(props, { query: "page=2" });
	await screen.rerender(props);
	await expect.poll(() => network.pageRead.mock.calls.length).toBe(2);
	Object.assign(props, { query: "q=Ada" });
	await screen.rerender(props);
	await new Promise((resolve) => setTimeout(resolve, 60));
	expect(network.pageRead).toHaveBeenCalledTimes(2);
	await expect.poll(() => network.pageRead.mock.calls.length).toBe(3);
});

it("does not request unused status counts or surface their errors", async () => {
	const network = clientFixture();
	const initial = data();
	initial.counts = {
		"": { error: { code: "internal", status: 500, message: "unused count", issues: [] } },
	};
	const { controller, props, screen } = await fixture(network.client, { prepared: initial });
	expect(controller.error).toBeUndefined();
	await screen.rerender({ ...props, query: "page=2", prepared: undefined });
	await expect.poll(() => network.pageRead.mock.calls.length).toBe(1);
	expect(network.countRead).not.toHaveBeenCalled();
});

it("uses the server-resolved filter for all-filtered-results selection", async () => {
	const network = clientFixture();
	const { controller } = await fixture(network.client, { prepared: data() });
	controller.togglePage(true);
	await controller.selectAllMatches();
	expect(network.selection).toHaveBeenCalledWith(
		"posts",
		expect.objectContaining({ where: { title: { equals: "server-selected" } } })
	);
});

it("offers bulk fields only when every selected row permits updating them", async () => {
	const network = clientFixture();
	const initial = data();
	const value = initial.page!.value!;
	value.docs.push({ id: "two", title: "two", status: "draft" });
	value.access.documents.two = access(false);
	const { controller } = await fixture(network.client, { prepared: initial });
	controller.togglePage(true);
	expect(controller.canBulkUpdateField("title")).toBe(false);
	controller.toggleDocument("two", false);
	expect(controller.canBulkUpdateField("title")).toBe(true);
});

it("refreshes page after mutations", async () => {
	const network = clientFixture();
	const { controller } = await fixture(network.client, { counts: true, prepared: data() });
	controller.togglePage(true);
	await controller.bulkUpdate(controller.captureBulkUpdateTarget(), {
		title: "Updated",
		status: "published",
	});
	expect(network.bulkUpdate).toHaveBeenCalledWith(
		"posts",
		["one"],
		{ title: "Updated", status: "published" },
		{ locale: undefined }
	);
	expect(network.pageRead).toHaveBeenCalledTimes(1);
	expect(network.countRead).not.toHaveBeenCalled();
});

it("keeps form targets frozen and rethrows structured field issues", async () => {
	const network = clientFixture();
	const failure = new RiduError({
		code: "validation",
		status: 422,
		message: "Title is invalid",
		issues: [{ path: "title", code: "invalid", message: "Choose another title" }],
	});
	network.bulkUpdate.mockRejectedValueOnce(failure);
	const { controller, notifications } = await fixture(network.client, { prepared: data() });
	controller.togglePage(true);
	const target = controller.captureBulkUpdateTarget();
	controller.clearSelection();

	await expect(
		controller.bulkUpdate(target, { title: "Updated", status: "published" })
	).rejects.toBe(failure);
	expect(network.bulkUpdate).toHaveBeenCalledWith(
		"posts",
		["one"],
		{ title: "Updated", status: "published" },
		{ locale: undefined }
	);
	expect(notifications.error).toHaveBeenCalledTimes(1);
});

for (const change of ["resource", "locale", "schema", "access"] as const) {
	it(`refuses a frozen bulk update target after its ${change} owner changes`, async () => {
		const network = clientFixture();
		const { controller, props, screen } = await fixture(network.client, { prepared: data() });
		controller.togglePage(true);
		const target = controller.captureBulkUpdateTarget();

		await screen.rerender({
			...props,
			prepared: data("next"),
			...(change === "resource" ? { slug: "pages" } : {}),
			...(change === "locale" ? { locale: "fr" } : {}),
			...(change === "schema" ? { collection: { ...props.collection } } : {}),
			...(change === "access" ? { query: "page=2" } : {}),
		});

		expect(await controller.bulkUpdate(target, { title: "Stale update" })).toBe(false);
		expect(network.bulkUpdate).not.toHaveBeenCalled();
	});
}

for (const action of ["bulkUpdate", "emptyTrash"] as const) {
	for (const outcome of ["success", "failure"] as const) {
		it(`ignores an old ${action} ${outcome} after returning to the same route while a new mutation is pending`, async () => {
			const network = clientFixture();
			const previous = Promise.withResolvers<never[]>();
			const current = Promise.withResolvers<never[]>();
			network[action]
				.mockImplementationOnce(() => previous.promise)
				.mockImplementationOnce(() => current.promise);
			const { controller, notifications, props, screen } = await fixture(network.client, {
				prepared: data(),
			});
			controller.togglePage(true);
			const oldOperation =
				action === "bulkUpdate"
					? controller.bulkUpdate(controller.captureBulkUpdateTarget(), { title: "Old update" })
					: controller.emptyTrash();
			expect(controller.bulkPending).toBe(true);
			// A destination can start loading preferences before it can load its list.
			await screen.rerender({ ...props, slug: "pages", canLoad: false });
			expect(controller.bulkPending).toBe(false);
			await screen.rerender({ ...props, prepared: data("current") });
			controller.togglePage(true);
			const newOperation =
				action === "bulkUpdate"
					? controller.bulkUpdate(controller.captureBulkUpdateTarget(), { title: "Current update" })
					: controller.emptyTrash();
			expect(controller.bulkPending).toBe(true);
			if (outcome === "success") previous.resolve([]);
			else previous.reject(new Error("Old operation failed"));
			const oldResult = await oldOperation;
			if (action === "bulkUpdate") expect(oldResult).toBe(false);
			expect(controller.docs.map((doc) => doc.id)).toEqual(["current"]);
			expect(controller.selectedIDs.has("current")).toBe(true);
			expect(controller.bulkPending).toBe(true);
			expect(notifications.success).not.toHaveBeenCalled();
			expect(notifications.error).not.toHaveBeenCalled();
			expect(network.read).not.toHaveBeenCalled();

			current.resolve([]);
			const currentResult = await newOperation;
			if (action === "bulkUpdate") expect(currentResult).toBe(true);
			expect(controller.selectedIDs.size).toBe(0);
			expect(controller.bulkPending).toBe(false);
			expect(notifications.success).toHaveBeenCalledTimes(1);
			expect(network.read).toHaveBeenCalledTimes(1);
		});

		it(`ignores ${action} ${outcome} after its list owner unmounts`, async () => {
			const network = clientFixture();
			const pending = Promise.withResolvers<never[]>();
			network[action].mockImplementationOnce(() => pending.promise);
			const { controller, notifications, screen } = await fixture(network.client, {
				prepared: data(),
			});
			controller.togglePage(true);
			const operation =
				action === "bulkUpdate"
					? controller.bulkUpdate(controller.captureBulkUpdateTarget(), { title: "Update" })
					: controller.emptyTrash();
			await screen.unmount();
			if (outcome === "success") pending.resolve([]);
			else pending.reject(new Error("Disposed operation failed"));
			const result = await operation;
			if (action === "bulkUpdate") expect(result).toBe(false);
			expect(controller.bulkPending).toBe(false);
			expect(notifications.success).not.toHaveBeenCalled();
			expect(notifications.error).not.toHaveBeenCalled();
			await controller.retry();
			if (action === "bulkUpdate")
				await controller.bulkUpdate(controller.captureBulkUpdateTarget(), {
					title: "Late callback",
				});
			else await controller.emptyTrash();
			expect(network.read).not.toHaveBeenCalled();
			expect(network[action]).toHaveBeenCalledTimes(1);
		});
	}
}

for (const change of ["collection", "locale", "query", "trash", "schema"] as const) {
	it(`keeps the next list selection when a bulk mutation completes after a ${change} change`, async () => {
		const network = clientFixture();
		const pending = Promise.withResolvers<never[]>();
		network.bulkUpdate.mockImplementationOnce(() => pending.promise);
		const { controller, notifications, props, screen } = await fixture(network.client, {
			prepared: data(),
		});
		controller.togglePage(true);
		const operation = controller.bulkUpdate(controller.captureBulkUpdateTarget(), {
			title: "Update",
		});
		await screen.rerender({
			...props,
			prepared: data("next"),
			...(change === "collection" ? { slug: "pages" } : {}),
			...(change === "locale" ? { locale: "fr" } : {}),
			...(change === "query" ? { query: "page=2" } : {}),
			...(change === "trash" ? { trashOnly: true } : {}),
			...(change === "schema" ? { collection: { ...props.collection } } : {}),
		});
		controller.togglePage(true);
		expect(controller.bulkPending).toBe(false);
		pending.resolve([]);
		expect(await operation).toBe(false);
		expect(controller.docs.map((doc) => doc.id)).toEqual(["next"]);
		expect(controller.selectedIDs.has("next")).toBe(true);
		expect(notifications.success).not.toHaveBeenCalled();
		expect(network.read).not.toHaveBeenCalled();
	});
}

it("does not report bulk success to its caller when the route changes during the refresh", async () => {
	const network = clientFixture();
	const pending = Promise.withResolvers<AdminCollectionListDataV1>();
	const { controller, props, screen } = await fixture(network.client, { prepared: data() });
	network.pageRead.mockImplementationOnce(() => pending.promise);
	controller.togglePage(true);
	const operation = controller.bulkUpdate(controller.captureBulkUpdateTarget(), {
		title: "Update",
	});
	await expect.poll(() => network.read.mock.calls.length).toBe(1);
	const signal = network.read.mock.calls[0]?.[3]?.signal;
	await screen.rerender({ ...props, query: "page=2", canLoad: false, prepared: undefined });
	expect(signal?.aborted).toBe(true);
	pending.resolve(data("old refreshed page"));
	expect(await operation).toBe(false);
	expect(controller.docs.map((doc) => doc.id)).toEqual(["one"]);
	await screen.rerender({ ...props, query: "page=2", prepared: data("next") });
	controller.togglePage(true);
	expect(controller.docs.map((doc) => doc.id)).toEqual(["next"]);
	expect(controller.selectedIDs.has("next")).toBe(true);
});

it("checks live route inputs when a mutation resolves before route effects flush", async () => {
	const network = clientFixture();
	const pending = Promise.withResolvers<never[]>();
	network.bulkUpdate.mockImplementationOnce(() => pending.promise);
	const { controller, changeQuery, notifications } = await fixture(network.client, {
		prepared: data(),
	});
	controller.togglePage(true);
	const operation = controller.bulkUpdate(controller.captureBulkUpdateTarget(), {
		title: "Update",
	});
	// Queue the completion before Svelte's route synchronization, then replace the live input.
	pending.resolve([]);
	changeQuery("page=2");
	expect(await operation).toBe(false);
	expect(controller.selectedIDs.has("one")).toBe(true);
	expect(notifications.success).not.toHaveBeenCalled();
	expect(notifications.error).not.toHaveBeenCalled();
});

it("ignores superseded page responses and aborts the request", async () => {
	const network = clientFixture();
	let resolve!: (value: AdminCollectionListDataV1) => void;
	network.pageRead.mockImplementationOnce(() => new Promise((complete) => (resolve = complete)));
	const { controller, props, screen } = await fixture(network.client, { canLoad: false });
	await screen.rerender({ ...props, canLoad: true });
	await expect.poll(() => network.pageRead.mock.calls.length).toBe(1);
	const signal = network.read.mock.calls[0]?.[3]?.signal;
	await screen.rerender({ ...props, canLoad: true, query: "page=2" });
	await expect.poll(() => controller.status).toBe("ready");
	expect(signal?.aborted).toBe(true);
	resolve(data("stale"));
	await new Promise((complete) => setTimeout(complete, 0));
	expect(controller.docs[0]?.id).toBe("one");
	await screen.unmount();
});
