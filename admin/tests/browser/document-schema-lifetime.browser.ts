import { expect, it, vi } from "vitest";
import { flushSync, tick } from "svelte";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import {
	SCHEMA_MANIFEST_VERSION,
	type AccessCapabilitiesEnvelope,
	type DeleteEnvelope,
	type DocumentLockEnvelope,
	type SchemaCollection,
	type SchemaField,
} from "@riducms/protocol";
import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import { DocumentController } from "@admin/features/documents/document-controller.svelte";

const Editor = svelte`
	<script>
		let { Controller, runtime, notifications, navigate, slug, id, locale, ready } = $props();
		let route = $state();
		function transition(next) { route = next; }
		// svelte-ignore state_referenced_locally
		const controller = new Controller({
			runtime, notifications, navigate,
			get documentID() { return route?.id ?? id; },
			get slug() { return route?.slug ?? slug; },
			get global() { return false; },
			get locale() { return route?.locale ?? locale; },
			get editable() { return true; },
		});
		ready(controller, transition);
	</script>
	<p>{controller.loading ? "Loading" : controller.documentHeading}</p>
`;

const allowedAccess: AccessCapabilitiesEnvelope = {
	operations: {
		admin: true,
		create: true,
		read: true,
		readVersions: false,
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

const titleField = textField("posts-title", "title", "Title");
const summaryField = textField("posts-summary", "summary", "Summary");
const posts = collection("posts", [titleField, summaryField]);

interface FindOptions {
	locale?: string;
	signal?: AbortSignal;
}

interface CollectionAccessOptions extends FindOptions {
	id?: string;
	data?: Record<string, unknown>;
}

interface RequestOptions {
	keepalive?: boolean;
	signal?: AbortSignal;
}

interface ClientOverrides {
	find?: (slug: string, id: string, options?: FindOptions) => Promise<AdminDocument>;
	update?: (
		slug: string,
		id: string,
		data: Record<string, unknown>,
		options?: FindOptions
	) => Promise<AdminDocument>;
	collectionAccess?: (
		slug: string,
		options?: CollectionAccessOptions
	) => Promise<AccessCapabilitiesEnvelope>;
	acquireDocumentLock?: (
		slug: string,
		id: string,
		takeover?: boolean,
		options?: RequestOptions
	) => Promise<DocumentLockEnvelope>;
	releaseDocumentLock?: (
		slug: string,
		id: string,
		options?: RequestOptions
	) => Promise<DeleteEnvelope>;
}

interface FixtureOptions {
	id?: string;
	slug?: string;
	locale?: string;
	schema?: SchemaCollection;
	client?: ClientOverrides;
}

async function editor(options: FixtureOptions = {}) {
	const schema = options.schema ?? posts;
	const client = {
		find: vi.fn(async (slug: string, id: string, request?: FindOptions) => ({
			id,
			title: `${slug}:${id}:${request?.locale ?? "default"}`,
			summary: "Stored summary",
			_revision: 2,
		})),
		collectionAccess: vi.fn(async () => allowedAccess),
		...options.client,
	} as unknown as AdminClient;
	const runtime = new AdminRuntime(client);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Document schema lifetime" },
		collections: [schema],
		plugins: [],
	};
	runtime.manifestRevision = 1;
	const notifications = new NotificationCenter();
	const warning = vi.spyOn(notifications, "warning").mockReturnValue(0);
	const error = vi.spyOn(notifications, "error").mockReturnValue(0);
	const navigate = vi.fn();
	let controller!: DocumentController;
	let transition!: (route: { slug: string; id?: string; locale?: string }) => void;
	const props = {
		Controller: DocumentController,
		runtime,
		notifications,
		navigate,
		slug: options.slug ?? schema.slug,
		id: Object.hasOwn(options, "id") ? options.id : "one",
		locale: options.locale ?? "en",
		ready: (
			value: DocumentController,
			setRoute: (route: { slug: string; id?: string; locale?: string }) => void
		) => {
			controller = value;
			transition = setRoute;
		},
	};
	const screen = await render(Editor, props);
	const dispose = async () => {
		await screen.unmount();
		notifications.destroy();
		runtime.dispose();
	};
	return {
		client,
		controller,
		dispose,
		error,
		navigate,
		notifications,
		props,
		runtime,
		screen,
		transition,
		warning,
	};
}

function refreshSchema(
	fixture: Awaited<ReturnType<typeof editor>>,
	collections: SchemaCollection[]
) {
	fixture.runtime.manifest = { ...fixture.runtime.manifest!, collections };
	fixture.runtime.manifestRevision += 1;
}

it("keeps an existing document draft dirty through a stable-ID field rename", async () => {
	const fixture = await editor();
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		fixture.controller.form.set("title", "Unsaved headline");
		const renamed = collection("posts", [
			textField("posts-title", "headline", "Headline"),
			summaryField,
		]);

		refreshSchema(fixture, [renamed]);

		await expect.poll(() => fixture.controller.form.get("headline")).toBe("Unsaved headline");
		expect(fixture.controller.form.get("title")).toBeUndefined();
		expect(fixture.controller.form.original.headline).toBe("posts:one:en");
		expect(fixture.controller.hasUnsavedChanges).toBe(true);
		expect(fixture.warning).not.toHaveBeenCalled();
	} finally {
		await fixture.dispose();
	}
});

