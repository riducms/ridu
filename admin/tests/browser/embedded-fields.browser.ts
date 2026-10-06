import { describe, expect, it } from "vitest";
import { resolveBlockTypes, type SchemaBlockType, type SchemaField } from "@riducms/protocol";
import { embeddedOccurrences } from "@admin/core/forms/embedded-fields";
import {
	changedFormValues,
	initialFormValues,
	reconcileFormSchema,
	shouldSubmitLocalizedPath,
	submissionFormValues,
} from "@admin/core/forms/form-schema";
import { validateFormValues, unknownBlockIssues } from "@admin/core/forms/form-validation";
import { cloneFieldForPaste } from "@admin/fields/field-clipboard";
import { createAdminI18n } from "@riducms/translations";
import { RiduError } from "@riducms/sdk";
import { evaluateFieldCondition } from "@admin/core/forms/field-condition";
import { relationshipOptionFilters } from "@admin/fields/relationship/relationship-query";

import { bindBlockField, blockDefinition } from "../block-manifest";

// Card definition fields take definition-relative paths; their placement in `body`
// gives them canonical paths such as `body.widgets.widget.card.title`.
const titlePath = "body.widgets.widget.card.title";
const title: SchemaField = {
	id: "block-card-title",
	name: "title",
	path: "title",
	type: "text",
	category: "scalar",
	required: true,
	unique: false,
	localized: true,
	admin: { label: "Title" },
	text: {},
};
const caption: SchemaField = {
	...title,
	id: "block-card-settings-caption",
	name: "caption",
	path: "settings.caption",
	required: false,
	default: "Default caption",
};
const group: SchemaField = {
	id: "block-card-settings",
	name: "settings",
	path: "settings",
	type: "group",
	category: "nested",
	required: false,
	unique: false,
	admin: { label: "Settings" },
	nested: { fields: [caption] },
};
const json: SchemaField = {
	...title,
	id: "block-card-raw",
	name: "raw",
	path: "raw",
	type: "json",
	localized: false,
	required: false,
};
/** A plugin field whose `widget` items select the `slugs` block definitions. */
function outline(name: string, slugs: string[]): SchemaField {
	return {
		id: name,
		name,
		path: name,
		type: "plugin",
		category: "plugin",
		required: false,
		unique: false,
		admin: { label: "Body" },
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
							blockReferences: slugs,
						},
					],
				},
			],
		},
	};
}
/**
 * Binds `body`, whose widgets select the `card` definition with `cardFields` and
 * any further `variants`; `blocks` are definitions that card fields select.
 */
function bodyField(
	cardFields = [title, group, json],
	variants: SchemaBlockType[] = [],
	blocks: SchemaBlockType[] = []
) {
	return bindBlockField(
		[
			blockDefinition("card", cardFields, { singular: "Card", plural: "Cards" }),
			...variants,
			...blocks,
		],
		outline("body", ["card", ...variants.map((variant) => variant.slug)])
	);
}
/** A card's nested plugin field selects its own definition; a block cannot contain itself. */
const innerCard = blockDefinition("inner-card", [title, group, json]);
function placedCardField(body: SchemaField, name: string) {
	return resolveBlockTypes(body.plugin!.embeddedTrees![0]!.cases[0])[0]!.fields.find(
		(child) => child.name === name
	)!;
}
const field = bodyField();
const card = (uid: string, text = "Hello") => ({
	kind: "widget",
	content: {
		uid,
		schema: "card",
		title: text,
		raw: { kind: "widget", content: { uid: "same", schema: "retired" } },
	},
});
const envelope = (...items: unknown[]) => ({ outline: [{ kind: "section", items }] });
const path = "body.outline.0.items.0.content";

async function draftRuntime() {
	return {
		...(await import("@admin/core/forms/form-controller.svelte")),
		...(await import("@admin/core/forms/embedded-schema-draft.svelte")),
	};
}

