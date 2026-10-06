import { describe, expect, it } from "vitest";
import type { AccessCapabilitiesEnvelope, SchemaBlockType, SchemaField } from "@riducms/protocol";

import { normalizeSlug, slugFollowsSource } from "@admin/fields/text/slug";
import { changedFormValues, reconcileFormSchema } from "@admin/core/forms/form-schema";
import { bindBlockField } from "../block-manifest";
import { card, tree } from "./draft-fixture";

const { FormController } = await import("@admin/core/forms/form-controller.svelte");

it("retains edits typed while a draft autosave is in flight", async () => {
	const field: SchemaField = {
		id: "title",
		name: "title",
		path: "title",
		type: "text",
		category: "scalar",
		required: true,
		unique: false,
		admin: { label: "Title" },
		text: {},
	};
	const form = new FormController({ title: "Original" });
	form.reset({ title: "Original" }, [field]);
	form.set("title", "Submitted");
	const result = Promise.withResolvers<{ title: string }>();
	const saving = form.submit([field], false, async () => result.promise, {
		mode: "draft",
		allowEditsDuringRequest: true,
	});
	expect(form.editingBlocked).toBe(false);
	form.set("title", "Typed later");
	result.resolve({ title: "Submitted" });
	await saving;
	form.acceptSavedBaseline({ title: "Submitted" }, { title: "Submitted" });
	expect(form.get("title")).toBe("Typed later");
	expect(form.original.title).toBe("Submitted");
	expect(form.dirty).toBe(true);
	expect(form.submitting).toBe(false);
});

it("keeps plugin editor lifetimes when a silent save settles without later typing", async () => {
	const field: SchemaField = {
		id: "title",
		name: "title",
		path: "title",
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Title" },
		text: {},
	};
	const form = new FormController();
	form.reset({ title: "Original" }, [field]);
	form.set("title", "Edited");
	const epoch = form.editorEpoch;
	let invalidations = 0;
	form.registerEditorLifetime(() => invalidations++);
	const response = Promise.withResolvers<void>();
	const saving = form.submit([field], false, async () => response.promise, {
		mode: "draft",
		allowEditsDuringRequest: true,
	});
	response.resolve();
	await saving;
	expect(form.acceptSavedBaseline({ title: "Edited" }, { title: "Edited" })).toBe(true);
	expect(form.editorEpoch).toBe(epoch);
	expect(invalidations).toBe(0);
});

it("rebases late group and keyed-row edits over server normalization without remounting rows", async () => {
	const text = (name: string): SchemaField => ({
		id: name,
		name,
		path: name,
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: name },
		text: {},
	});
	const fields: SchemaField[] = [
		{
			id: "group",
			name: "group",
			path: "group",
			type: "group",
			category: "nested",
			required: false,
			unique: false,
			admin: { label: "Group" },
			nested: { fields: [text("a"), text("b")] },
		},
		{
			id: "rows",
			name: "rows",
			path: "rows",
			type: "array",
			category: "nested",
			required: false,
			unique: false,
			admin: { label: "Rows" },
			nested: { fields: [text("a"), text("b")] },
		},
	];
	const form = new FormController();
	form.reset(
		{
			group: { a: "original", b: "old" },
			rows: [
				{ _key: "first", a: "original", b: "old" },
				{ _key: "second", a: "second", b: "old" },
			],
		},
		fields
	);
	const firstMount = form.rowMountKey((form.get("rows") as Record<string, unknown>[])[0]!);
	form.set("group.a", "submitted");
	form.set("rows.0.a", "submitted");
	const submitted = form.snapshot();
	const result = Promise.withResolvers<void>();
	const saving = form.submit(fields, false, async () => result.promise, {
		mode: "draft",
		allowEditsDuringRequest: true,
	});
	form.set("group.b", "typed later");
	form.set("rows.0.b", "typed later");
	form.setRows("rows", [...(form.snapshot().rows as Record<string, unknown>[])].reverse());
	result.resolve();
	await saving;
	form.acceptSavedBaseline(
		{
			group: { a: "normalized", b: "old" },
			rows: [
				{ _key: "first", a: "normalized", b: "old" },
				{ _key: "second", a: "second", b: "old" },
			],
		},
		submitted
	);
	const rows = form.get("rows") as Record<string, unknown>[];
	expect(form.get("group")).toEqual({ a: "normalized", b: "typed later" });
	expect(rows).toEqual([
		{ _key: "second", a: "second", b: "old" },
		{ _key: "first", a: "normalized", b: "typed later" },
	]);
	expect(form.rowMountKey(rows[1]!)).toBe(firstMount);
	expect(changedFormValues(fields, form.snapshot(), form.original)).toEqual({
		group: { b: "typed later" },
		rows: [{ _key: "second" }, { _key: "first", b: "typed later" }],
	});
});