it("revokes a pending save when schema reconciliation replaces its draft lifetime", async () => {
	const update = Promise.withResolvers<AdminDocument>();
	const fixture = await editor({ client: { update: vi.fn(() => update.promise) } });
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		fixture.controller.form.set("title", "Submitted old-name edit");
		const pending = fixture.controller.save();
		await expect.poll(() => fixture.controller.form.submitting).toBe(true);

		refreshSchema(fixture, [
			collection("posts", [textField("posts-title", "headline", "Headline"), summaryField]),
		]);
		await expect.poll(() => fixture.controller.form.submitting).toBe(false);
		expect(fixture.controller.form.get("headline")).toBe("Submitted old-name edit");
		fixture.controller.form.set("headline", "New schema edit");

		update.resolve({ id: "one", title: "Superseded save", _revision: 3 });
		expect(await pending).toBe(false);
		expect(fixture.controller.form.get("headline")).toBe("New schema edit");
		expect(fixture.controller.form.original.headline).toBe("posts:one:en");
		expect(fixture.controller.hasUnsavedChanges).toBe(true);
	} finally {
		await fixture.dispose();
	}
});

it("keeps a create draft and initializes new defaults through a stable-ID field rename", async () => {
	const fixture = await editor({ id: undefined });
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		fixture.controller.form.set("title", "New unsaved post");
		const status = textField("posts-status", "status", "Status");
		status.default = "draft";
		const renamed = collection("posts", [textField("posts-title", "headline", "Headline"), status]);

		refreshSchema(fixture, [renamed]);

		await expect.poll(() => fixture.controller.form.get("headline")).toBe("New unsaved post");
		expect(fixture.controller.form.get("status")).toBe("draft");
		expect(fixture.controller.form.original.status).toBe("draft");
		expect(fixture.controller.hasUnsavedChanges).toBe(true);
	} finally {
		await fixture.dispose();
	}
});

it("preserves compatible edits and warns when a field is removed", async () => {
	const fixture = await editor();
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		fixture.controller.form.set("title", "Detached edit");
		fixture.controller.form.set("summary", "Retained edit");

		refreshSchema(fixture, [collection("posts", [summaryField])]);

		await expect.poll(() => fixture.controller.form.get("title")).toBeUndefined();
		expect(fixture.controller.form.get("summary")).toBe("Retained edit");
		expect(fixture.controller.hasUnsavedChanges).toBe(true);
		expect(fixture.warning).toHaveBeenCalledOnce();
	} finally {
		await fixture.dispose();
	}
});

