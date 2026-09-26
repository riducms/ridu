import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import {
	SCHEMA_MANIFEST_VERSION,
	type AdminCollectionListDataV1,
	type SchemaCollection,
} from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import type { AdminClient } from "@admin/core/api/admin-client";
import type { CollectionList } from "@admin/features/collections/collection-list.svelte";

const Consumer = svelte`
 <script>
  import { getCollectionList } from '../../src/features/collections/collection-list.svelte';
  const list = getCollectionList();
  const { documentHref, setPageSize, setColumns, saveView, applySavedView } = list;
 </script>
 <p data-testid="identity">{list.slug}:{list.contentLocale}:{list.searchLabel}</p>
 <p data-testid="limit">{list.view.limit}</p>
 <p data-testid="search">{list.view.q || 'No search'}</p>
 <p data-testid="sort">{list.view.sort}</p>
 <p data-testid="documents">{list.controller.docs.map((doc) => doc.id).join(',')}</p>
 <a href={documentHref({ id: 'one' })}>Document</a>
 <button onclick={() => setPageSize(25)}>25 per page</button>
 <button onclick={() => setColumns([{path: 'title', active: false}, {path: 'id', active: true}])}>ID column</button>
 <button onclick={() => saveView('Focused')}>Save view</button>
 <button onclick={() => applySavedView({name: 'Preset', q: 'saved', status: '', folder: '', view: 'list', filters: [], sort: '-title', columns: [{path:'title',active:true}], limit: 50})}>Apply view</button>
`;
const Owner = svelte`
 <script>
  import { CollectionList, setCollectionList } from '../../src/features/collections/collection-list.svelte';
  import { CollectionListQuery } from '../../src/features/collections/collection-list-query';
  import { NotificationCenter } from '../../src/core/notifications/notification-center.svelte';
  let { runtime, Consumer, capture, slug = 'posts', search = 'locale=en', seed, trashOnly = false } = $props();
  const query = new CollectionListQuery({get current() {return new URLSearchParams(search);}}, (next) => { search = next.toString(); }, () => undefined);
  const list = new CollectionList({
   runtime, query, notifications: new NotificationCenter(),
   get slug() {return slug;}, get trashOnly() {return trashOnly;},
   navigationIdle: true, prepared: () => seed,
  });
  setCollectionList(list);
  capture(list);
 </script>
 <section aria-label={runtime.session.id}><Consumer /></section>
`;

function collection(slug: string, field: string): SchemaCollection {
	return {
		id: slug,
		slug,
		labels: { singular: slug, plural: slug },
		admin: { useAsTitle: field, defaultColumns: [field] },
		capabilities: { auth: false, upload: false, versions: false, trash: true, locking: false },
		fields: [
			{
				id: field,
				name: field,
				path: field,
				type: "text",
				category: "scalar",
				required: false,
				unique: false,
				admin: { label: field },
				text: {},
			},
		],
	};
}
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
		restoreDeleted: true,
		deletePermanent: true,
		selectAll: true,
	},
	fields: {},
};
function data(id: string, field = "title"): AdminCollectionListDataV1 {
	return {
		query: { trash: false },
		counts: {},
		page: {
			value: {
				docs: [{ id, [field]: id }],
				access: { collection: access, documents: { [id]: access } },
				pagination: {
					page: 1,
					limit: 10,
					totalDocs: 1,
					totalPages: 1,
					hasNextPage: false,
					hasPrevPage: false,
				},
			},
		},
		preferences: {
			workspace: { value: { columns: [{ path: field, active: true }], limit: 10 } },
			presets: { value: [] },
		},
	};
}
function fixture(id: string) {
	const read = vi.fn(async () => data("loaded"));
	const preference = vi.fn(async () => []);
	const setPreference = vi.fn(async (_key: string, value: unknown) => value);
	const runtime = new AdminRuntime({
		adminCollectionList: read,
		preference,
		setPreference,
	} as unknown as AdminClient);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: {
			name: "List context",
			localization: {
				defaultLocale: "en",
				fallback: false,
				locales: [
					{ code: "en", label: "English" },
					{ code: "fr", label: "French" },
				],
			},
		},
		collections: [collection("posts", "title"), collection("pages", "name")],
		plugins: [],
	};
	runtime.session = { id, collection: "users", user: { id }, expiresAt: "2099-01-01T00:00:00Z" };
	const capture = vi.fn<(list: CollectionList) => void>();
	return {
		runtime,
		read,
		preference,
		setPreference,
		capture,
		props: { runtime, Consumer, capture, seed: data("seed") },
	};
}