describe("public embedded draft host", () => {
	it("reports only writes inside the mounted item and follows it through reorders", async () => {
		const { FormController } = await draftRuntime();
		const { observeEmbeddedSchemaForm } = await import("@admin/core/forms/embedded-schema-form");
		const parent = new FormController({ body: envelope(card("one"), card("two", "Two")) });
		parent.reset(parent.snapshot(), [field]);
		const changes: Record<string, unknown>[] = [];
		let writable = true;
		const stop = observeEmbeddedSchemaForm(
			parent,
			field,
			"widgets",
			"one",
			(payload) => changes.push(payload),
			() => writable
		);
		expect(changes).toEqual([]);

		parent.set("body.outline.0.items.1.content.title", "Sibling edit");
		expect(changes).toEqual([]);
		parent.set(`${path}.title`, "Inline edit");
		expect(changes).toHaveLength(1);
		expect(changes[0]?.title).toBe("Inline edit");
		changes[0]!.title = "Mutated copy";
		expect(parent.get(`${path}.title`)).toBe("Inline edit");

		parent.set("body", envelope(card("two", "Sibling edit"), card("one", "External")));
		expect(changes).toHaveLength(1);
		parent.set("body.outline.0.items.1.content.title", "After reorder");
		expect(changes[1]?.title).toBe("After reorder");
		writable = false;
		parent.set("body.outline.0.items.1.content.title", "Locked");
		expect(changes).toHaveLength(2);
		writable = true;
		parent.set("body.outline.0.items.1.content.title", "Unlocked");
		expect(changes[2]?.title).toBe("Unlocked");

		parent.set("body", envelope(card("two")));
		parent.set("body", envelope(card("two"), card("one", "Reinserted")));
		const fresh: Record<string, unknown>[] = [];
		const stopFresh = observeEmbeddedSchemaForm(
			parent,
			field,
			"widgets",
			"one",
			(payload) => fresh.push(payload),
			() => true
		);
		parent.set("body.outline.0.items.1.content.title", "New mount");
		expect(changes).toHaveLength(3);
		expect(fresh[0]?.title).toBe("New mount");
		stopFresh();
		parent.set("body.outline.0.items.1.content.title", "After cleanup");
		expect(fresh).toHaveLength(1);
		stop();
	});

	it("creates independent inline payloads with configured defaults", async () => {
		const { createEmbeddedSchemaPayload } = await draftRuntime();
		const scope = { treeKey: "widgets", caseTag: "widget", variantSlug: "card" };
		const first = createEmbeddedSchemaPayload(field, scope);
		const second = createEmbeddedSchemaPayload(field, scope);
		expect(first.schema).toBe("card");
		expect(typeof first.uid).toBe("string");
		expect(first.uid).not.toBe(second.uid);
		expect(first.settings).toEqual({ caption: "Default caption" });
	});

	it("retains live document context for conditions and reference filters while restricting writes to the draft", async () => {
		const { FormController, createEmbeddedSchemaDraft } = await draftRuntime();
		const parent = new FormController({ tenant: "a", body: envelope(card("one")) });
		const session = createEmbeddedSchemaDraft(
			parent,
			() => field,
			{ treeKey: "widgets", identity: "one" },
			createAdminI18n()
		);
		expect(
			evaluateFieldCondition(
				{
					kind: "predicate",
					predicate: {
						scope: "document",
						path: "tenant",
						operator: "equals",
						values: [{ type: "string", value: "a" }],
					},
				},
				session.fields[0]!.path,
				(path) => session.form.get(path)
			)
		).toBe(true);
		const relation: SchemaField = {
			...title,
			relationship: {
				collectionId: "targets",
				collectionSlug: "targets",
				onDelete: "restrict",
				optionFilters: [{ targetPath: "tenant", sourcePath: "tenant", operator: "equals" }],
			},
		};
		expect(
			relationshipOptionFilters(relation, (path) => session.form.get(path), "targets")
		).toEqual([{ field: "tenant", operator: "equals", value: "a" }]);
		parent.set("tenant", "b");
		expect(session.form.get("tenant")).toBe("b");
		expect(session.form.snapshot().tenant).toBe("b");
		expect(() => session.form.set("tenant", "forbidden")).toThrow("scoped draft");
		expect(parent.get("tenant")).toBe("b");
		session.draft.discard();
		expect(() =>
			createEmbeddedSchemaDraft(
				parent,
				() => field,
				{ treeKey: "widgets", identity: "one", readOnly: true },
				createAdminI18n()
			)
		).toThrow("read-only");
	});
	it("invalidates a draft when occurrence access changes, without invalidating a permitted reorder", async () => {
		const { FormController, createEmbeddedSchemaDraft } = await draftRuntime();
		const parent = new FormController({ body: envelope(card("one"), card("two")) });
		const allowed = { read: true, create: true, update: true };
		const access = {
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
			fields: { [`${path}.title`]: allowed },
		};
		parent.setAccess(access, "update");
		const session = createEmbeddedSchemaDraft(
			parent,
			() => field,
			{ treeKey: "widgets", identity: "one" },
			createAdminI18n()
		);
		parent.setEmbedded(field, envelope(card("two"), card("one")));
		expect(session.draft.stale).toBe(false);
		parent.setAccess(
			{
				...access,
				fields: {
					"body.outline.0.items.1.content.title": { read: false, create: false, update: false },
				},
			},
			"update"
		);
		expect(session.draft.stale).toBe(true);
		expect(session.draft.validate()).toBe(false);
		session.draft.discard();
	});
	it("keeps insertion/defaults detached, validates ordinary fields, and blocks outer Save until resolved", async () => {
		const { FormController, createEmbeddedSchemaDraft } = await draftRuntime();
		const parent = new FormController({ body: envelope() });
		const initial = parent.snapshot();
		const session = createEmbeddedSchemaDraft(
			parent,
			() => field,
			{ treeKey: "widgets", caseTag: "widget", variantSlug: "card" },
			createAdminI18n()
		);
		expect(session.draft.payload()).toMatchObject({
			schema: "card",
			settings: { caption: "Default caption" },
		});
		expect(session.draft.identity).not.toBe("");
		expect(parent.snapshot()).toEqual(initial);
		expect(parent.dirty).toBe(true);
		expect(session.draft.validate()).toBe(false);
		expect(session.draft.issues[0]?.path).toBe(`${session.draft.id}.title`);
		let submitted = false;
		await expect(
			parent.submit([field], true, async () => {
				submitted = true;
			})
		).rejects.toThrow();
		expect(submitted).toBe(false);
		expect(parent.issues[0]?.code).toBe("pending_embedded_edit");
		session.form.set(`${session.draft.id}.title`, "Valid");
		expect(session.draft.validate()).toBe(true);
		expect(parent.snapshot()).toEqual(initial);
		session.draft.discard();
		expect(parent.dirty).toBe(false);
		await parent.submit([field], true, async () => {
			submitted = true;
		});
		expect(submitted).toBe(true);
	});
	it("retains a draft through reorder but refuses deletion, same-key replacement, locale and revision changes", async () => {
		const { FormController, createEmbeddedSchemaDraft } = await draftRuntime();
		for (const change of ["delete", "replace", "locale", "revision"]) {
			const parent = new FormController({ body: envelope(card("one"), card("two")) });
			const session = createEmbeddedSchemaDraft(
				parent,
				() => field,
				{ treeKey: "widgets", identity: "one" },
				createAdminI18n()
			);
			session.form.set(`${session.draft.id}.title`, "Detached edit");
			parent.setEmbedded(field, envelope(card("two"), card("one")));
			expect(session.draft.stale).toBe(false);
			expect(session.draft.validate()).toBe(true);
			if (change === "delete") parent.setEmbedded(field, envelope(card("two")));
			if (change === "replace") parent.setEmbedded(field, envelope(card("one", "Replaced")));
			if (change === "locale") parent.setLocalization("fr");
			if (change === "revision") parent.reset(parent.snapshot());
			expect(session.draft.stale).toBe(true);
			expect(session.draft.validate()).toBe(false);
			expect(session.draft.issues[0]?.code).toBe("stale_embedded_edit");
			session.draft.discard();
		}
	});
	it("uses canonical and occurrence access in the detached form and preserves redacted missing values", async () => {
		const { FormController, createEmbeddedSchemaDraft } = await draftRuntime();
		const node = card("one");
		const parent = new FormController({ body: envelope(node) });
		parent.setAccess(
			{
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
				fields: { [titlePath]: { read: false, create: false, update: false } },
			},
			"update"
		);
		const session = createEmbeddedSchemaDraft(
			parent,
			() => field,
			{ treeKey: "widgets", identity: "one" },
			createAdminI18n()
		);
		expect(session.form.canRead(`${session.draft.id}.title`, titlePath)).toBe(false);
		expect(session.form.canWrite(`${session.draft.id}.title`, titlePath)).toBe(false);
		expect(session.draft.payload()).toEqual(node.content);
		session.draft.discard();
	});
	it("copies only schema identities and leaves lookalike opaque JSON alone", async () => {
		const { copyEmbeddedSchemaPayload } = await draftRuntime();
		const source = card("one").content;
		const copied = copyEmbeddedSchemaPayload(
			field,
			{ treeKey: "widgets", caseTag: "widget", variantSlug: "card" },
			source
		);
		expect(copied.uid).not.toBe(source.uid);
		expect(copied.raw).toEqual(source.raw);
		expect(source.uid).toBe("one");
	});
});