it("retains a dirty editor while its collection is removed and reconciles it when reintroduced", async () => {
	const fixture = await editor();
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		fixture.controller.form.set("title", "Work through reload");

		refreshSchema(fixture, []);

		await expect.poll(() => fixture.controller.collectionAvailable).toBe(false);
		expect(fixture.controller.form.get("title")).toBe("Work through reload");
		expect(fixture.controller.hasUnsavedChanges).toBe(true);
		expect(fixture.error).toHaveBeenCalledOnce();

		const restored = collection("articles", [
			textField("posts-title", "headline", "Headline"),
			summaryField,
		]);
		refreshSchema(fixture, [restored]);

		await expect.poll(() => fixture.controller.collectionAvailable).toBe(true);
		expect(fixture.controller.form.get("headline")).toBe("Work through reload");
		expect(fixture.controller.hasUnsavedChanges).toBe(true);
		expect(fixture.controller.form.resource?.collection).toBe("articles");
		expect(fixture.navigate).toHaveBeenCalledWith("/collections/articles/one?locale=en", {
			replace: true,
		});
	} finally {
		await fixture.dispose();
	}
});

it("restarts an interrupted document load when its collection is reintroduced", async () => {
	const loads: Array<ReturnType<typeof Promise.withResolvers<AdminDocument>>> = [];
	const find = vi.fn(() => {
		const load = Promise.withResolvers<AdminDocument>();
		loads.push(load);
		return load.promise;
	});
	const fixture = await editor({ client: { find } });
	try {
		await expect.poll(() => loads.length).toBe(1);
		refreshSchema(fixture, []);
		await expect.poll(() => fixture.controller.collectionAvailable).toBe(false);

		refreshSchema(fixture, [posts]);
		await expect.poll(() => loads.length).toBe(2);
		loads[1]!.resolve({ id: "one", title: "Reloaded document", _revision: 3 });
		await expect.poll(() => fixture.controller.form.get("title")).toBe("Reloaded document");

		loads[0]!.resolve({ id: "one", title: "Superseded document", _revision: 2 });
		await tick();
		await tick();
		expect(fixture.controller.form.get("title")).toBe("Reloaded document");
		expect(fixture.controller.collectionAvailable).toBe(true);
	} finally {
		await fixture.dispose();
	}
});

it("renames a collection without reloading or changing its dirty document", async () => {
	const fixture = await editor();
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		fixture.controller.form.set("title", "Unsaved title");
		const initialFindCount = vi.mocked(fixture.client.find).mock.calls.length;
		refreshSchema(fixture, [collection("articles", posts.fields)]);

		await expect.poll(() => fixture.controller.collectionSlug).toBe("articles");
		expect(fixture.controller.form.get("title")).toBe("Unsaved title");
		expect(fixture.controller.form.resource?.collection).toBe("articles");
		expect(vi.mocked(fixture.client.find)).toHaveBeenCalledTimes(initialFindCount);
		expect(fixture.navigate).toHaveBeenCalledWith("/collections/articles/one?locale=en", {
			replace: true,
		});
	} finally {
		await fixture.dispose();
	}
});

it("refreshes path-keyed field access after a stable-ID rename", async () => {
	const oldAccess = {
		...allowedAccess,
		fields: { title: { read: true, create: true, update: false } },
	};
	const renamedAccess = {
		...allowedAccess,
		fields: { headline: { read: true, create: true, update: false } },
	};
	const collectionAccess = vi
		.fn()
		.mockResolvedValueOnce(oldAccess)
		.mockResolvedValue(renamedAccess);
	const fixture = await editor({ client: { collectionAccess } });
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		expect(fixture.controller.form.canWrite("title")).toBe(false);

		refreshSchema(fixture, [
			collection("posts", [textField("posts-title", "headline", "Headline"), summaryField]),
		]);

		await expect.poll(() => collectionAccess.mock.calls.length).toBeGreaterThan(1);
		await expect.poll(() => fixture.controller.form.canWrite("headline")).toBe(false);
		expect(fixture.controller.form.get("headline")).toBe("posts:one:en");
	} finally {
		await fixture.dispose();
	}
});

