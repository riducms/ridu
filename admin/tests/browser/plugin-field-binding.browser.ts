import { describe, expect, it } from "vitest";
import type { SchemaField } from "@riducms/protocol";
import type { FieldAuthoringHost, FieldDocument } from "@riducms/plugin";
import { PluginFieldBinding } from "@admin/core/forms/plugin-field-binding";
import { guardPluginAuthoring } from "@admin/core/forms/plugin-field-authoring";
import { createAdminI18n } from "@riducms/translations";

import { FormController } from "@admin/core/forms/form-controller.svelte";
import { createEmbeddedSchemaDraft } from "@admin/core/forms/embedded-schema-draft.svelte";
const leaf: SchemaField = {
	id: "value",
	name: "value",
	path: "rows.value",
	type: "plugin",
	category: "plugin",
	required: false,
	unique: false,
	admin: { label: "Value" },
	plugin: { key: "structured", config: { nested: { flag: true } } },
};
const rows: SchemaField = {
	id: "rows",
	name: "rows",
	path: "rows",
	type: "array",
	category: "nested",
	required: false,
	unique: false,
	admin: { label: "Rows" },
	nested: { fields: [leaf] },
};
const decodeValue = (value: unknown) => value;
function fixture() {
	const form = new FormController();
	form.reset(
		{
			rows: [
				{ _key: "a", value: { items: [1] } },
				{ _key: "b", value: { items: [2] } },
			],
		},
		[rows]
	);
	let schema = { ...leaf, path: "rows.0.value", id: "value-a" };
	let version = 1;
	const binding = new PluginFieldBinding(
		form,
		() => schema,
		{ decodeValue },
		() => version
	);
	return {
		form,
		binding,
		changeSchema: () => {
			version++;
		},
		readonly: () => {
			schema = { ...schema, admin: { ...schema.admin, readOnly: true } };
		},
	};
}

describe("advanced plugin occurrence bindings", () => {
	it("keeps pending submission out of resolved read-only presentation", () => {
		const { form, binding } = fixture();
		form.submitting = true;
		expect(binding.readOnly).toBe(true);
		expect(binding.schema.admin.readOnly).not.toBe(true);
		expect(() => binding.set({ items: [3] })).toThrow("read-only");
		binding.destroy();
	});

	it("retained writers and acquired sibling writers follow logical rows across reorder", () => {
		const { form, binding } = fixture();
		const set = binding.set,
			sibling = binding.form.bind("rows.1.value");
		form.setRows("rows", (form.snapshot().rows as Record<string, unknown>[]).toReversed());
		set({ items: [3] });
		sibling.set({ items: [4] });
		expect(form.get("rows.1.value")).toEqual({ items: [3] });
		expect(form.get("rows.0.value")).toEqual({ items: [4] });
		binding.destroy();
		expect(() => sibling.set({ items: [5] })).toThrow("stale");
		expect(form.isRegistered("rows.1.value")).toBe(false);
	});
	for (const transition of [
		"remove and reuse key",
		"variant",
		"reset",
		"locale",
		"resource",
		"schema",
		"unmount",
	] as const) {
		it(`revokes reads, writes and acquired capabilities after ${transition}`, () => {
			const { form, binding, changeSchema } = fixture();
			const read = binding.form.get,
				write = binding.set,
				sibling = binding.form.bind("rows.1.value");
			const before = form.snapshot();
			if (transition === "remove and reuse key") {
				form.setRows("rows", []);
				form.setRows("rows", before.rows as Record<string, unknown>[]);
			}
			if (transition === "variant") form.set("rows.0.blockType", "changed");
			if (transition === "reset") form.reset(before);
			if (transition === "locale") form.setLocalization("fr", {});
			if (transition === "resource") form.setResource({ collection: "posts", id: "other" });
			if (transition === "schema") changeSchema();
			if (transition === "unmount") binding.destroy();
			expect(() => read("rows.0.value")).toThrow("stale");
			expect(() => write({ items: [8] })).toThrow("stale");
			expect(() => sibling.set({ items: [9] })).toThrow("stale");
		});
	}
	it("detaches reads, snapshots, schema, issues, supplied values and decoder-owned results", () => {
		const { form, binding, readonly } = fixture();
		(binding.rawValue as { items: number[] }).items.push(9);
		(binding.value as { items: number[] }).items.push(9);
		(binding.form.get("rows.0.value") as { items: number[] }).items.push(9);
		(binding.schema.plugin!.config as { nested: { flag: boolean } }).nested.flag = false;
		(binding.form.snapshot().rows as Record<string, unknown>[])[0]!.value = null;
		expect(form.get("rows.0.value")).toEqual({ items: [1] });
		expect(leaf.plugin!.config).toEqual({ nested: { flag: true } });
		const supplied = { items: [2] };
		binding.set(supplied);
		supplied.items.push(9);
		expect(form.get("rows.0.value")).toEqual({ items: [2] });
		readonly();
		expect(() => binding.set({ items: [] })).toThrow("read-only");
		(binding.rawValue as { items: number[] }).items.push(9);
		expect(form.get("rows.0.value")).toEqual({ items: [2] });
		binding.destroy();
	});
	it("rechecks lifetime and editability after a decoder executes", () => {
		const { form, binding } = fixture();
		binding.destroy();
		const hostile = new PluginFieldBinding(form, () => ({ ...leaf, path: "rows.0.value" }), {
			decodeValue(value) {
				form.reset(form.snapshot());
				return value;
			},
		});
		expect(() => hostile.set({ items: [8] })).toThrow("stale");
		expect(form.get("rows.0.value")).toEqual({ items: [1] });
	});
	it("rejects delayed document/plugin responses and retained browser callbacks after revocation", async () => {
		const { binding } = fixture();
		let finish!: (value: FieldDocument) => void;
		let commit!: () => Promise<unknown>;
		let commits = 0;
		const host: FieldAuthoringHost = {
			collections: [],
			documentRevision: 0,
			findDocument: () =>
				new Promise((resolve) => {
					finish = resolve;
				}),
			referenceBrowser: (_anchor, props) => {
				commit = async () => props.onCommit(["one"]);
				return {};
			},
		};
		const guarded = guardPluginAuthoring(host, binding);
		guarded.referenceBrowser({} as never, {
			field: leaf,
			collection: {} as never,
			hasMany: false,
			selectedIDs: [],
			onCommit: () => {
				commits++;
			},
			onClose() {},
		});
		const pending = guarded.findDocument("posts", "one");
		binding.destroy();
		finish({ id: "one" });
		await expect(pending).rejects.toThrow("stale");
		await expect(commit()).rejects.toThrow("stale");
		expect(commits).toBe(0);
	});
});

