import { describe, expect, it } from "vitest";
import type { SchemaField } from "@riducms/protocol";
import type {
	FieldAuthoringHost,
	FieldDocument,
	FieldReferenceBrowserProps,
} from "@riducms/plugin";
import { FieldEditorBinding } from "@admin/core/forms/field-editor-binding";
import { guardEditorAuthoring } from "@admin/core/forms/field-editor-authoring";

import { FormController } from "@admin/core/forms/form-controller.svelte";

function schema(path = "rows.0.title"): SchemaField {
	return {
		id: "title-first",
		name: "title",
		path,
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: "Title", editor: { reference: "app:title" } },
		text: {},
	};
}
function rowsSchema(fields = [schema("rows.title")]): SchemaField {
	return {
		...schema("rows"),
		id: "rows",
		name: "rows",
		type: "array",
		category: "nested",
		admin: { label: "Rows" },
		nested: { fields },
	};
}
function fixture() {
	const form = new FormController({
		rows: [
			{ _key: "first", blockType: "card", title: "First" },
			{ _key: "second", blockType: "card", title: "Second" },
		],
	});
	form.reset(form.snapshot(), [rowsSchema()]);
	form.setResource({ collection: "posts", id: "one" });
	const field = schema();
	return { form, field, binding: new FieldEditorBinding(form, () => field, "text") };
}