it("loads the requested document when its route and schema rename change together", async () => {
	const fixture = await editor();
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		flushSync(() => {
			refreshSchema(fixture, [collection("articles", posts.fields)]);
			fixture.transition({ slug: "articles", id: "two", locale: "en" });
		});

		await expect.poll(() => fixture.controller.currentDocument?.id).toBe("two");
		expect(fixture.controller.form.get("title")).toBe("articles:two:en");
		expect(fixture.controller.form.resource).toEqual({
			collection: "articles",
			id: "two",
			global: false,
		});
		expect(fixture.navigate).not.toHaveBeenCalled();
	} finally {
		await fixture.dispose();
	}
});

it("replaces pending create access with the renamed collection request", async () => {
	const requests = new Map<
		string,
		ReturnType<typeof Promise.withResolvers<AccessCapabilitiesEnvelope>>
	>();
	const collectionAccess = vi.fn((slug: string) => {
		const request = Promise.withResolvers<AccessCapabilitiesEnvelope>();
		requests.set(slug, request);
		return request.promise;
	});
	const fixture = await editor({ id: undefined, client: { collectionAccess } });
	try {
		await expect.poll(() => requests.has("posts")).toBe(true);
		fixture.controller.form.set("title", "Pending create draft");
		refreshSchema(fixture, [collection("articles", posts.fields)]);
		await expect.poll(() => requests.has("articles")).toBe(true);

		const denied = {
			...allowedAccess,
			operations: { ...allowedAccess.operations, create: false },
		};
		requests.get("articles")!.resolve(denied);
		await expect.poll(() => fixture.controller.loading).toBe(false);
		requests.get("posts")!.resolve(allowedAccess);
		await tick();

		expect(fixture.controller.form.access?.operations.create).toBe(false);
		expect(fixture.controller.form.get("title")).toBe("Pending create draft");
		expect(fixture.controller.form.resource?.collection).toBe("articles");
	} finally {
		await fixture.dispose();
	}
});

it("keeps schema-refresh access blocking authoritative while an old lock heartbeat completes", async () => {
	const heartbeat = Promise.withResolvers<DocumentLockEnvelope>();
	const schemaAccess = Promise.withResolvers<AccessCapabilitiesEnvelope>();
	const owned: DocumentLockEnvelope = {
		lock: {
			documentId: "one",
			ownerId: "editor",
			ownerLabel: "Editor",
			createdAt: "2026-09-13T00:00:00Z",
			updatedAt: "2026-09-13T00:00:00Z",
			expiresAt: "2026-09-13T00:02:00Z",
		},
		owned: true,
		acquired: true,
		canTakeOver: false,
	};
	const collectionAccess = vi
		.fn()
		.mockResolvedValueOnce(allowedAccess)
		.mockImplementationOnce(() => schemaAccess.promise);
	const acquireDocumentLock = vi
		.fn()
		.mockResolvedValueOnce(owned)
		.mockImplementationOnce(() => heartbeat.promise)
		.mockResolvedValue(owned);
	const callbacks: Array<() => Promise<void>> = [];
	const nativeSetInterval = globalThis.setInterval.bind(globalThis);
	const nativeClearInterval = globalThis.clearInterval.bind(globalThis);
	const interval = vi.spyOn(globalThis, "setInterval").mockImplementation((callback) => {
		callbacks.push(callback as () => Promise<void>);
		return nativeSetInterval(() => undefined, 60_000);
	});
	const clearInterval = vi
		.spyOn(globalThis, "clearInterval")
		.mockImplementation((timer) => nativeClearInterval(timer));
	const locking = {
		...posts,
		capabilities: { ...posts.capabilities, locking: true },
		documentLockSettings: { durationSeconds: 120 },
	};
	const fixture = await editor({
		schema: locking,
		client: {
			acquireDocumentLock,
			collectionAccess,
			releaseDocumentLock: vi.fn(async (_slug, id) => ({ id, deleted: true })),
		},
	});
	try {
		await expect.poll(() => callbacks.length).toBe(1);
		await expect.poll(() => fixture.controller.loading).toBe(false);
		const pendingHeartbeat = callbacks[0]!();
		await expect.poll(() => acquireDocumentLock.mock.calls.length).toBe(2);

		refreshSchema(fixture, [
			{
				...locking,
				fields: [textField("posts-title", "headline", "Headline"), summaryField],
			},
		]);
		await expect.poll(() => fixture.controller.form.writeBlocked).toBe(true);
		heartbeat.resolve(owned);
		await pendingHeartbeat;
		expect(fixture.controller.form.writeBlocked).toBe(true);
		expect(callbacks).toHaveLength(1);

		schemaAccess.resolve(allowedAccess);
		await expect.poll(() => fixture.controller.loading).toBe(false);
		expect(fixture.controller.form.writeBlocked).toBe(false);
		expect(callbacks).toHaveLength(2);
	} finally {
		await fixture.dispose();
		interval.mockRestore();
		clearInterval.mockRestore();
	}
});