describe("embedded server issue identity", () => {
	it("does not mark an unsent edit as saved after a deferred success", async () => {
		const { FormController } = await draftRuntime();
		const parent = new FormController({ body: envelope(card("one")) });
		const completion = Promise.withResolvers<void>();
		const submitting = parent.submit([field], true, () => completion.promise);
		parent.setEmbedded(field, envelope(card("one", "Not submitted")));
		completion.resolve();
		await submitting;
		expect(parent.dirty).toBe(true);
		expect(parent.submitting).toBe(false);
	});
	it("keeps current errors on their occurrence through structural edits", async () => {
		const { FormController } = await draftRuntime();
		const parent = new FormController({ body: envelope(card("one"), card("two")) });
		parent.issues = [{ code: "validation", path: `${path}.title`, message: "Rejected title" }];
		parent.setEmbedded(field, envelope(card("two"), card("one")));
		expect(parent.issues[0]?.path).toBe("body.outline.0.items.1.content.title");
		parent.setEmbedded(field, envelope(card("two"), card("one", "Corrected")));
		expect(parent.issues).toEqual([]);
	});
	it("correlates a late validation response and drops errors for edited or removed occurrences", async () => {
		const { FormController } = await draftRuntime();
		for (const mode of ["reorder", "edit", "remove"]) {
			const parent = new FormController({ body: envelope(card("one"), card("two")) });
			const completion = Promise.withResolvers<never>();
			const submitting = parent.submit([field], true, () => completion.promise);
			parent.setEmbedded(
				field,
				mode === "reorder"
					? envelope(card("two"), card("one"))
					: mode === "edit"
						? envelope(card("one", "Corrected"), card("two"))
						: envelope(card("two"))
			);
			completion.reject(
				new RiduError({
					code: "validation",
					status: 422,
					message: "Invalid",
					issues: [{ code: "validation", path: `${path}.title`, message: "Rejected title" }],
				})
			);
			await expect(submitting).rejects.toThrow("Invalid");
			expect(parent.issues.map((issue) => issue.path)).toEqual(
				mode === "reorder" ? ["body.outline.0.items.1.content.title"] : []
			);
		}
	});
});