const rebaseRowsField: SchemaField = {
	id: "rows",
	name: "rows",
	path: "rows",
	type: "array",
	category: "nested",
	required: false,
	unique: false,
	admin: { label: "Rows" },
	nested: {
		fields: [
			{
				id: "rows-text",
				name: "text",
				path: "rows.text",
				type: "text",
				category: "scalar",
				required: false,
				unique: false,
				admin: { label: "Text" },
				text: {},
			},
		],
	},
};

it.each(["insert", "reorder"] as const)(
	"accepts a saved server %s around a later row-field edit",
	(serverChange) => {
		const submitted = {
			rows: [
				{ _key: "a", text: "A" },
				{ _key: "b", text: "B" },
			],
		};
		const form = new FormController();
		form.reset(submitted, [rebaseRowsField]);
		const mount = form.rowMountKey((form.get("rows") as Record<string, unknown>[])[1]!);
		form.set("rows.1.text", "Typed after submit");
		const saved = {
			rows:
				serverChange === "insert"
					? [
							{ _key: "a", text: "Normalized A" },
							{ _key: "new", text: "Hook row" },
							{ _key: "b", text: "B" },
						]
					: [
							{ _key: "b", text: "B" },
							{ _key: "a", text: "Normalized A" },
						],
		};
		expect(form.acceptSavedBaseline(saved, submitted)).toBe(true);
		expect(form.get("rows")).toEqual(
			saved.rows.map((row) => (row._key === "b" ? { ...row, text: "Typed after submit" } : row))
		);
		const rows = form.get("rows") as Record<string, unknown>[];
		expect(form.rowMountKey(rows.find((row) => row._key === "b")!)).toBe(mount);
		expect(form.original).toEqual(saved);
	}
);

it.each(["remove", "replace", "concurrent structure"] as const)(
	"keeps all local values when a saved %s cannot safely rebase",
	(serverChange) => {
		const submitted = {
			rows: [
				{ _key: "a", text: "A" },
				{ _key: "b", text: "B" },
			],
		};
		const form = new FormController();
		form.reset(submitted, [rebaseRowsField]);
		form.set("rows.1.text", "My late edit");
		if (serverChange === "concurrent structure")
			form.setRows("rows", [...(form.get("rows") as Record<string, unknown>[])].reverse());
		const local = form.snapshot();
		const saved = {
			rows:
				serverChange === "remove"
					? [{ _key: "a", text: "A" }]
					: serverChange === "replace"
						? [
								{ _key: "a", text: "A" },
								{ _key: "replacement", text: "New" },
							]
						: [
								{ _key: "a", text: "A" },
								{ _key: "new", text: "Hook row" },
								{ _key: "b", text: "B" },
							],
		};
		expect(form.acceptSavedBaseline(saved, submitted)).toBe(false);
		expect(form.snapshot()).toEqual(local);
	}
);

it("rebases an embedded plugin leaf onto server-inserted and reordered occurrences", () => {
	const title: SchemaField = {
		id: "block-card-title",
		name: "title",
		path: "title",
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Title" },
		text: {},
	};
	const body = bindBlockField([card([title])], {
		id: "body",
		name: "body",
		path: "body",
		type: "plugin",
		category: "plugin",
		required: false,
		unique: false,
		admin: { label: "Body" },
		plugin: { key: "outline", config: {}, embeddedTrees: [tree()] },
	} satisfies SchemaField);
	const row = (uid: string, value: string) => ({
		kind: "widget",
		content: { schema: "card", uid, title: value },
	});
	const submitted = { body: { outline: [row("a", "A"), row("b", "B")] } };
	const form = new FormController();
	form.reset(submitted, [body]);
	form.set("body.outline.1.content.title", "Typed later");
	const saved = { body: { outline: [row("new", "Hook"), row("b", "B"), row("a", "A")] } };
	expect(form.acceptSavedBaseline(saved, submitted)).toBe(true);
	expect(form.get("body")).toEqual({
		outline: [row("new", "Hook"), row("b", "Typed later"), row("a", "A")],
	});
	expect(form.original).toEqual(saved);
	const removed = new FormController();
	removed.reset(submitted, [body]);
	removed.set("body.outline.1.content.title", "Typed later");
	expect(removed.acceptSavedBaseline({ body: { outline: [row("a", "A")] } }, submitted)).toBe(
		false
	);
	expect(removed.get("body")).toEqual({ outline: [row("a", "A"), row("b", "Typed later")] });
});

