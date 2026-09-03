import { describe, expect, it } from "bun:test";
import type { SchemaField } from "@riducms/protocol";

import {
	documentFormValues,
	initialFormValues,
	localizationSource,
	reconcileFormSchema,
	recoverFormDraft,
	shouldSubmitLocalizedPath,
	submissionFormValues,
} from "../src/core/forms/form-schema";

describe("form schema reconciliation", () => {
	it("omits UI, inverse join, and virtual values from submissions", () => {
		const fields = [
			textField("title", "title", "Title"),
			presentationField("notice", "ui"),
			presentationField("related", "join"),
			presentationField("label", "virtual"),
		];
		expect(
			submissionFormValues(fields, {
				title: "Hello",
				notice: "ignore",
				related: [{ id: "posts_1" }],
				label: "computed",
			})
		).toEqual({ title: "Hello" });
	});

	it("omits fields that access capabilities make read-only", () => {
		const fields = [textField("title", "title", "Title"), textField("secret", "secret", "Secret")];
		expect(
			submissionFormValues(
				fields,
				{ title: "Hello", secret: "redacted" },
				(path) => path !== "secret"
			)
		).toEqual({ title: "Hello" });
	});

	it("keeps hidden field defaults and current values in submissions", () => {
		const hidden = textField("posts-source", "source", "Source");
		hidden.default = "system";
		hidden.admin.hidden = true;

		expect(initialFormValues([hidden])).toEqual({ source: "system" });
		expect(submissionFormValues([hidden], { source: "existing" })).toEqual({
			source: "existing",
		});
	});
	it("preserves dirty values when presentation metadata changes", () => {
		const previous = [textField("posts-title", "title", "Title")];
		const next = [textField("posts-title", "title", "Headline")];

		const result = reconcileFormSchema(
			{ values: { title: "Draft title" }, original: { title: "Published title" } },
			previous,
			next
		);

		expect(result.values).toEqual({ title: "Draft title" });
		expect(result.original).toEqual({ title: "Published title" });
		expect(result.detached).toEqual([]);
	});

	it("moves values across a stable-ID rename and initializes new defaults for creates", () => {
		const previous = [textField("posts-title", "title", "Title")];
		const next = [
			textField("posts-title", "headline", "Headline"),
			selectField("posts-status", "status", "draft"),
		];

		const result = reconcileFormSchema(
			{ values: { title: "Draft title" }, original: { title: "Published title" } },
			previous,
			next,
			{ initializeDefaults: true }
		);

		expect(result.values).toEqual({ headline: "Draft title", status: "draft" });
		expect(result.original).toEqual({ headline: "Published title", status: "draft" });
	});

	it("keeps newly added defaulted fields absent while reconciling an existing document", () => {
		const previous = [textField("posts-title", "title", "Title")];
		const next = [
			...previous,
			groupField("settings", [textField("posts-settings-theme", "theme", "Theme")]),
		];
		next[1]!.nested!.fields[0]!.default = "dark";

		const result = reconcileFormSchema(
			{ values: { title: "Draft title" }, original: { title: "Published title" } },
			previous,
			next
		);

		expect(result.values).toEqual({ title: "Draft title" });
		expect(result.original).toEqual({ title: "Published title" });
	});

	it("initializes ordered multi-select defaults without sharing the manifest array", () => {
		const roles = selectField("users-roles", "roles", "admin");
		roles.select = {
			hasMany: true,
			defaultValues: ["admin", "editor"],
			choices: [
				{ value: "admin", label: "Admin" },
				{ value: "editor", label: "Editor" },
			],
		};
		roles.default = undefined;

		const values = initialFormValues([roles]);
		expect(values).toEqual({ roles: ["admin", "editor"] });
		expect(submissionFormValues([roles], { roles: ["editor", "admin"] })).toEqual({
			roles: ["editor", "admin"],
		});
		(values.roles as string[]).push("reviewer");
		expect(roles.select.defaultValues).toEqual(["admin", "editor"]);
	});

	it("does not materialize an empty ordered multi-select default", () => {
		const roles = selectField("users-roles", "roles", "admin");
		roles.select = {
			hasMany: true,
			defaultValues: [],
			choices: [{ value: "admin", label: "Admin" }],
		};
		roles.default = undefined;

		expect(initialFormValues([roles])).toEqual({});
	});

	it("materializes recursive defaults for groups and repeating-row schemas", () => {
		const settings = groupField("settings", [
			textField("settings-theme", "theme", "Theme"),
			groupField("seo", [textField("settings-seo-title", "title", "SEO title")]),
		]);
		settings.nested!.fields[0]!.default = "dark";
		settings.nested!.fields[1]!.nested!.fields[0]!.default = "Untitled";

		expect(initialFormValues([settings])).toEqual({
			settings: { theme: "dark", seo: { title: "Untitled" } },
		});
		expect(initialFormValues(settings.nested!.fields)).toEqual({
			theme: "dark",
			seo: { title: "Untitled" },
		});

		settings.admin.condition = {
			kind: "predicate",
			predicate: {
				scope: "sibling",
				path: "mode",
				operator: "equals",
				values: [{ type: "string", value: "advanced" }],
			},
		};
		expect(initialFormValues([settings])).toEqual({
			settings: { theme: "dark", seo: { title: "Untitled" } },
		});
	});

	it("initializes minimum array rows only for create forms", () => {
		const items: SchemaField = {
			id: "posts-items",
			name: "items",
			path: "items",
			type: "array",
			category: "nested",
			required: false,
			unique: false,
			admin: { label: "Items" },
			nested: {
				minRows: 2,
				fields: [textField("posts-items-label", "label", "Label")],
			},
		};
		items.nested!.fields[0]!.default = "New item";

		const created = initialFormValues([items]);
		expect(created.items).toEqual([
			{ label: "New item", _key: expect.any(String) },
			{ label: "New item", _key: expect.any(String) },
		]);
		expect((created.items as Array<{ _key: string }>)[0]!._key).not.toBe(
			(created.items as Array<{ _key: string }>)[1]!._key
		);
		expect(documentFormValues([items], {})).toEqual({});
	});

	it("keeps absent defaulted groups absent while editing and during unrelated submissions", () => {
		const title = textField("posts-title", "title", "Title");
		const settings = groupField("settings", [textField("settings-theme", "theme", "Theme")]);
		settings.nested!.fields[0]!.default = "dark";
		const fields = [title, settings];

		const values = documentFormValues(fields, { title: "Stored title" });
		expect(values).toEqual({ title: "Stored title" });

		values.title = "Unrelated title edit";
		expect(submissionFormValues(fields, values)).toEqual({
			title: "Unrelated title edit",
		});
	});

	it("initializes a compatible absent group when a child gains a default during create", () => {
		const previous = groupField("settings", [textField("settings-theme", "theme", "Theme")]);
		const next = groupField("settings", [textField("settings-theme", "theme", "Theme")]);
		next.nested!.fields[0]!.default = "dark";

		const created = reconcileFormSchema({ values: {}, original: {} }, [previous], [next], {
			initializeDefaults: true,
		});
		expect(created.values).toEqual({ settings: { theme: "dark" } });
		expect(created.original).toEqual({ settings: { theme: "dark" } });

		const existing = reconcileFormSchema({ values: {}, original: {} }, [previous], [next]);
		expect(existing.values).toEqual({});
		expect(existing.original).toEqual({});
	});

	it("initializes a compatible absent array when create-mode minimum rows increase", () => {
		const previous = arrayField("items", 0, [textField("posts-items-label", "label", "Label")]);
		const next = arrayField("items", 2, [textField("posts-items-label", "label", "Label")]);
		next.nested!.fields[0]!.default = "New item";

		const result = reconcileFormSchema({ values: {}, original: {} }, [previous], [next], {
			initializeDefaults: true,
		});
		const currentRows = result.values.items as Array<{ _key: string; label: string }>;
		const originalRows = result.original.items as Array<{ _key: string; label: string }>;

		expect(currentRows).toEqual([
			{ label: "New item", _key: expect.any(String) },
			{ label: "New item", _key: expect.any(String) },
		]);
		expect(originalRows).toEqual(currentRows);
		expect(originalRows).not.toBe(currentRows);
		expect(originalRows[0]).not.toBe(currentRows[0]);
		expect(currentRows[0]!._key).not.toBe(currentRows[1]!._key);
	});

	it("detaches a dirty select value when its cardinality changes", () => {
		const previous = [selectField("posts-status", "status", "draft")];
		const nextStatus = selectField("posts-status", "status", "draft");
		nextStatus.default = undefined;
		nextStatus.select = {
			hasMany: true,
			defaultValues: ["draft"],
			choices: [{ value: "draft", label: "Draft" }],
		};

		const result = reconcileFormSchema(
			{ values: { status: "published" }, original: { status: "draft" } },
			previous,
			[nextStatus],
			{ initializeDefaults: true }
		);

		expect(result.values).toEqual({ status: ["draft"] });
		expect(result.original).toEqual({ status: ["draft"] });
		expect(result.detached).toEqual([
			{
				fieldId: "posts-status",
				label: "status",
				path: "status",
				reason: "incompatible",
				value: "published",
			},
		]);
	});

	it("detaches dirty values that no longer have a compatible field", () => {
		const previous = [textField("posts-title", "title", "Title")];

		const result = reconcileFormSchema(
			{ values: { title: "Unsaved" }, original: { title: "Saved" } },
			previous,
			[]
		);

		expect(result.values).toEqual({});
		expect(result.detached).toEqual([
			{
				fieldId: "posts-title",
				label: "Title",
				path: "title",
				reason: "removed",
				value: "Unsaved",
			},
		]);
	});

	it("restores only dirty draft fields over freshly loaded server values", () => {
		const fields = [
			textField("posts-title", "title", "Title"),
			selectField("posts-status", "status", "draft"),
		];

		const result = recoverFormDraft(
			{
				values: { title: "Server title", status: "published" },
				original: { title: "Server title", status: "published" },
			},
			{
				values: { title: "Unsaved title", status: "draft" },
				original: { title: "Old server title", status: "draft" },
			},
			fields,
			fields
		);

		expect(result.values).toEqual({ title: "Unsaved title", status: "published" });
		expect(result.original).toEqual({ title: "Server title", status: "published" });
		expect(result.restoredFields).toBe(1);
	});

	it("does not persist untouched fallback values into the selected locale", () => {
		const fields = [
			textField("posts-title", "title", "Title"),
			textField("posts-slug", "slug", "Slug"),
		];
		const original = { title: "Hello", slug: "hello" };
		const values = { title: "Hello", slug: "bonjour" };
		const sources = { title: "en" };
		const include = (path: string) =>
			shouldSubmitLocalizedPath(path, values, original, "fr", sources);

		expect(submissionFormValues(fields, values, include)).toEqual({ slug: "bonjour" });

		values.title = "Bonjour";
		expect(submissionFormValues(fields, values, include)).toEqual({
			title: "Bonjour",
			slug: "bonjour",
		});
	});

	it("keeps fallback provenance attached to keyed rows after a reorder", () => {
		const original = {
			links: [
				{ _key: "first", label: "First" },
				{ _key: "second", label: "Second" },
			],
		};
		const values = { links: [original.links[1], original.links[0]] };
		const sources = { "links.0.label": "en", "links.1.label": "en" };

		expect(localizationSource("links.0.label", values, original, sources)).toBe("en");
		expect(shouldSubmitLocalizedPath("links.0.label", values, original, "fr", sources)).toBe(false);
		values.links[0] = { ...values.links[0], label: "Deuxième" };
		expect(shouldSubmitLocalizedPath("links.0.label", values, original, "fr", sources)).toBe(true);
	});
});

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

function presentationField(name: string, type: "ui" | "join" | "virtual"): SchemaField {
	return {
		id: name,
		name,
		path: name,
		type,
		category: "presentation",
		required: false,
		unique: false,
		admin: { label: name },
	};
}

function groupField(name: string, fields: SchemaField[]): SchemaField {
	return {
		id: name,
		name,
		path: name,
		type: "group",
		category: "nested",
		required: false,
		unique: false,
		admin: { label: name },
		nested: { fields },
	};
}

function arrayField(name: string, minRows: number, fields: SchemaField[]): SchemaField {
	return {
		id: name,
		name,
		path: name,
		type: "array",
		category: "nested",
		required: false,
		unique: false,
		admin: { label: name },
		nested: { minRows, fields },
	};
}

type SelectFieldFixture = SchemaField & { select: NonNullable<SchemaField["select"]> };

function selectField(id: string, name: string, defaultValue: string): SelectFieldFixture {
	return {
		id,
		name,
		path: name,
		type: "select",
		category: "scalar",
		required: false,
		unique: false,
		default: defaultValue,
		admin: { label: name },
		select: { choices: [{ value: defaultValue, label: defaultValue }] },
	};
}