describe("declarative embedded admin fields", () => {
	it("patches schema payloads by identity while preserving the surrounding plugin envelope", () => {
		const before = { body: envelope(card("a"), card("b")) };
		const copy = card("new");
		const after = { body: envelope(card("b", "Edited"), card("a"), copy) };
		const patch = changedFormValues([field], after, before);
		expect(patch).toEqual({
			body: envelope(
				{ kind: "widget", content: { uid: "b", schema: "card", title: "Edited" } },
				{ kind: "widget", content: { uid: "a", schema: "card" } },
				copy
			),
		});
		expect(changedFormValues([field], before, before)).toEqual({});
	});

	it("visits only declared envelope edges and returns exact paths once", () => {
		const value = envelope(card("first"), card("second"));
		const found = embeddedOccurrences(field, value);
		expect(found.issues).toEqual([]);
		expect(found.occurrences.map((entry) => [entry.path, entry.identity])).toEqual([
			[path, "first"],
			["body.outline.0.items.1.content", "second"],
		]);
		expect(
			embeddedOccurrences({ ...field, type: "json", plugin: undefined }, value).occurrences
		).toEqual([]);
	});
	it("runs ordinary defaults/validation and preserves canonical access paths", () => {
		expect(initialFormValues([group])).toEqual({ settings: { caption: "Default caption" } });
		expect(
			validateFormValues([field], { body: envelope(card("first", "")) }, { requireMissing: true })
		).toEqual([{ code: "required", path: `${path}.title`, message: "Title is required" }]);
		const filtered = submissionFormValues(
			[field],
			{ body: envelope(card("first")) },
			(_runtime, canonical) => canonical !== titlePath
		);
		const payload = embeddedOccurrences(field, filtered.body).occurrences[0]?.payload;
		expect(payload).toEqual({ uid: "first", schema: "card", raw: card("first").content.raw });
		expect(
			validateFormValues(
				[field],
				{ body: envelope(card("first", "")) },
				{ requireMissing: true, include: (_runtime, canonical) => canonical !== titlePath }
			)
		).toEqual([]);
	});
	it("rejects unknown payloads, malformed and duplicate identities with exact paths", () => {
		const invalid = card("a");
		invalid.content.schema = "retired";
		expect(unknownBlockIssues([field], { body: envelope(invalid) })[0]).toMatchObject({
			code: "unknown_block_schema",
			path: `${path}.schema`,
		});
		expect(embeddedOccurrences(field, envelope(card(" "))).issues[0]).toMatchObject({
			code: "invalid_key",
			path: `${path}.uid`,
		});
		expect(embeddedOccurrences(field, envelope(card("a"), card("a"))).issues[0]).toMatchObject({
			code: "duplicate_key",
			path: "body.outline.0.items.1.content.uid",
		});
		expect(() => submissionFormValues([field], { body: envelope(invalid) })).toThrow(
			"Restore the missing embedded schema"
		);
	});
	it("keeps localization inheritance with stable identity across reorder", () => {
		const before = { body: envelope(card("a", "Inherited"), card("b", "French")) };
		const after = { body: envelope(card("b", "French"), card("a", "Inherited")) };
		const sources = { [`${path}.title`]: "en", "body.outline.0.items.1.content.title": "fr" };
		expect(
			shouldSubmitLocalizedPath(
				"body.outline.0.items.1.content.title",
				after,
				before,
				"fr",
				sources,
				[field]
			)
		).toBe(false);
		expect(shouldSubmitLocalizedPath(`${path}.title`, after, before, "fr", sources, [field])).toBe(
			true
		);
		expect(
			shouldSubmitLocalizedPath(
				`${path}.title`,
				{ body: envelope(card("new", "Inherited")) },
				before,
				"fr",
				sources,
				[field]
			)
		).toBe(true);
	});
	it("reconciles added ordinary defaults without changing identities or arbitrary JSON", () => {
		const previous = bodyField([title, json]);
		const value = { body: envelope(card("first")) };
		const result = reconcileFormSchema({ values: value, original: value }, [previous], [field], {
			initializeDefaults: true,
		});
		expect(embeddedOccurrences(field, result.values.body).occurrences[0]?.payload).toEqual({
			...card("first").content,
			settings: { caption: "Default caption" },
		});
		expect(result.detached).toEqual([]);
	});
	it("copies new identities only for declared payloads and ordinary descendants", () => {
		const value = envelope(card("first"));
		const copy = cloneFieldForPaste(field, value);
		const payload = embeddedOccurrences(field, copy).occurrences[0]?.payload;
		expect(payload?.uid).not.toBe("first");
		expect(payload?.raw).toEqual(card("first").content.raw);
		expect(embeddedOccurrences(field, value).occurrences[0]?.identity).toBe("first");
	});
	it("bounds deep and broad trees and rejects unsupported metadata", () => {
		let node: unknown = card("last");
		for (let i = 0; i < 70; i++) node = { kind: "section", items: [node] };
		expect(embeddedOccurrences(field, { outline: [node] }).issues[0]?.code).toBe("embedded_budget");
		expect(
			embeddedOccurrences(
				field,
				envelope(...Array.from({ length: 10_001 }, () => ({ kind: "text" })))
			).issues[0]?.code
		).toBe("embedded_budget");
		const incompatible = bodyField();
		incompatible.plugin!.embeddedTrees![0]!.version = 9;
		expect(embeddedOccurrences(incompatible, envelope(card("first"))).issues[0]?.message).toContain(
			"Unsupported embedded tree metadata version 9"
		);
	});
});