it.each(["reset", "reconcile"] as const)(
	"a stale submission cannot unlock a newer submission after %s",
	async (boundary) => {
		const field: SchemaField = {
			id: "title",
			name: "title",
			path: "title",
			type: "text",
			category: "scalar",
			required: false,
			unique: false,
			admin: { label: "Title" },
			text: {},
		};
		const form = new FormController();
		form.reset({ title: "First" }, [field]);
		const first = Promise.withResolvers<void>();
		const firstSave = form.submit([field], false, async () => first.promise);
		if (boundary === "reset") form.reset({ title: "Second" }, [field]);
		else form.reconcile([field], [field]);
		form.set("title", "Second edit");
		const second = Promise.withResolvers<void>();
		const secondSave = form.submit([field], false, async () => second.promise);
		first.resolve();
		await firstSave;
		expect(form.submitting).toBe(true);
		expect(form.editingBlocked).toBe(true);
		second.resolve();
		await secondSave;
		expect(form.submitting).toBe(false);
	}
);

describe("form controller field access", () => {
	it("uses a block definition's capabilities at a placement without its own entry", () => {
		const values = { layout: [{ _key: "new", blockType: "quote" }] };
		const form = new FormController(values);
		form.reset(values, [blocksField()]);
		const access = accessEnvelope();
		delete access.fields["layout.quote.secret"];
		delete access.fields["layout.0.secret"];
		access.blockFields = { quote: { secret: { read: true, create: false, update: false } } };
		form.setAccess(access, "update");
		expect(form.canRead("layout.0.secret", "layout.quote.secret")).toBe(true);
		expect(form.canWrite("layout.0.secret", "layout.quote.secret")).toBe(false);
		// A placement holding values keeps its own entry.
		access.fields["layout.quote.secret"] = { read: true, create: true, update: true };
		form.setAccess(access, "update");
		expect(form.canWrite("layout.0.secret", "layout.quote.secret")).toBe(true);
	});

	it("preserves mixed occurrence permissions and unsaved edits through reorder", async () => {
		const form = new FormController({
			layout: [
				{ _key: "allowed", blockType: "quote", secret: "Original" },
				{ _key: "protected", blockType: "quote", secret: "Protected" },
			],
		});
		const access = accessEnvelope();
		access.fields["layout.1.secret"] = { read: true, create: false, update: false };
		form.setAccess(access, "update");
		form.set("layout.0.secret", "Edited");
		const rows = form.snapshot().layout as Record<string, unknown>[];
		form.setRows("layout", [rows[1]!, rows[0]!]);
		expect(form.canWrite("layout.1.secret", "layout.quote.secret")).toBe(true);
		expect(form.canWrite("layout.0.secret", "layout.quote.secret")).toBe(false);
		expect(form.canRead("layout.0.secret", "layout.quote.secret")).toBe(true);
		const submitted = await form.submit([blocksField()], true, async (values) => values);
		expect(submitted).toEqual({
			layout: [
				{ _key: "protected", blockType: "quote" },
				{ _key: "allowed", blockType: "quote", secret: "Edited" },
			],
		});
	});

	it("moves nested capabilities with retained rows without granting them to insertions or replacements", () => {
		const form = new FormController({
			layout: [
				{ _key: "allowed", blockType: "quote", links: [{ _key: "first" }, { _key: "second" }] },
				{ _key: "removed", blockType: "quote" },
			],
		});
		const access = accessEnvelope();
		const allowed = { read: true, create: true, update: true };
		const denied = { read: false, create: false, update: false };
		access.fields["layout.0.links.0.label"] = allowed;
		access.fields["layout.0.links.1.label"] = denied;
		access.fields["layout.1.secret"] = allowed;
		form.setAccess(access, "update");
		const retained = (form.snapshot().layout as Record<string, unknown>[])[0]!;
		form.setRows("layout", [{ _key: "new", blockType: "quote" }, retained]);
		expect(form.access?.fields["layout.0.secret"]).toBeUndefined();
		expect(form.canWrite("layout.0.secret", "layout.quote.secret")).toBe(false);
		expect(form.access?.fields["layout.1.links.0.label"]).toEqual(allowed);
		expect(form.access?.fields["layout.1.links.1.label"]).toEqual(denied);
		form.setRows("layout.1.links", [{ _key: "second" }, { _key: "first" }, { _key: "copy" }]);
		expect(form.access?.fields["layout.1.links.0.label"]).toEqual(denied);
		expect(form.access?.fields["layout.1.links.1.label"]).toEqual(allowed);
		expect(form.access?.fields["layout.1.links.2.label"]).toBeUndefined();
		form.setRows("layout", [{ _key: "allowed", blockType: "other" }]);
		expect(form.access?.fields["layout.0.secret"]).toBeUndefined();
		expect(form.access?.fields["layout.1.secret"]).toBeUndefined();
		expect(form.access?.fields["layout.quote.secret"]).toEqual(denied);
		expect(form.access?.fields.title).toEqual(allowed);
	});

	it("does not transfer indexed permissions through ambiguous keys", () => {
		for (const rows of [
			[{}, {}],
			[{ _key: "duplicate" }, { _key: "duplicate" }],
		]) {
			const form = new FormController({ layout: rows });
			form.setAccess(accessEnvelope(), "update");
			form.setRows("layout", [...rows].reverse());
			expect(form.access?.fields["layout.0.secret"]).toBeUndefined();
		}
	});

	it("does not submit changed protected values or treat a new row as an unchanged occurrence", async () => {
		const form = new FormController({
			title: "Read only title",
			layout: [{ _key: "protected", blockType: "quote", secret: "Protected" }],
		});
		const denied = { read: true, create: false, update: false };
		const access = accessEnvelope();
		access.fields["layout.0.secret"] = denied;
		access.fields["layout.quote.secret"] = denied;
		form.setAccess(access, "update");
		form.set("layout.0.secret", "Forged edit");
		let submitted = await form.submit([blocksField()], false, async (values) => values);
		expect(submitted.layout).toEqual([{ _key: "protected", blockType: "quote" }]);
		form.setRows("layout", [{ _key: "new", blockType: "quote", secret: "Forged edit" }]);
		submitted = await form.submit([blocksField()], false, async (values) => values);
		expect(submitted.layout).toEqual([{ _key: "new", blockType: "quote" }]);
	});

	it("recovery cannot transfer removed occurrence grants to new rows and Discard restores removed denials", () => {
		const fields = [blocksField()];
		const original = {
			layout: [
				{ _key: "allowed", blockType: "quote", secret: "Public copy" },
				{ _key: "removed", blockType: "quote" },
			],
		};
		const form = new FormController();
		form.reset(original, fields);
		const access = accessEnvelope();
		access.fields["layout.1.secret"] = { read: false, create: false, update: false };
		form.setAccess(access, "update");
		const values = {
			layout: [{ _key: "new", blockType: "quote" }, original.layout[0]],
		};
		form.recover(reconcileFormSchema({ values, original }, fields, fields), fields);
		expect(form.access?.fields["layout.0.secret"]).toBeUndefined();
		expect(form.canRead("layout.0.secret", "layout.quote.secret")).toBe(false);
		expect(form.hasWriteAccess("layout.0.secret", "layout.quote.secret")).toBe(false);
		expect(form.canRead("layout.1.secret", "layout.quote.secret")).toBe(true);
		expect(form.hasWriteAccess("layout.1.secret", "layout.quote.secret")).toBe(true);
		form.discard();
		expect(form.snapshot()).toEqual(original);
		expect(form.access?.fields).toEqual(access.fields);
		expect(form.canRead("layout.1.secret", "layout.quote.secret")).toBe(false);
		expect(form.hasWriteAccess("layout.1.secret", "layout.quote.secret")).toBe(false);
	});

	it.each(["access", "schema", "resource", "locale", "reset"])(
		"Discard cannot reinstate pre-recovery grants after a new %s boundary",
		(boundary) => {
			const fields = [blocksField()];
			const original = {
				layout: [
					{ _key: "allowed", blockType: "quote", secret: "Public copy" },
					{ _key: "protected", blockType: "quote" },
				],
			};
			const form = new FormController();
			form.reset(original, fields);
			form.setAccess(accessEnvelope(), "update");
			const values = { layout: [...original.layout].reverse() };
			form.recover(reconcileFormSchema({ values, original }, fields, fields), fields);
			const access = accessEnvelope();
			delete access.fields["layout.0.secret"];
			if (boundary === "access") form.setAccess(access, "update");
			else {
				if (boundary === "schema") form.reconcile(fields, fields);
				if (boundary === "resource") form.setResource({ collection: "other" });
				if (boundary === "locale") form.setLocalization("fr");
				if (boundary === "reset") form.reset(form.snapshot(), fields);
				form.access = access;
			}
			form.discard();
			expect(form.canRead("layout.0.secret", "layout.quote.secret")).toBe(false);
			expect(form.hasWriteAccess("layout.0.secret", "layout.quote.secret")).toBe(false);
			expect(form.canRead("layout.1.secret", "layout.quote.secret")).toBe(false);
			expect(form.hasWriteAccess("layout.1.secret", "layout.quote.secret")).toBe(false);
		}
	);
});