it("keeps context identity while collection, locale and prepared data change without duplicate reads", async () => {
	const f = fixture("list-live");
	const screen = await render(Owner, f.props);
	try {
		const list = f.capture.mock.calls[0]![0];
		await expect
			.element(screen.getByTestId("identity"))
			.toHaveTextContent("posts:en:Search by title");
		await screen.rerender({ slug: "pages", search: "locale=fr", seed: data("page-fr", "name") });
		await expect
			.element(screen.getByTestId("identity"))
			.toHaveTextContent("pages:fr:Search by name");
		await expect.element(screen.getByTestId("documents")).toHaveTextContent("page-fr");
		await expect
			.element(screen.getByRole("link"))
			.toHaveAttribute("href", "/collections/pages/one?locale=fr");
		expect(f.capture).toHaveBeenCalledTimes(1);
		expect(list.collection?.slug).toBe("pages");
		expect(f.read).not.toHaveBeenCalled();
		expect(f.preference).not.toHaveBeenCalled();
		await screen.getByRole("button", { name: "25 per page" }).click();
		await expect.poll(() => f.setPreference.mock.calls[0]?.[0]).toBe("collection:pages:workspace");
		expect(f.setPreference.mock.calls[0]?.[1]).toMatchObject({
			limit: 25,
			columns: expect.arrayContaining([{ path: "name", active: true }]),
		});
	} finally {
		await screen.unmount();
		f.runtime.dispose();
	}
});

it("coordinates column, page-size and saved-view writes without sharing state across list providers", async () => {
	const first = fixture("list-isolated-a");
	const second = fixture("list-isolated-b");
	const a = await render(Owner, first.props);
	const b = await render(Owner, second.props);
	const aScope = a.getByRole("region", { name: "list-isolated-a" });
	const bScope = b.getByRole("region", { name: "list-isolated-b" });
	try {
		await aScope.getByRole("button", { name: "25 per page" }).click();
		await aScope.getByRole("button", { name: "ID column" }).click();
		await expect.poll(() => first.setPreference.mock.calls.length).toBe(2);
		expect(first.setPreference.mock.calls[1]?.[1]).toEqual({
			limit: 25,
			columns: [
				{ path: "title", active: false },
				{ path: "id", active: true },
			],
		});
		await expect.element(aScope.getByTestId("limit")).toHaveTextContent("25");
		await expect.element(bScope.getByTestId("limit")).toHaveTextContent("10");
		expect(second.setPreference).not.toHaveBeenCalled();
		await aScope.getByRole("button", { name: "Save view", exact: true }).click();
		await expect.poll(() => first.setPreference.mock.calls.length).toBe(3);
		expect(first.setPreference.mock.calls[2]).toEqual([
			"collection:posts:presets",
			[
				expect.objectContaining({
					name: "Focused",
					limit: 25,
					columns: expect.arrayContaining([
						{ path: "title", active: false },
						{ path: "id", active: true },
					]),
				}),
			],
		]);
		await aScope.getByRole("button", { name: "Apply view", exact: true }).click();
		await expect.poll(() => first.setPreference.mock.calls.length).toBe(4);
		expect(first.setPreference.mock.calls[3]).toEqual([
			"collection:posts:workspace",
			{ limit: 50, columns: [{ path: "title", active: true }] },
		]);
		await expect.element(aScope.getByTestId("search")).toHaveTextContent("saved");
		await expect.element(aScope.getByTestId("sort")).toHaveTextContent("-title");
		await expect.element(aScope.getByTestId("limit")).toHaveTextContent("50");
		await expect.element(bScope.getByTestId("search")).toHaveTextContent("No search");
	} finally {
		await a.unmount();
		await b.unmount();
		first.runtime.dispose();
		second.runtime.dispose();
	}
});

it("aborts owned list and preference reads and unregisters the locale blocker on unmount", async () => {
	const f = fixture("list-cleanup");
	const pendingList = Promise.withResolvers<AdminCollectionListDataV1>();
	let listSignal: AbortSignal | undefined;
	f.runtime.client.adminCollectionList = vi.fn((_slug, _query, _part, options) => {
		listSignal = options?.signal;
		return pendingList.promise;
	});
	const pendingPreferences = Promise.withResolvers<unknown>();
	const preferenceSignals: AbortSignal[] = [];
	f.runtime.client.preference = vi.fn((_key, options) => {
		if (options?.signal) preferenceSignals.push(options.signal);
		return pendingPreferences.promise;
	}) as AdminClient["preference"];
	const unregister = vi.fn();
	const originalRegister = f.runtime.registerContentLocaleBlocker.bind(f.runtime);
	vi.spyOn(f.runtime, "registerContentLocaleBlocker").mockImplementation((blocked) => {
		const cleanup = originalRegister(blocked);
		return () => {
			unregister();
			cleanup();
		};
	});
	const screen = await render(Owner, f.props);
	const list = f.capture.mock.calls[0]![0];
	try {
		await screen.rerender({ seed: undefined, search: "page=2&locale=en" });
		await expect.poll(() => listSignal !== undefined).toBe(true);
		await screen.rerender({ slug: "pages" });
		await expect.poll(() => preferenceSignals.length).toBe(2);
		list.controller.bulkPending = true;
		expect(f.runtime.contentLocaleSwitchBlocked).toBe(true);
	} finally {
		await screen.unmount();
	}
	expect(listSignal?.aborted).toBe(true);
	expect(preferenceSignals.every((signal) => signal.aborted)).toBe(true);
	expect(unregister).toHaveBeenCalledTimes(1);
	expect(f.runtime.contentLocaleSwitchBlocked).toBe(false);
	pendingList.resolve(data("late"));
	pendingPreferences.resolve([]);
	await Promise.all([pendingList.promise, pendingPreferences.promise]);
	expect(list.controller.docs.map((doc) => doc.id)).not.toContain("late");
	f.runtime.dispose();
});