it("reconciliation preserves content authored under a newly added embedded variant", () => {
	const next = bodyField(undefined, [
		blockDefinition("new-card", [title], { singular: "New card", plural: "New cards" }),
	]);
	const node = card("new");
	node.content.schema = "new-card";
	const values = { body: envelope(node) };
	const recovered = reconcileFormSchema(
		{ values, original: { body: envelope(card("old")) } },
		[field],
		[next]
	);
	expect(embeddedOccurrences(next, recovered.values.body).occurrences[0]?.payload).toEqual(
		node.content
	);
});

it("enters nested plugin fields through ordinary schemas without traversing lookalike JSON", () => {
	const parent = bodyField(
		[title, group, json, outline("nested", ["inner-card"])],
		[],
		[innerCard]
	);
	const nested = placedCardField(parent, "nested");
	expect(nested.path).toBe("body.widgets.widget.card.nested");
	const inner = card("inner", "");
	inner.content.schema = "inner-card";
	const value = envelope({
		...card("outer"),
		content: { ...card("outer").content, nested: envelope(inner) },
	});
	const issues = validateFormValues([parent], { body: value }, { requireMissing: true });
	expect(issues).toHaveLength(1);
	expect(issues[0]?.path).toBe(`${path}.nested.outline.0.items.0.content.title`);
	const copied = cloneFieldForPaste(parent, value);
	const outer = embeddedOccurrences(parent, copied).occurrences[0]!;
	expect(outer.identity).not.toBe("outer");
	expect(embeddedOccurrences(nested, outer.payload.nested).occurrences[0]?.identity).not.toBe(
		"inner"
	);
	expect(outer.payload.raw).toEqual(card("outer").content.raw);
});