describe("form controller derived text bindings", () => {
	it("keeps generated slugs following while the field has no mounted consumer", () => {
		const form = new FormController({ title: "Hello World", slug: "hello-world" });
		const binding = form.bindDerivedText(
			"slug",
			"title",
			(source) => normalizeSlug(String(source ?? "")),
			(current, source) => String(current ?? "") === "" || slugFollowsSource(current, source)
		);

		form.set("title", "Hidden Field Update");
		expect(form.get("slug")).toBe("hidden-field-update");

		binding.setManual("hand-authored");
		form.set("title", "Ignored Update");
		expect(form.get("slug")).toBe("hand-authored");

		binding.follow();
		form.set("title", "Following Again");
		expect(form.get("slug")).toBe("following-again");
	});

	it("clears keep-alive observers when the form resets", () => {
		const form = new FormController({ title: "One", slug: "one" });
		form.bindDerivedText(
			"slug",
			"title",
			(source) => normalizeSlug(String(source ?? "")),
			(current, source) => slugFollowsSource(current, source)
		);

		form.reset({ title: "Fresh", slug: "" });
		form.set("title", "No Stale Observer");
		expect(form.get("slug")).toBe("");
	});
});

function accessEnvelope(): AccessCapabilitiesEnvelope {
	return {
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
		fields: {
			"layout.quote.secret": { read: false, create: false, update: false },
			"layout.0.secret": { read: true, create: true, update: true },
			title: { read: true, create: true, update: true },
		},
	};
}