const title: SchemaField = { ...leaf, id: "title", name: "title", path: "body.title" };
const embedded: SchemaField = {
	...leaf,
	id: "body",
	name: "body",
	path: "body",
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
							{ slug: "card", labels: { singular: "Card", plural: "Cards" }, fields: [title] },
						],
					},
				],
			},
		],
	},
};
function embeddedFixture() {
	const form = new FormController();
	form.reset(
		{
			body: {
				outline: [
					{ kind: "widget", content: { schema: "card", uid: "a", title: { items: [1] } } },
					{ kind: "widget", content: { schema: "card", uid: "b", title: { items: [2] } } },
				],
			},
		},
		[embedded]
	);
	return form;
}
it("binds fields inside plugin-owned embedded trees by descriptor identity", () => {
	const form = embeddedFixture();
	const binding = new PluginFieldBinding(
		form,
		() => ({ ...title, path: "body.outline.0.content.title" }),
		{ decodeValue }
	);
	const rows = form.get("body.outline") as unknown[];
	form.set("body.outline", rows.toReversed());
	binding.set({ items: [3] });
	expect(form.get("body.outline.1.content.title")).toEqual({ items: [3] });
	form.set("body.outline", []);
	form.set("body.outline", rows);
	expect(() => binding.set({ items: [4] })).toThrow("stale");
});
it("returned drafts cannot revive after removal/reinsertion or write through retained child bindings", () => {
	const form = embeddedFixture();
	const parent = new PluginFieldBinding(form, () => embedded, { decodeValue });
	let session!: ReturnType<typeof createEmbeddedSchemaDraft>;
	const authoring = guardPluginAuthoring(
		{
			collections: [],
			documentRevision: 0,
			referenceBrowser: () => ({}),
			findDocument: async () => ({ id: "one" }),
			beginSchemaDraft(scope) {
				session = createEmbeddedSchemaDraft(form, () => parent.schema, scope, createAdminI18n());
				return session.draft;
			},
		},
		parent
	);
	const draft = authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	const child = new PluginFieldBinding(session.form, () => session.fields[0]!, { decodeValue });
	const before = form.snapshot();
	form.set("body.outline", []);
	form.set("body", before.body);
	expect(draft.stale).toBe(true);
	expect(() => draft.payload()).toThrow("stale");
	expect(() => child.set({ items: [5] })).toThrow("stale");
	draft.discard();
	parent.destroy();
	expect(form.pendingEditIssues()).toEqual([]);
});
it("completed raw draft sessions release owner cleanup while live drafts remain owned", async () => {
	const form = embeddedFixture();
	const binding = new PluginFieldBinding(form, () => embedded, { decodeValue });
	const sessions: ReturnType<typeof createEmbeddedSchemaDraft>[] = [];
	const disposals: { count: number }[] = [];
	const authoring = guardPluginAuthoring(
		{
			collections: [],
			documentRevision: 0,
			referenceBrowser: () => ({}),
			findDocument: async () => ({ id: "one" }),
			beginSchemaDraft(scope) {
				const session = createEmbeddedSchemaDraft(
					form,
					() => binding.schema,
					scope,
					createAdminI18n()
				);
				const disposal = { count: 0 };
				const discard = session.draft.discard;
				session.draft.discard = () => {
					disposal.count++;
					discard();
				};
				sessions.push(session);
				disposals.push(disposal);
				return session.draft;
			},
		},
		binding
	);
	for (let index = 0; index < 3; index++) {
		const draft = authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
		const session = sessions[index]!;
		const child = new PluginFieldBinding(session.form, () => session.fields[0]!, { decodeValue });
		// The drawer's Apply/unmount path disposes its raw session, not the public wrapper.
		session.draft.discard();
		expect(draft.stale).toBe(true);
		expect(() => child.set({ items: [9] })).toThrow("stale");
	}
	const active = authoring.beginSchemaDraft!({ treeKey: "widgets", identity: "a" });
	expect(disposals.map(({ count }) => count)).toEqual([1, 1, 1, 0]);
	binding.destroy();
	expect(active.stale).toBe(true);
	expect(disposals.map(({ count }) => count)).toEqual([1, 1, 1, 1]);
	await Promise.resolve();
	expect(form.pendingEditIssues()).toEqual([]);
});
it("retained acquired bindings intersect owner and ancestor editability", () => {
	const { form, binding, readonly } = fixture();
	const child = binding.form.bind("rows.1.value");
	readonly();
	expect(child.readOnly).toBe(true);
	expect(() => child.set({ items: [8] })).toThrow("read-only");
	binding.destroy();
	const group: SchemaField = {
		...rows,
		type: "group",
		name: "group",
		path: "group",
		admin: { label: "Group", readOnly: true },
	};
	form.reset({ group: { value: { items: [1] } } }, [group]);
	const nested = new PluginFieldBinding(form, () => ({ ...leaf, path: "group.value" }), {
		decodeValue,
	});
	expect(nested.readOnly).toBe(true);
	expect(() => nested.set({ items: [2] })).toThrow("read-only");
	nested.destroy();
});
it("structural writes rebase descendant access and reject protected descendant changes", () => {
	const { form, binding } = fixture();
	const operations = {
		create: true,
		read: true,
		update: true,
		delete: true,
		admin: true,
		readVersions: true,
		duplicate: true,
		publish: true,
		unpublish: true,
		restore: true,
		purge: true,
		unlock: true,
		restoreDeleted: true,
		deletePermanent: true,
		selectAll: true,
	};
	form.setAccess(
		{ operations, fields: { "rows.1.value": { read: true, create: false, update: false } } },
		"update"
	);
	const b = binding.form.bind("rows.1.value");
	const list = new PluginFieldBinding(form, () => rows, { decodeValue });
	list.set((form.snapshot().rows as unknown[]).toReversed());
	expect(b.readOnly).toBe(true);
	expect(() => b.set({ items: [8] })).toThrow("read-only");
	const changed = form.snapshot().rows as Record<string, unknown>[];
	changed[0]!.value = { items: [8] };
	expect(() => list.set(changed)).toThrow("read-only");
	expect(form.get("rows.0.value")).toEqual({ items: [2] });
	list.destroy();
	binding.destroy();
});
it("browser capability forwarding preserves live Svelte prop getters", () => {
	const { binding } = fixture();
	let open = false;
	let readOpen!: () => boolean | undefined;
	const guarded = guardPluginAuthoring(
		{
			collections: [],
			documentRevision: 0,
			findDocument: async () => ({ id: "one" }),
			referenceBrowser: (_anchor, props) => {
				readOpen = () => props.open;
				return {};
			},
		},
		binding
	);
	guarded.referenceBrowser({} as never, {
		get open() {
			return open;
		},
		field: leaf,
		collection: {} as never,
		hasMany: false,
		selectedIDs: [],
		onCommit() {},
		onClose() {},
	});
	expect(readOpen()).toBe(false);
	open = true;
	expect(readOpen()).toBe(true);
	binding.destroy();
});
it("rejects non-JSON supplied values instead of leaking or changing their types", () => {
	const { binding, form } = fixture();
	for (const value of [new Date(), new Map(), { fn: () => 1 }, Number.NaN])
		expect(() => binding.set(value)).toThrow("JSON");
	const cycle: Record<string, unknown> = {};
	cycle.self = cycle;
	expect(() => binding.set(cycle)).toThrow("JSON");
	expect(form.get("rows.0.value")).toEqual({ items: [1] });
	binding.destroy();
});
it("browser capability forwarding follows replacement spread-props backing objects", () => {
	const { binding } = fixture();
	let current = {
		open: false,
		field: leaf,
		collection: {} as never,
		hasMany: false,
		selectedIDs: [] as string[],
		onCommit() {},
		onClose() {},
	};
	const spread = new Proxy(current, { get: (_target, key) => Reflect.get(current, key) });
	let readOpen!: () => boolean | undefined;
	const guarded = guardPluginAuthoring(
		{
			collections: [],
			documentRevision: 0,
			findDocument: async () => ({ id: "one" }),
			referenceBrowser: (_anchor, props) => {
				readOpen = () => props.open;
				return {};
			},
		},
		binding
	);
	guarded.referenceBrowser({} as never, spread);
	expect(readOpen()).toBe(false);
	current = { ...current, open: true };
	expect(readOpen()).toBe(true);
	binding.destroy();
});
it("schema refresh releases pending draft edits before the first dirty read", () => {
	const form = embeddedFixture();
	let version = 1;
	const binding = new PluginFieldBinding(
		form,
		() => embedded,
		{ decodeValue },
		() => version
	);
	const session = createEmbeddedSchemaDraft(
		form,
		() => binding.schema,
		{ treeKey: "widgets", identity: "a" },
		createAdminI18n(),
		() => !binding.stale
	);
	session.form.set(session.fields[0]!.path, { items: [2] });
	version++;
	expect(() => form.dirty).not.toThrow();
	expect(session.draft.stale).toBe(true);
	expect(form.pendingEditIssues()).toEqual([]);
	session.draft.discard();
	binding.destroy();
});
it("actual registrations read both server output and pending input without inventing server data", async () => {
	const { definePluginField } = await import("@riducms/plugin/authoring/v1");
	type Output = { id: string; label: string };
	type Input = { id: string };
	const registration = definePluginField({
		component: (() => ({})) as import("svelte").Component<
			import("@riducms/plugin").PluginFieldProps<Output, undefined, "plugin", Input>
		>,
		decodeValue(raw: unknown): Output {
			if (
				typeof raw !== "object" ||
				raw === null ||
				!("id" in raw) ||
				typeof raw.id !== "string" ||
				!("label" in raw) ||
				typeof raw.label !== "string"
			)
				throw new Error("invalid output");
			return { id: raw.id, label: raw.label };
		},
		decodeInput(raw: unknown): Input {
			if (typeof raw !== "object" || raw === null || !("id" in raw) || typeof raw.id !== "string")
				throw new Error("invalid input");
			return { id: raw.id };
		},
	});
	const { form, binding } = fixture();
	binding.destroy();
	form.set("rows.0.value", { id: "one", label: "Server label" });
	const input = new PluginFieldBinding(
		form,
		() => ({ ...leaf, path: "rows.0.value" }),
		registration
	);
	expect(input.value).toEqual({ id: "one", label: "Server label" });
	input.set({ id: "two" });
	expect(input.value).toEqual({ id: "two" });
	expect(form.get("rows.0.value")).toEqual({ id: "two" });
	expect(() => input.set({ id: 3 })).toThrow("invalid input");
	input.destroy();
});