it("the parent controller retains embedded access and localization through structural edits", async () => {
	const { FormController } = await import("@admin/core/forms/form-controller.svelte");
	const form = new FormController();
	form.reset({ body: envelope(card("a", "Inherited"), card("b", "Protected")) }, [field]);
	form.setLocalization("fr", { [`${path}.title`]: "en" });
	form.setAccess(
		{
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
				[titlePath]: { read: true, create: true, update: false },
				[`${path}.title`]: { read: true, create: true, update: true },
				"body.outline.0.items.1.content.title": { read: true, create: false, update: false },
			},
		},
		"update"
	);
	form.setEmbedded(field, envelope(card("b", "Protected"), card("a", "Inherited")));
	expect(form.canWrite(`${path}.title`, titlePath)).toBe(false);
	expect(form.canWrite("body.outline.0.items.1.content.title", titlePath)).toBe(true);
	expect(form.localizationSource("body.outline.0.items.1.content.title")).toBe("en");
	const submitted = await form.submit([field], false, async (values) => values);
	expect(
		embeddedOccurrences(field, submitted.body).occurrences.every(
			(entry) => entry.payload.title === undefined
		)
	).toBe(true);
});

it("accepts null children and counts literal root segments in its depth limit", () => {
	const value = { outline: [{ ...card("null-children"), items: null }] };
	expect(embeddedOccurrences(field, value).issues).toEqual([]);
	expect(validateFormValues([field], { body: value }, { requireMissing: true })).toEqual([]);
	const nested = (count: number) => {
		let node: unknown = { kind: "text" };
		for (let index = 1; index < count; index++) node = { kind: "section", items: [node] };
		return { outline: [node] };
	};
	expect(embeddedOccurrences(field, nested(63)).issues).toEqual([]);
	expect(embeddedOccurrences(field, nested(64)).issues[0]?.code).toBe("embedded_budget");
});

