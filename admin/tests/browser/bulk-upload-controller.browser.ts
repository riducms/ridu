import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import {
	SCHEMA_MANIFEST_VERSION,
	type AccessCapabilitiesEnvelope,
	type SchemaCollection,
} from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { BulkUploadController } from "@admin/features/uploads/bulk-upload-controller.svelte";

const Harness = svelte`
	<script>
		let { Controller, runtime, notifications, owner, access, ready } = $props();
		// svelte-ignore state_referenced_locally
		const controller = new Controller({
			runtime,
			notifications,
			get slug() { return owner.collection.slug; },
			get locale() { return owner.locale; },
			get collection() { return owner.collection; },
			get routeIdentity() { return owner.routeIdentity; },
			get preparedAccess() { return { value: access }; },
		});
		ready(controller);
	</script>
	<output>{controller.queue.length}:{controller.running ? "running" : "idle"}</output>
`;

const collection = (slug = "media"): SchemaCollection => ({
	id: slug,
	slug,
	labels: { singular: "Asset", plural: "Assets" },
	admin: {},
	capabilities: {
		auth: false,
		upload: true,
		versions: false,
		trash: false,
		locking: false,
	},
	uploadSettings: {
		maxFileSize: 1_000_000,
		mimeTypes: ["image/png", "text/plain"],
		private: false,
	},
	fields: [
		{
			id: "alt",
			name: "alt",
			path: "alt",
			type: "text",
			category: "scalar",
			required: false,
			unique: false,
			admin: { label: "Alt text" },
			text: {},
		},
	],
});

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

const document = (file: File): AdminDocument => ({
	id: `document-${file.name}`,
	filename: file.name,
	mimeType: file.type,
	filesize: file.size,
	url: `/media/${file.name}`,
	_revision: 1,
});

interface Owner {
	collection: SchemaCollection;
	locale: string;
	routeIdentity: string;
}

async function fixture(
	upload: (slug: string, file: File, options: unknown) => Promise<AdminDocument>,
	overrides: Partial<AdminClient> = {}
) {
	const client = {
		upload: vi.fn(upload),
		collectionAccess: vi.fn(async () => access),
		...overrides,
	} as unknown as AdminClient;
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Bulk upload controller" },
		collections: [collection(), collection("other-media")],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	const notifications = new NotificationCenter();
	let controller!: BulkUploadController;
	const owner: Owner = {
		collection: collection(),
		locale: "en",
		routeIdentity: "/collections/media/upload?locale=en",
	};
	const props = {
		Controller: BulkUploadController,
		runtime,
		notifications,
		owner,
		access,
		ready: (value: BulkUploadController) => {
			controller = value;
		},
	};
	const screen = await render(Harness, props);
	await expect.poll(() => controller.canCreate).toBe(true);
	return {
		client,
		controller,
		notifications,
		owner,
		props,
		runtime,
		screen,
		async dispose() {
			await screen.unmount();
			runtime.dispose();
			notifications.destroy();
		},
	};
}

it("retries confirmed failures and never retries an uncertain write", async () => {
	const attempts = new Map<string, number>();
	const failure = new RiduError({
		code: "validation",
		status: 422,
		message: "Metadata rejected",
		issues: [],
	});
	const f = await fixture(async (_slug, file) => {
		const attempt = (attempts.get(file.name) ?? 0) + 1;
		attempts.set(file.name, attempt);
		if (file.name === "confirmed.txt" && attempt === 1) throw failure;
		if (file.name === "uncertain.txt") throw new Error("Connection closed after write");
		return document(file);
	});
	try {
		await f.controller.addFiles([
			new File(["confirmed"], "confirmed.txt", { type: "text/plain" }),
			new File(["uncertain"], "uncertain.txt", { type: "text/plain" }),
		]);
		await f.controller.saveAll();

		expect(f.controller.queue.map((item) => item.status)).toEqual(["failed", "uncertain"]);
		await f.controller.retry(f.controller.queue[0]!);
		expect(f.controller.queue[0]?.status).toBe("complete");
		await f.controller.retry(f.controller.queue[1]!);
		expect(f.controller.queue[1]?.status).toBe("uncertain");
		expect(attempts).toEqual(
			new Map([
				["confirmed.txt", 2],
				["uncertain.txt", 1],
			])
		);
	} finally {
		await f.dispose();
	}
});

it("ignores a stale upload completion after the collection owner changes", async () => {
	let resolveUpload!: (value: AdminDocument) => void;
	const pending = new Promise<AdminDocument>((resolve) => {
		resolveUpload = resolve;
	});
	const f = await fixture(async () => pending);
	const changed = vi.spyOn(f.runtime, "documentsChanged");
	try {
		const file = new File(["stale"], "stale.txt", { type: "text/plain" });
		await f.controller.addFiles([file]);
		const saving = f.controller.saveAll();
		await expect.poll(() => f.controller.queue[0]?.status).toBe("uploading");

		await f.screen.rerender({
			...f.props,
			owner: {
				collection: collection("other-media"),
				locale: "fr",
				routeIdentity: "/collections/other-media/upload?locale=fr",
			},
		});
		await expect.poll(() => f.controller.queue.length).toBe(0);
		resolveUpload(document(file));
		await saving;

		expect(f.controller.queue).toEqual([]);
		expect(f.controller.running).toBe(false);
		expect(changed).not.toHaveBeenCalled();
	} finally {
		changed.mockRestore();
		await f.dispose();
	}
});