it("browser forwarding retains Svelte bindable accessor setters", () => {
	const { binding } = fixture();
	let open = false;
	const guarded = guardPluginAuthoring(
		{
			collections: [],
			documentRevision: 0,
			findDocument: async () => ({ id: "one" }),
			referenceBrowser: (_anchor, props) => {
				const setter = Object.getOwnPropertyDescriptor(props, "open")?.set;
				expect(setter).toBeDefined();
				setter!.call(props, true);
				return {};
			},
		},
		binding
	);
	guarded.referenceBrowser({} as never, {
		get open() {
			return open;
		},
		set open(value) {
			open = value;
		},
		field: leaf,
		collection: {} as never,
		hasMany: false,
		selectedIDs: [],
		onCommit() {},
		onClose() {},
	});
	expect(open).toBe(true);
	binding.destroy();
});

it("draft child bindings synchronously revoke when parent access changes without a value mutation", () => {
	const form = embeddedFixture();
	const parent = new PluginFieldBinding(form, () => embedded, { decodeValue });
	const session = createEmbeddedSchemaDraft(
		form,
		() => parent.schema,
		{ treeKey: "widgets", identity: "a" },
		createAdminI18n(),
		() => !parent.stale
	);
	const child = new PluginFieldBinding(session.form, () => session.fields[0]!, { decodeValue });
	form.setAccess(
		{
			operations: { update: true },
			fields: { body: { read: true, create: false, update: false } },
		} as never,
		"update"
	);
	expect(() => child.set({ items: [9] })).toThrow("stale");
	expect(session.draft.stale).toBe(true);
	expect(child.stale).toBe(true);
	session.draft.discard();
	parent.destroy();
});
