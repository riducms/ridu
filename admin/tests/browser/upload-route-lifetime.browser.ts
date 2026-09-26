import { createMemoryRouter } from "@hvniel/svelte-router";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import {
	ADMIN_PREPARED_ROUTE_STATE_VERSION,
	SCHEMA_MANIFEST_VERSION,
	type AccessCapabilitiesEnvelope,
	type AdminPreparedRouteStateV1,
	type SchemaCollection,
} from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { page } from "vitest/browser";
import { render } from "vitest-browser-svelte";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { AdminBootstrapCoordinator } from "@admin/core/bootstrap/admin-bootstrap";
import { preloadAdminModuleGroups } from "@admin/core/bootstrap/admin-route-modules";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import DocumentRoute from "@admin/features/documents/document-route.svelte";

const Provider = svelte`
	<script>
		import { RouterProvider } from "@hvniel/svelte-router";
		import { setAdminI18n } from "@riducms/plugin";
		import { TooltipProvider } from "@riducms/ui";
		import { setAdminBootstrapCoordinator } from "../../src/core/bootstrap/admin-bootstrap";
		import { setNotificationCenter } from "../../src/core/notifications/notification-center.svelte";
		import { createAdminScroll } from "../../src/core/routing/admin-scroll.svelte";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		let { router, runtime, notifications, bootstrap } = $props();
		setAdminBootstrapCoordinator(bootstrap);
		setAdminRuntime(runtime);
		setNotificationCenter(notifications);
		setAdminI18n(runtime.i18n);
		createAdminScroll();
	</script>
	<TooltipProvider><RouterProvider {router} /></TooltipProvider>
`;

it("discards the old upload dialog during retained prepared document navigation without a loading fallback", async () => {
	const canvas = document.createElement("canvas");
	canvas.width = 100;
	canvas.height = 80;
	const image = await new Promise<Blob>((resolve, reject) => {
		canvas.toBlob((blob) => {
			if (blob) resolve(blob);
			else reject(new Error("Could not create the upload image fixture."));
		}, "image/png");
	});
	const imageURL = URL.createObjectURL(image);
	const mediaDocument = (id: string): AdminDocument => ({
		id,
		filename: `${id}.png`,
		mimeType: "image/png",
		url: imageURL,
		width: 100,
		height: 80,
		filesize: image.size,
		_revision: 1,
	});
	const find = vi.fn(async (_slug: string, id: string) => mediaDocument(id));
	const collectionAccess = vi.fn(async () => access);
	const readUploadSource = vi.fn(async () => image);
	const runtime = new AdminRuntime({
		find,
		collectionAccess,
		readUploadSource,
	} as unknown as AdminClient);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Prepared upload navigation" },
		collections: [collection],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	const notifications = new NotificationCenter();
	const bootstrap = new AdminBootstrapCoordinator();
	bootstrap.configure(runtime);
	const stage = (id: string) => {
		const pathname = `/admin/collections/media/${id}`;
		const state: AdminPreparedRouteStateV1 = {
			version: ADMIN_PREPARED_ROUTE_STATE_VERSION,
			outcome: "prepared",
			pathname,
			search: "",
			contextKey: "upload-route-context",
			fingerprint: `upload-${id}`,
			buildId: "upload-route-build",
			moduleGroups: ["document", "upload-preview"],
			navigation: { collectionOperations: {}, globalOperations: {} },
			route: {
				kind: "collection-document",
				document: { document: { value: mediaDocument(id) }, access: { value: access } },
			},
		};
		expect(bootstrap.stage(state, pathname)).toBe(true);
	};
	await preloadAdminModuleGroups(["document", "upload-preview"]);
	stage("one");
	const router = createMemoryRouter(
		[{ path: "/collections/:collection/:document/:view?", Component: DocumentRoute }],
		{ initialEntries: ["/collections/media/one"] }
	);
	const screen = await render(Provider, { router, runtime, notifications, bootstrap });
	const loadingSurfaces: Element[] = [];
	const observer = new MutationObserver((records) => {
		for (const record of records) {
			for (const node of record.addedNodes) {
				if (!(node instanceof Element)) continue;
				if (node.matches('[data-ridu-loading-surface="document"]')) loadingSurfaces.push(node);
				loadingSurfaces.push(...node.querySelectorAll('[data-ridu-loading-surface="document"]'));
			}
		}
	});
	try {
		await expect
			.element(screen.getByRole("heading", { name: "one.png", exact: true }))
			.toBeVisible();
		const viewport = document.querySelector('[data-slot="document-viewport"]');
		expect(viewport).not.toBeNull();
		await screen.getByRole("button", { name: "Edit Image", exact: true }).click();
		await expect.element(page.getByRole("dialog", { name: "Editing one.png" })).toBeVisible();
		await page.getByRole("spinbutton", { name: "Width (px)", exact: true }).fill("40");
		expect(readUploadSource).toHaveBeenCalledOnce();

		observer.observe(document.body, { childList: true, subtree: true });
		stage("two");
		await router.navigate("/collections/media/two");

		await expect
			.element(screen.getByRole("heading", { name: "two.png", exact: true }))
			.toBeVisible();
		await expect.element(page.getByRole("dialog")).not.toBeInTheDocument();
		expect(document.querySelector('[data-slot="document-viewport"]')).toBe(viewport);
		expect(loadingSurfaces).toEqual([]);
		expect(find).not.toHaveBeenCalled();
		expect(collectionAccess).not.toHaveBeenCalled();

		await screen.getByRole("button", { name: "Edit Image", exact: true }).click();
		await expect.element(page.getByRole("dialog", { name: "Editing two.png" })).toBeVisible();
		await expect
			.element(page.getByRole("spinbutton", { name: "Width (px)", exact: true }))
			.toHaveValue(100);
	} finally {
		observer.disconnect();
		await screen.unmount();
		router.dispose();
		runtime.dispose();
		notifications.destroy();
		URL.revokeObjectURL(imageURL);
		delete document.documentElement.dataset.riduContext;
	}
});

const collection: SchemaCollection = {
	id: "media",
	slug: "media",
	labels: { singular: "Media", plural: "Media" },
	admin: {},
	capabilities: { auth: false, upload: true, versions: false, trash: false, locking: false },
	uploadSettings: { maxFileSize: 1_000_000, mimeTypes: ["image/png"], private: false },
	fields: [],
};

const access: AccessCapabilitiesEnvelope = {
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
	fields: {},
};