it("initializes only inserted embedded payloads through the ordinary form defaults", async () => {
	const { FormController } = await import("@admin/core/forms/form-controller.svelte");
	const rows: SchemaField = {
		...group,
		id: "block-card-rows",
		name: "rows",
		path: "rows",
		type: "array",
		nested: { minRows: 1, fields: [caption] },
	};
	const schema = bodyField([title, group, json, rows]);
	const retained = card("retained");
	const form = new FormController();
	form.reset({ body: envelope(retained) }, [schema]);
	const inserted = {
		...card("new"),
		content: { ...card("new").content, settings: { custom: "supplied" } },
	};
	const explicitNull = {
		...card("null"),
		content: { ...card("null").content, settings: null, rows: [] },
	};
	const input = envelope(inserted, explicitNull, retained);
	form.setEmbedded(schema, input);
	const payloads = embeddedOccurrences(schema, form.get("body")).occurrences.map((o) => o.payload);
	expect(payloads[0]?.settings).toEqual({ caption: "Default caption", custom: "supplied" });
	expect(payloads[0]?.rows).toEqual([{ _key: expect.any(String), caption: "Default caption" }]);
	expect(payloads[0]?.raw).toEqual(card("new").content.raw);
	expect(payloads[1]?.settings).toBeNull();
	expect(payloads[1]?.rows).toEqual([]);
	// Retained omissions can be missing translations or redacted fields.
	expect(payloads[2]?.settings).toBeUndefined();
	expect(payloads[2]?.rows).toBeUndefined();
	expect(inserted.content.settings).toEqual({ custom: "supplied" });
	expect(form.original).toEqual({ body: envelope(retained) });
	expect(form.dirty).toBe(true);
	form.set("body.outline.0.items.0.content.settings.caption", "Edited default");
	const current = form.get("body") as { outline: { items: unknown[] }[] };
	form.setEmbedded(schema, envelope(...current.outline[0]!.items.toReversed()));
	const reordered = embeddedOccurrences(schema, form.get("body")).occurrences;
	expect(reordered[0]?.payload.settings).toBeUndefined();
	expect(reordered[2]?.payload.settings).toEqual({ caption: "Edited default", custom: "supplied" });
	expect(reordered[2]?.payload.rows).toEqual(payloads[0]?.rows);
});

it("initializes supplied array, block and nested plugin children in a new payload", async () => {
	const { FormController } = await import("@admin/core/forms/form-controller.svelte");
	const schema = bodyField(
		[
			title,
			group,
			json,
			{ ...group, name: "rows", type: "array", nested: { fields: [caption] } },
			{
				...group,
				name: "blocks",
				type: "blocks",
				nested: undefined,
				blocks: { blockReferences: ["nested"] },
			},
			outline("nested", ["inner-card"]),
		],
		[],
		[blockDefinition("nested", [caption], { singular: "Nested", plural: "Nesteds" }), innerCard]
	);
	const form = new FormController();
	const inner = card("inner");
	inner.content.schema = "inner-card";
	const node = {
		...card("new"),
		content: {
			...card("new").content,
			rows: [{ _key: "row" }, { _key: "explicit", caption: null }],
			blocks: [{ _key: "block", blockType: "nested" }],
			nested: envelope(inner),
		},
	};
	form.setEmbedded(schema, envelope(node));
	const payload = embeddedOccurrences(schema, form.get("body")).occurrences[0]!.payload;
	expect(payload.rows).toEqual([
		{ _key: "row", caption: "Default caption" },
		{ _key: "explicit", caption: null },
	]);
	expect(payload.blocks).toEqual([
		{ _key: "block", blockType: "nested", caption: "Default caption" },
	]);
	expect(
		embeddedOccurrences(placedCardField(schema, "nested"), payload.nested).occurrences[0]?.payload
			.settings
	).toEqual({ caption: "Default caption" });
});
