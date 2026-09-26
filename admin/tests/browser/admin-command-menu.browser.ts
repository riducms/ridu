import { createMemoryRouter } from "@hvniel/svelte-router";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { SCHEMA_MANIFEST_VERSION, type SchemaCollection } from "@riducms/protocol";
import { tick } from "svelte";
import { expect, it, vi } from "vitest";
import { page } from "vitest/browser";
import { render } from "vitest-browser-svelte";

import { createAdminClient, type AdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const Provider = svelte`
	<script>
		import { RouterProvider } from "@hvniel/svelte-router";
		import { setAdminI18n } from "@riducms/plugin";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		let { runtime, router } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<RouterProvider {router} />
`;

const CommandRoute = svelte`
	<script>
		import { getAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import AdminCommandMenu from "../../src/features/navigation/admin-command-menu.svelte";
		const runtime = getAdminRuntime();
	</script>
	<AdminCommandMenu open collections={runtime.manifest?.collections ?? []} />
`;

type DocumentPage = Awaited<ReturnType<AdminClient["list"]>>;

const posts: SchemaCollection = {
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
	],
};
const pages: SchemaCollection = {
	...posts,
	id: "pages",
	slug: "pages",
	labels: { singular: "Page", plural: "Pages" },
};

const listedDocument = { id: "one", title: "Previously available document" };
const documentPage: DocumentPage = {
	docs: [listedDocument],
	pagination: {
		page: 1,
		limit: 6,
		totalDocs: 1,
		totalPages: 1,
		hasNextPage: false,
		hasPrevPage: false,
	},
	access: {
		collection: {
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
				deletePermanent: false,
				selectAll: true,
			},
			fields: {},
		},
		documents: {},
	},
};

async function mountMenu(response: Promise<DocumentPage>) {
	const client = createAdminClient();
	// Ignore cancellation at the transport seam so a late response must also be rejected.
	const list = vi.spyOn(client, "list").mockImplementation(() => response);
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Command menu test" },
		collections: [posts],
		plugins: [],
	};
	const router = createMemoryRouter([{ path: "*", Component: CommandRoute }]);
	const screen = await render(Provider, { runtime, router });
	return {
		list,
		setCollections(collections: SchemaCollection[]) {
			runtime.manifest = { ...runtime.manifest!, collections };
		},
		removeCollections() {
			runtime.manifest = { ...runtime.manifest!, collections: [] };
		},
		async dispose() {
			await screen.unmount();
			router.dispose();
			runtime.dispose();
			list.mockRestore();
		},
	};
}

it("removes loaded document commands when an open menu loses all collections", async () => {
	const fixture = await mountMenu(Promise.resolve(documentPage));
	try {
		const result = page.getByRole("option", { name: /Previously available document/ });
		await expect.element(result).toBeVisible();

		fixture.removeCollections();

		await expect.element(page.getByRole("dialog", { name: "Navigate Ridu" })).toBeVisible();
		await expect.element(result).not.toBeInTheDocument();
		await expect
			.element(page.getByText("Loading documents…", { exact: true }))
			.not.toBeInTheDocument();
		await expect.element(page.getByText("Nothing matches.", { exact: true })).toBeVisible();
		expect(fixture.list).toHaveBeenCalledOnce();
	} finally {
		await fixture.dispose();
	}
});

it("hides the previous collection's documents while loading a different collection", async () => {
	const nextPage = Promise.withResolvers<DocumentPage>();
	const fixture = await mountMenu(Promise.resolve(documentPage));
	try {
		const oldResult = page.getByRole("option", { name: /Previously available document/ });
		await expect.element(oldResult).toBeVisible();

		fixture.list.mockImplementation(() => nextPage.promise);
		fixture.setCollections([pages]);

		await expect.element(oldResult).not.toBeInTheDocument();
		await expect.element(page.getByText("Loading documents…", { exact: true })).toBeVisible();
		const nextDocument = { id: "page-one", title: "New collection document" };
		nextPage.resolve({ ...documentPage, docs: [nextDocument] });
		await expect
			.element(page.getByRole("option", { name: /New collection document/ }))
			.toBeVisible();
	} finally {
		nextPage.resolve(documentPage);
		await fixture.dispose();
	}
});

it("clears loading and ignores a late response when an open menu loses all collections", async () => {
	const response = Promise.withResolvers<DocumentPage>();
	const fixture = await mountMenu(response.promise);
	try {
		const loading = page.getByText("Loading documents…", { exact: true });
		await expect.element(loading).toBeVisible();
		const signal = fixture.list.mock.calls[0]?.[1]?.signal;
		expect(signal?.aborted).toBe(false);

		fixture.removeCollections();

		await expect.element(page.getByRole("dialog", { name: "Navigate Ridu" })).toBeVisible();
		await expect.element(loading).not.toBeInTheDocument();
		expect(signal?.aborted).toBe(true);

		response.resolve(documentPage);
		await response.promise;
		// Drain the aggregate request and its DOM update before checking the abandoned result.
		await tick();

		await expect
			.element(page.getByRole("option", { name: /Previously available document/ }))
			.not.toBeInTheDocument();
		await expect.element(loading).not.toBeInTheDocument();
		await expect.element(page.getByText("Nothing matches.", { exact: true })).toBeVisible();
		expect(fixture.list).toHaveBeenCalledOnce();
	} finally {
		await fixture.dispose();
	}
});