describe("host-owned scalar editor lifetime", () => {
	it("defers native required input only for omitted dynamic defaults", () => {
		const field = { ...schema("title"), required: true, dynamicDefault: true };
		const form = new FormController({});
		form.reset({}, [field]);
		const binding = new FieldEditorBinding(form, () => field, "text");
		expect(binding.value).toBeUndefined();
		expect(binding.inputProps.required).toBe(false);
		binding.set("");
		expect(binding.inputProps.required).toBe(true);
		binding.set(null);
		expect(binding.inputProps.required).toBe(true);
		binding.destroy();
	});
	it("allows a custom required checkbox to submit a persisted false default again", () => {
		const field: SchemaField = {
			...schema("featured"),
			name: "featured",
			type: "checkbox",
			required: true,
			dynamicDefault: true,
		};
		// A successful server default returns false. The next form starts with it present.
		const form = new FormController({ featured: false });
		form.reset(form.snapshot(), [field]);
		const binding = new FieldEditorBinding(form, () => field, "checkbox");
		const input = document.createElement("input");
		input.type = "checkbox";
		input.name = binding.inputProps.name;
		input.required = binding.inputProps.required;
		input.checked = binding.value === true;
		expect(binding.schema.required).toBe(true);
		expect(binding.value).toBe(false);
		expect(input.checkValidity()).toBe(true);
		binding.destroy();
	});
	it("resolves nested identities synchronously before Svelte updates props", () => {
		const { form, binding } = fixture();
		const delayed = binding.set;
		const rows = form.snapshot().rows as Record<string, unknown>[];
		form.setRows("rows", rows.toReversed());
		delayed("Changed first");
		expect(form.get("rows.1.title")).toBe("Changed first");
		expect(form.get("rows.0.title")).toBe("Second");
		expect(binding.schema.path).toBe("rows.1.title");
		expect(form.isRegistered("rows.0.title")).toBe(false);
		expect(form.isRegistered("rows.1.title")).toBe(true);
		binding.destroy();
		expect(form.isRegistered("rows.1.title")).toBe(false);
	});

	it("follows every enclosing row key through multi-level reorder", () => {
		const form = new FormController({
			rows: [
				{
					_key: "a",
					children: [
						{ _key: "x", title: "X" },
						{ _key: "y", title: "Y" },
					],
				},
				{ _key: "b", children: [] },
			],
		});
		form.reset(form.snapshot(), [
			rowsSchema([
				{
					...rowsSchema([schema("rows.children.title")]),
					id: "children",
					name: "children",
					path: "rows.children",
				},
			]),
		]);
		const binding = new FieldEditorBinding(form, () => schema("rows.0.children.1.title"), "text");
		form.setRows("rows", (form.snapshot().rows as Record<string, unknown>[]).toReversed());
		form.setRows(
			"rows.1.children",
			(form.get("rows.1.children") as Record<string, unknown>[]).toReversed()
		);
		binding.set("Updated Y");
		expect(form.get("rows.1.children.0.title")).toBe("Updated Y");
		expect(form.get("rows.1.children.1.title")).toBe("X");
		binding.destroy();
	});

	for (const transition of [
		"remove",
		"replace variant",
		"duplicate key",
		"reset",
		"resource",
		"locale",
		"schema",
		"unmount",
	] as const) {
		it(`permanently revokes a delayed capability after ${transition}`, () => {
			const { form, binding } = fixture();
			const before = form.snapshot();
			const delayed = binding.set;
			const read = binding.form.get;
			switch (transition) {
				case "remove":
					form.setRows("rows", []);
					form.setRows("rows", before.rows as Record<string, unknown>[]);
					break;
				case "replace variant":
					form.setRows("rows", [{ _key: "first", blockType: "different", title: "Replacement" }]);
					break;
				case "duplicate key":
					form.setRows("rows", [{ _key: "first" }, { _key: "first" }]);
					break;
				case "reset":
					form.reset(before);
					break;
				case "resource":
					form.setResource({ collection: "posts", id: "two" });
					form.setResource({ collection: "posts", id: "one" });
					break;
				case "locale":
					form.setLocalization("fr");
					form.setLocalization(undefined);
					break;
				case "schema":
					form.reconcile([], []);
					break;
				case "unmount":
					binding.destroy();
					break;
			}
			const values = form.snapshot();
			expect(binding.stale).toBe(true);
			expect(() => delayed("Wrong document")).toThrow("stale");
			expect(() => read("rows.0.title")).toThrow("stale");
			expect(form.snapshot()).toEqual(values);
			expect(form.isRegistered("rows.0.title")).toBe(false);
		});
	}

	it("checks current access, submission and scalar values on every write", () => {
		const { form, binding, field } = fixture();
		form.writeBlocked = true;
		expect(() => binding.set("Blocked")).toThrow("read-only");
		form.writeBlocked = false;
		form.submitting = true;
		expect(binding.readOnly).toBe(true);
		expect(binding.schema.admin.readOnly).not.toBe(true);
		expect(() => binding.set("Blocked")).toThrow("read-only");
		form.submitting = false;
		field.admin.readOnly = true;
		expect(() => binding.set("Blocked")).toThrow("read-only");
		field.admin.readOnly = false;
		expect(() => {
			// @ts-expect-error runtime callers must still supply the declared scalar type
			binding.set(12);
		}).toThrow("string");
		field.admin.description = "Keep this guidance visible.";
		expect(binding.inputProps).toMatchObject({
			"aria-invalid": false,
			"aria-describedby": "title-first-description",
			"aria-errormessage": undefined,
		});
		form.issues = [{ code: "invalid", path: "rows.0.title", message: "Server error" }];
		expect(binding.inputProps).toMatchObject({
			"aria-invalid": true,
			"aria-describedby": "title-first-description title-first-error",
			"aria-errormessage": "title-first-error",
		});
		binding.set(null);
		expect(binding.value).toBeNull();
		expect(binding.issues).toEqual([]);
		expect(binding.inputProps).toMatchObject({
			"aria-invalid": false,
			"aria-describedby": "title-first-description",
			"aria-errormessage": undefined,
		});
		binding.destroy();
	});

	it("revokes as soon as submission replaces the baseline, before route reset", async () => {
		const form = new FormController({ title: "Original" });
		const field = schema("title");
		form.reset(form.snapshot(), [field]);
		const binding = new FieldEditorBinding(form, () => field, "text");
		binding.set("Saved");
		expect(form.dirty).toBe(true);
		await form.submit([field], false, async (values) => {
			expect(values).toEqual({ title: "Saved" });
			return values;
		});
		expect(form.dirty).toBe(false);
		expect(binding.stale).toBe(true);
		expect(() => binding.set("Late completion")).toThrow("stale");
		expect(form.get("title")).toBe("Saved");
	});

	it("does not expose writable schema metadata through a retained binding", () => {
		const { binding, field } = fixture();
		field.text = { maxLength: 20 };
		field.admin.readOnly = true;
		const retained = binding.schema;
		retained.admin.readOnly = false;
		expect(() => binding.set("Bypassed metadata")).toThrow("read-only");
		binding.destroy();
		retained.text!.maxLength = 0;
		expect(field.text.maxLength).toBe(20);
	});

	it("never exposes writable document references through advanced reads", () => {
		const { form, binding } = fixture();
		const rows = binding.form.get("rows") as Record<string, unknown>[];
		const resource = binding.form.resource!;
		const snapshot = binding.form.snapshot!();
		form.issues = [{ code: "invalid", path: "rows.0.title", message: "Original issue" }];
		const issues = binding.form.issuesFor("rows.0.title");
		binding.destroy();
		rows[1]!.title = "Changed through stale read";
		resource.id = "different";
		(snapshot.rows as Record<string, unknown>[])[0]!.title = "Changed through stale snapshot";
		issues[0]!.message = "Changed through stale issue";
		expect(form.get("rows.1.title")).toBe("Second");
		expect(form.get("rows.0.title")).toBe("First");
		expect(form.resource?.id).toBe("one");
		expect(form.issues[0]!.message).toBe("Original issue");
		expect(() => binding.form.contentLocale).toThrow("stale");
		expect(() => binding.form.resource).toThrow("stale");
		expect(() => binding.form.snapshot!()).toThrow("stale");
		expect(() => binding.form.issuesFor("rows.0.title")).toThrow("stale");
	});

	it("keeps authoring metadata live and forwards stable functions to the current host", async () => {
		const { binding } = fixture();
		const browserProps: FieldReferenceBrowserProps = {
			field: schema(),
			collection: {
				id: "posts",
				slug: "posts",
				labels: { singular: "Post", plural: "Posts" },
				admin: {},
				fields: [schema()],
				capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
			},
			hasMany: false,
			selectedIDs: [],
			onCommit: () => {},
			onClose: () => {},
		};
		const internals = {} as Parameters<FieldAuthoringHost["referenceBrowser"]>[0];
		const rendered = {};
		const signal = new AbortController().signal;
		let host: FieldAuthoringHost = {
			collections: [],
			documentRevision: 0,
			referenceBrowser(anchor, props) {
				expect(anchor).toBe(internals);
				expect(props).toBe(browserProps);
				return rendered;
			},
			async findDocument(collection, id, receivedSignal) {
				expect(this).toBe(host);
				expect(collection).toBe("posts");
				expect(receivedSignal).toBe(signal);
				return { id };
			},
		};
		const authoring = guardEditorAuthoring(() => host, binding.assertActive);
		const browser = authoring.referenceBrowser;
		const find = authoring.findDocument;
		expect(authoring.locale).toBeUndefined();
		expect(authoring.documentRevision).toBe(0);
		host = { ...host, collections: [browserProps.collection], documentRevision: 1, locale: "fr" };
		expect(authoring.collections).toEqual(host.collections);
		const retainedCollections = authoring.collections;
		retainedCollections[0]!.fields[0]!.text!.maxLength = 5;
		expect(host.collections[0]!.fields[0]!.text!.maxLength).toBeUndefined();
		expect(authoring.documentRevision).toBe(1);
		expect(authoring.locale).toBe("fr");
		expect(authoring.referenceBrowser).toBe(browser);
		expect(authoring.findDocument).toBe(find);
		expect(browser(internals, browserProps)).toBe(rendered);
		await expect(find("posts", "one", signal)).resolves.toEqual({ id: "one" });
		binding.destroy();
		expect(() => browser(internals, browserProps)).toThrow("stale");
		expect(() => authoring.collections).toThrow("stale");
		expect(() => authoring.documentRevision).toThrow("stale");
		expect(() => authoring.locale).toThrow("stale");
		expect(() => authoring.referenceBrowser).toThrow("stale");
		expect(() => authoring.findDocument).toThrow("stale");
	});

	it("guards retained authoring calls and discards stale asynchronous completions", async () => {
		const { binding } = fixture();
		let resolve!: (value: FieldDocument) => void;
		let requests = 0;
		const host: FieldAuthoringHost = {
			collections: [],
			documentRevision: 0,
			referenceBrowser: () => ({}),
			findDocument: () => {
				requests += 1;
				return new Promise((complete) => {
					resolve = complete;
				});
			},
		};
		const authoring = guardEditorAuthoring(() => host, binding.assertActive);
		const find = authoring.findDocument;
		expect(authoring.referenceBrowser).toBe(authoring.referenceBrowser);
		expect(find).toBe(authoring.findDocument);
		const pending = find("posts", "one");
		binding.destroy();
		resolve({ id: "one" });
		await expect(pending).rejects.toThrow("stale");
		expect(() => find("posts", "two")).toThrow("stale");
		expect(requests).toBe(1);
	});

	it("revokes before the view updates when a new manifest arrives", () => {
		const form = new FormController({ title: "Existing" });
		form.reset(form.snapshot(), [schema("title")]);
		let revision = 1;
		const binding = new FieldEditorBinding(
			form,
			() => schema("title"),
			"text",
			() => revision
		);
		revision = 2;
		expect(() => binding.set("Obsolete schema")).toThrow("stale");
		expect(form.get("title")).toBe("Existing");
	});

	it("rejects missing row identities instead of trusting reused indexes", () => {
		const form = new FormController({ rows: [{ title: "Unidentified" }] });
		form.reset(form.snapshot(), [rowsSchema()]);
		expect(() => new FieldEditorBinding(form, () => schema(), "text")).toThrow("stable _key");
		expect(form.isRegistered("rows.0.title")).toBe(false);
	});
});
