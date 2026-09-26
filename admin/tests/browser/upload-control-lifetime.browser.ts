import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import {
	SCHEMA_MANIFEST_VERSION,
	type AccessCapabilitiesEnvelope,
	type SchemaCollection,
} from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { page } from "vitest/browser";
import { render } from "vitest-browser-svelte";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { DocumentController } from "@admin/features/documents/document-controller.svelte";

const UploadHarness = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import UploadControl from "../../src/features/uploads/upload-control.svelte";
		let { Controller, runtime, notifications, prepared, settings, ready, editable = true, visible = true } = $props();
		setAdminI18n(runtime.i18n);
		// svelte-ignore state_referenced_locally
		const controller = new Controller({
			runtime, notifications, prepared,
			navigate: () => {}, slug: "media", documentID: "one", global: false,
			locale: undefined, editable: true,
		});
		ready(controller);
	</script>
	<output data-testid="save-status">{controller.canSave ? "ready" : "blocked"}</output>
	{#if visible}
		<UploadControl draft={controller.upload} {settings} {editable} />
	{/if}
`;

const collection: SchemaCollection = {
	id: "media",
	slug: "media",
	labels: { singular: "Media", plural: "Media" },
	admin: {},
	capabilities: { auth: false, upload: true, versions: false, trash: false, locking: false },
	uploadSettings: { maxFileSize: 1_000_000, mimeTypes: ["image/png"], private: false },
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

async function uploadEditor() {
	const canvas = document.createElement("canvas");
	canvas.width = 100;
	canvas.height = 80;
	const image = await new Promise<Blob>((resolve, reject) => {
		canvas.toBlob((blob) => {
			if (blob) resolve(blob);
			else reject(new Error("Could not create the upload image fixture."));
		}, "image/png");
	});
	const url = URL.createObjectURL(image);
	const mediaDocument: AdminDocument = {
		id: "one",
		title: "Original title",
		filename: "one.png",
		mimeType: "image/png",
		url,
		width: 100,
		height: 80,
		filesize: image.size,
		sizes: {
			thumbnail: {
				url,
				filename: "thumbnail.png",
				mimeType: "image/png",
				filesize: image.size,
				width: 50,
				height: 40,
			},
		},
		_revision: 1,
	};
	const updateUpload = vi.fn(async () => mediaDocument);
	const client = {
		readUploadSource: vi.fn(async () => image),
		updateUpload,
		collectionAccess: vi.fn(async () => access),
	} as unknown as AdminClient;
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Upload lifetime" },
		collections: [collection],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	const notifications = new NotificationCenter();
	let controller!: DocumentController;
	const screen = await render(UploadHarness, {
		Controller: DocumentController,
		runtime,
		notifications,
		prepared: { document: { value: mediaDocument }, access: { value: access } },
		settings: collection.uploadSettings,
		ready: (value: DocumentController) => {
			controller = value;
		},
	});
	return {
		screen,
		controller,
		updateUpload,
		async dispose() {
			await screen.unmount();
			runtime.dispose();
			notifications.destroy();
			URL.revokeObjectURL(url);
		},
	};
}

async function openImageEditor(fixture: Awaited<ReturnType<typeof uploadEditor>>) {
	await fixture.screen.getByRole("button", { name: "Edit Image", exact: true }).click();
	await expect
		.element(page.getByRole("button", { name: "Apply Changes", exact: true }))
		.toBeVisible();
}

it("keeps every crop resize handle inside the clipping canvas at full image size", async () => {
	const fixture = await uploadEditor();
	try {
		await openImageEditor(fixture);
		await expect
			.poll(() => {
				const canvas = document.querySelector<HTMLElement>(".ridu-crop-canvas");
				const selection = canvas?.querySelector<HTMLElement>(".ridu-crop-selection");
				if (!canvas || !selection || selection.getBoundingClientRect().width === 0) return false;
				const bounds = canvas.getBoundingClientRect();
				const handles = selection.querySelectorAll<HTMLElement>(
					'cropper-handle[action$="-resize"]'
				);
				return (
					handles.length === 8 &&
					Array.from(handles).every((handle) => {
						const rect = handle.getBoundingClientRect();
						return (
							rect.left >= bounds.left - 0.5 &&
							rect.top >= bounds.top - 0.5 &&
							rect.right <= bounds.right + 0.5 &&
							rect.bottom <= bounds.bottom + 0.5 &&
							document
								.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2)
								?.getAttribute("action") === handle.getAttribute("action")
						);
					})
				);
			})
			.toBe(true);
	} finally {
		await fixture.dispose();
	}
});

it("revokes an open image editor and its pending Apply action when editing becomes unavailable", async () => {
	const fixture = await uploadEditor();
	const applyImage = vi.spyOn(fixture.controller.upload, "applyImage");
	try {
		await openImageEditor(fixture);
		await page.getByRole("spinbutton", { name: "Width (px)", exact: true }).fill("40");
		const apply = page.getByRole("button", { name: "Apply Changes", exact: true }).element();
		expect(apply).toBeInstanceOf(HTMLButtonElement);
		const revoked = fixture.screen.rerender({ editable: false });
		// A queued click may still target the old DOM before permission-change cleanup runs.
		(apply as HTMLButtonElement).click();
		await revoked;

		await expect.element(page.getByRole("dialog")).not.toBeInTheDocument();
		expect(applyImage).not.toHaveBeenCalled();
		expect(fixture.controller.upload.image).toBeUndefined();
		expect(fixture.controller.upload.editingImage).toBe(false);
	} finally {
		applyImage.mockRestore();
		await fixture.dispose();
	}
});

it("keeps the read-only sizes preview open when editing becomes unavailable", async () => {
	const fixture = await uploadEditor();
	try {
		await fixture.screen.getByRole("button", { name: "Preview Sizes", exact: true }).click();
		await expect.element(page.getByRole("dialog")).toBeVisible();
		await fixture.screen.rerender({ editable: false });
		await expect.element(page.getByRole("dialog")).toBeVisible();
		await page.getByRole("button", { name: /thumbnail thumbnail\.png/ }).click();
		await expect.element(page.getByRole("region", { name: "thumbnail" })).toBeVisible();
		await page.getByRole("button", { name: "Close", exact: true }).click();
		await expect.element(page.getByRole("dialog")).not.toBeInTheDocument();
	} finally {
		await fixture.dispose();
	}
});

it.each(["cancel", "apply", "unmount"] as const)(
	"blocks document saves for the loaded image-editing session until %s",
	async (finish) => {
		const fixture = await uploadEditor();
		try {
			fixture.controller.form.set("title", "Unsaved title");
			await expect.element(fixture.screen.getByTestId("save-status")).toHaveTextContent("ready");
			await openImageEditor(fixture);
			expect(fixture.controller.upload.busy).toBe(false);
			expect(fixture.controller.upload.editingImage).toBe(true);
			expect(fixture.controller.canSave).toBe(false);
			expect(await fixture.controller.save({ silent: true })).toBe(false);
			expect(fixture.updateUpload).not.toHaveBeenCalled();
			await page.getByRole("spinbutton", { name: "Width (px)", exact: true }).fill("40");

			if (finish === "unmount") await fixture.screen.rerender({ visible: false });
			else
				await page
					.getByRole("button", {
						name: finish === "apply" ? "Apply Changes" : "Cancel",
						exact: true,
					})
					.click();

			await expect.element(page.getByRole("dialog")).not.toBeInTheDocument();
			expect(fixture.controller.upload.editingImage).toBe(false);
			expect(fixture.controller.upload.image?.cropWidth).toBe(finish === "apply" ? 40 : undefined);
			await expect.element(fixture.screen.getByTestId("save-status")).toHaveTextContent("ready");
		} finally {
			await fixture.dispose();
		}
	}
);