it("preserves the selected item after an earlier row is removed and resets on schema changes", async () => {
	const freshAccess: AccessCapabilitiesEnvelope = {
		...access,
		operations: { ...access.operations, create: false },
	};
	const collectionAccess = vi.fn(async () => freshAccess);
	const f = await fixture(async (_slug, file) => document(file), {
		collectionAccess: collectionAccess as AdminClient["collectionAccess"],
	});
	try {
		await f.controller.addFiles([
			new File(["a"], "a.txt", { type: "text/plain" }),
			new File(["b"], "b.txt", { type: "text/plain" }),
			new File(["c"], "c.txt", { type: "text/plain" }),
		]);
		f.controller.select(1);
		f.controller.remove(f.controller.queue[0]!.id);
		expect(f.controller.activeItem?.sourceFile.name).toBe("b.txt");

		f.runtime.manifestRevision += 1;
		await expect.poll(() => f.controller.queue.length).toBe(0);
		expect(f.controller.activeIndex).toBe(0);
		await expect.poll(() => f.controller.canCreate).toBe(false);
		expect(collectionAccess).toHaveBeenCalledOnce();
	} finally {
		await f.dispose();
	}
});

it("starts a clean remote draft after success and invalidates its result on a metadata edit", async () => {
	const remoteDocument = document(new File(["remote"], "remote.txt", { type: "text/plain" }));
	const uploadFromURL = vi.fn(async () => remoteDocument);
	const f = await fixture(async (_slug, file) => document(file), {
		uploadFromURL: uploadFromURL as AdminClient["uploadFromURL"],
	});
	try {
		f.controller.setRemoteURL("https://assets.example/remote.txt");
		await f.controller.uploadRemote();
		expect(uploadFromURL).toHaveBeenCalledOnce();
		expect(f.controller.remoteDocumentID).toBe(remoteDocument.id);
		expect(f.controller.remoteURL).toBe("");
		expect(f.controller.remoteForm.dirty).toBe(false);
		expect(f.controller.dirty).toBe(false);

		f.controller.remoteForm.set("alt", "Another remote asset");
		expect(f.controller.remoteDocumentID).toBeUndefined();
		expect(f.controller.dirty).toBe(true);
		f.controller.discard();
		expect(f.controller.remoteForm.get("alt")).toBeUndefined();
		expect(f.controller.dirty).toBe(false);
	} finally {
		await f.dispose();
	}
});

it("does not save an earlier file while its replacement preview is preparing", async () => {
	let resolveDecode!: () => void;
	const decode = vi.spyOn(Image.prototype, "decode").mockImplementation(
		() =>
			new Promise<void>((resolve) => {
				resolveDecode = resolve;
			})
	);
	const upload = vi.fn(async (_slug: string, file: File) => document(file));
	const f = await fixture(upload);
	try {
		await f.controller.addFiles([new File(["original"], "original.txt", { type: "text/plain" })]);
		const item = f.controller.queue[0]!;
		const replacing = item.upload.select(
			new File(["replacement"], "replacement.png", { type: "image/png" }),
			f.owner.collection.uploadSettings!
		);
		expect(f.controller.preparing).toBe(true);
		await f.controller.saveAll();
		expect(upload).not.toHaveBeenCalled();
		expect(item.status).toBe("queued");

		resolveDecode();
		await replacing;
		await f.controller.saveAll();
		expect(upload).toHaveBeenCalledOnce();
		expect(upload.mock.calls[0]?.[1].name).toBe("replacement.png");
	} finally {
		await f.dispose();
		decode.mockRestore();
	}
});

it("revokes prepared and late image object URLs when queue items are removed", async () => {
	let resolveDecode!: () => void;
	const decode = vi.spyOn(Image.prototype, "decode").mockImplementation(
		() =>
			new Promise<void>((resolve) => {
				resolveDecode = resolve;
			})
	);
	let nextURL = 0;
	const createObjectURL = vi
		.spyOn(URL, "createObjectURL")
		.mockImplementation(() => `blob:bulk-${++nextURL}`);
	const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
	const f = await fixture(async (_slug, file) => document(file));
	try {
		const addingImage = f.controller.addFiles([
			new File(["image"], "pending.png", { type: "image/png" }),
		]);
		await expect.poll(() => f.controller.queue.length).toBe(1);
		const removedItem = f.controller.queue[0]!;
		f.controller.remove(removedItem.id);
		resolveDecode();
		await addingImage;
		expect(f.controller.queue).toEqual([]);
		expect(removedItem.status).toBe("queued");
		expect(removedItem.error).toBeUndefined();
		expect(revokeObjectURL).toHaveBeenCalledWith("blob:bulk-1");

		decode.mockResolvedValue(undefined);
		await f.controller.addFiles([new File(["plain"], "prepared.txt", { type: "text/plain" })]);
		f.controller.remove(f.controller.queue[0]!.id);
		expect(revokeObjectURL).toHaveBeenCalledWith("blob:bulk-2");
	} finally {
		await f.dispose();
		decode.mockRestore();
		createObjectURL.mockRestore();
		revokeObjectURL.mockRestore();
	}
});