function blocksField(): SchemaField {
	const quote: SchemaBlockType = {
		slug: "quote",
		labels: { singular: "Quote", plural: "Quotes" },
		fields: [
			{
				id: "block-quote-secret",
				name: "secret",
				path: "secret",
				type: "text",
				category: "scalar",
				required: true,
				unique: false,
				admin: { label: "Secret" },
				text: {},
			},
		],
	};
	return bindBlockField([quote], {
		id: "layout",
		name: "layout",
		path: "layout",
		type: "blocks",
		category: "nested",
		required: false,
		unique: false,
		admin: { label: "Layout" },
		blocks: { blockReferences: ["quote"] },
	});
}

describe("missing block schema recovery", () => {
	it("preserves raw removed rows through schema reload and prevents saving their deletion", async () => {
		const previous = blocksField();
		const next = { ...previous, blocks: {} };
		const values = {
			layout: [
				{ _key: "keep", blockType: "quote", secret: "Stored content", extra: { retained: true } },
			],
		};
		const form = new FormController(values);
		form.reconcile([previous], [next]);
		expect(form.snapshot()).toEqual(values);
		expect(form.original).toEqual(values);
		form.set("layout", []);
		let called = false;
		await expect(
			form.submit([next], false, async () => {
				called = true;
			})
		).rejects.toThrow();
		expect(called).toBe(false);
		expect(form.issues[0]?.code).toBe("unknown_block_schema");
	});
});

it("keeps overlapping field registrations owned by their mounted consumers during reorder", () => {
	const form = new FormController();
	const first = form.register("layout.0.heading");
	const second = form.register("layout.1.heading");
	first();
	const movedFirst = form.register("layout.1.heading");
	second();
	const movedSecond = form.register("layout.0.heading");
	expect(form.isRegistered("layout.0.heading")).toBe(true);
	expect(form.isRegistered("layout.1.heading")).toBe(true);
	movedFirst();
	movedSecond();
	expect(form.isRegistered("layout.0.heading")).toBe(false);
	expect(form.isRegistered("layout.1.heading")).toBe(false);
});