it("reconciles a schema update while retaining a dirty locale editor", async () => {
	const fixture = await editor();
	try {
		await expect.poll(() => fixture.controller.loading).toBe(false);
		fixture.controller.form.set("title", "English draft");
		const initialFindCount = vi.mocked(fixture.client.find).mock.calls.length;
		const renamed = collection("posts", [
			textField("posts-title", "headline", "Headline"),
			summaryField,
		]);
		refreshSchema(fixture, [renamed]);
		await fixture.screen.rerender({ ...fixture.props, locale: "fr" });

		await expect.poll(() => fixture.controller.form.get("headline")).toBe("English draft");
		expect(fixture.controller.contentLocale).toBe("en");
		expect(fixture.controller.form.contentLocale).toBe("en");
		expect(fixture.controller.hasUnsavedChanges).toBe(true);
		expect(vi.mocked(fixture.client.find)).toHaveBeenCalledTimes(initialFindCount);
	} finally {
		await fixture.dispose();
	}
});

it("does not let a late previous-locale load overwrite the current locale", async () => {
	const loads = new Map<string, ReturnType<typeof Promise.withResolvers<AdminDocument>>>();
	const find = vi.fn((_slug: string, id: string, request?: FindOptions) => {
		const locale = request?.locale ?? "default";
		const load = Promise.withResolvers<AdminDocument>();
		loads.set(locale, load);
		return load.promise;
	});
	const fixture = await editor({ client: { find } });
	try {
		await expect.poll(() => loads.has("en")).toBe(true);
		await fixture.screen.rerender({ ...fixture.props, locale: "fr" });
		await expect.poll(() => loads.has("fr")).toBe(true);
		loads.get("fr")!.resolve({ id: "one", title: "French", _revision: 3 });
		await expect.poll(() => fixture.controller.form.get("title")).toBe("French");

		loads.get("en")!.resolve({ id: "one", title: "Late English", _revision: 2 });
		await tick();
		await tick();

		expect(fixture.controller.form.get("title")).toBe("French");
		expect(fixture.controller.contentLocale).toBe("fr");
	} finally {
		await fixture.dispose();
	}
});

function collection(slug: string, fields: SchemaField[]): SchemaCollection {
	return {
		id: "posts",
		slug,
		labels: { singular: "Post", plural: "Posts" },
		admin: {},
		capabilities: {
			auth: false,
			upload: false,
			versions: false,
			trash: true,
			locking: false,
		},
		fields,
	};
}

function textField(id: string, name: string, label: string): SchemaField {
	return {
		id,
		name,
		path: name,
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label },
		text: {},
	};
}
