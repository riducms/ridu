import { describe, expect, it } from "bun:test";
import type { SchemaField } from "@riducms/protocol";
import { createAdminI18n, en, fr } from "@riducms/translations";

import { invalidFieldLabels, validateFormValues } from "../src/core/forms/form-validation";

describe("manifest-derived form validation", () => {
	it("rejects empty required fields and accepts empty optional controls", () => {
		const fields = [
			field("title", "text", true),
			field("summary", "textarea", true),
			selectField("status", true),
			selectField("category", false),
			relationshipField("author", true),
			relationshipField("related", false),
		];

		const issues = validateFormValues(
			fields,
			{ title: "", summary: "", status: "", category: "", author: "", related: "" },
			{ requireMissing: true }
		);

		expect(issues.map(({ code, path }) => ({ code, path }))).toEqual([
			{ code: "required", path: "title" },
			{ code: "required", path: "summary" },
			{ code: "required", path: "status" },
			{ code: "required", path: "author" },
		]);
	});

	it("does not validate fields omitted by access capabilities", () => {
		expect(
			validateFormValues(
				[field("title", "text", true), field("secret", "text", true)],
				{ title: "Visible", secret: "" },
				{ requireMissing: true, include: (path) => path !== "secret" }
			)
		).toEqual([]);
	});

	it("requires nested values whenever a row is supplied during an update", () => {
		const answers = field("answers", "array", false);
		answers.nested = { fields: [field("copy", "text", true, "answers.copy")] };

		expect(
			validateFormValues(
				[answers],
				{ answers: [{ _key: "row-1", copy: "" }] },
				{ requireMissing: false }
			)
		).toEqual([{ code: "required", path: "answers.0.copy", message: "copy is required" }]);
		expect(validateFormValues([answers], {}, { requireMissing: false })).toEqual([]);
	});

	it("reports collection and block errors at concrete runtime paths", () => {
		const requiredRows = field("items", "array", true);
		requiredRows.nested = { fields: [field("title", "text", true, "items.title")] };
		const content = field("content", "blocks", false);
		content.blocks = {
			types: [
				{
					key: "heading",
					label: "Heading",
					fields: [field("text", "text", true, "content.heading.text")],
				},
			],
		};

		const issues = validateFormValues(
			[requiredRows, content],
			{ items: [], content: [{ _key: "block-1", blockType: "heading", text: "" }] },
			{ requireMissing: true }
		);

		expect(issues.map(({ code, path }) => ({ code, path }))).toEqual([
			{ code: "required", path: "items" },
			{ code: "required", path: "content.0.text" },
		]);
	});

	it("allows false and zero for required boolean and number fields", () => {
		expect(
			validateFormValues(
				[field("enabled", "checkbox", true), field("priority", "number", true)],
				{ enabled: false, priority: 0 },
				{ requireMissing: true }
			)
		).toEqual([]);
	});

	it("validates bounded arrays, radio choices, and geographic points", () => {
		const team = field("team", "array", false);
		team.nested = { fields: [field("name", "text", true, "team.name")], minRows: 1, maxRows: 2 };
		const priority = { ...selectField("priority", true), type: "radio" as const };
		const location = field("location", "point", false);

		expect(
			validateFormValues(
				[team, priority, location],
				{ team: [], priority: "unknown", location: [181, 45] },
				{ requireMissing: true }
			).map(({ code, path }) => ({ code, path }))
		).toEqual([
			{ code: "min_rows", path: "team" },
			{ code: "invalid_choice", path: "priority" },
			{ code: "invalid_point", path: "location" },
		]);
	});

	it("validates required ordered multi-select values and reports member paths", () => {
		const roles = selectField("roles", true);
		roles.select = {
			hasMany: true,
			choices: [
				{ value: "admin", label: "Admin" },
				{ value: "editor", label: "Editor" },
			],
		};

		expect(
			validateFormValues([roles], { roles: [] }, { requireMissing: true }).map(
				({ code, path }) => ({
					code,
					path,
				})
			)
		).toEqual([{ code: "required", path: "roles" }]);
		expect(
			validateFormValues([roles], { roles: ["editor", "admin"] }, { requireMissing: true })
		).toEqual([]);
		expect(
			validateFormValues(
				[roles],
				{ roles: ["admin", "unknown", "admin", 42] },
				{ requireMissing: true }
			).map(({ code, path }) => ({ code, path }))
		).toEqual([
			{ code: "invalid_choice", path: "roles.1" },
			{ code: "duplicate_choice", path: "roles.2" },
			{ code: "invalid_type", path: "roles.3" },
		]);
		expect(
			validateFormValues([roles], { roles: "admin" }, { requireMissing: true }).map(
				({ code, path }) => ({ code, path })
			)
		).toEqual([{ code: "invalid_type", path: "roles" }]);

		roles.select.defaultValues = ["admin"];
		expect(validateFormValues([roles], {}, { requireMissing: true })).toEqual([]);

		roles.select.defaultValues = [];
		expect(validateFormValues([roles], {}, { requireMissing: true })).toEqual([
			{ code: "required", path: "roles", message: "roles is required" },
		]);
	});

	it("keeps nested required validation unconditional for default-only groups", () => {
		const settings = field("settings", "group", false);
		settings.category = "nested";
		const zeta = field("zeta", "text", false, "settings.zeta");
		zeta.default = "z";
		const alpha = field("alpha", "text", false, "settings.alpha");
		alpha.default = "a";
		settings.nested = {
			fields: [zeta, alpha, field("requiredSibling", "text", true, "settings.requiredSibling")],
		};

		expect(
			validateFormValues([settings], {}, { requireMissing: true }).map(({ code, path }) => ({
				code,
				path,
			}))
		).toEqual([{ code: "required", path: "settings.requiredSibling" }]);
		expect(
			validateFormValues(
				[settings],
				{ settings: { alpha: "a", zeta: "z" } },
				{ requireMissing: true }
			).map(({ code, path }) => ({ code, path }))
		).toEqual([{ code: "required", path: "settings.requiredSibling" }]);
	});

	it("turns concrete issue paths into distinct actionable field labels", () => {
		const title = field("title", "text", true);
		title.admin.label = "Title";
		const answers = field("answers", "array", false);
		answers.admin.label = "Answers";
		const copy = field("copy", "text", true, "answers.copy");
		copy.admin.label = "Copy";
		answers.nested = { fields: [copy] };

		expect(
			invalidFieldLabels(
				[title, answers],
				[
					{ code: "required", path: "title", message: "Title is required" },
					{ code: "required", path: "answers.0.copy", message: "Copy is required" },
					{ code: "required", path: "answers.0.copy", message: "Copy is required" },
				]
			)
		).toEqual(["Title", "Answers → Row 1 → Copy"]);
	});

	it("uses the interface language for authored labels and validation messages", () => {
		const i18n = createAdminI18n({ languages: [en, fr], language: "fr" });
		const title = field("title", "text", true);
		title.admin = {
			label: "Title",
			labelTranslations: { fr: "Titre" },
		};
		const answers = field("answers", "array", false);
		answers.admin = {
			label: "Answers",
			labelTranslations: { fr: "Réponses" },
		};
		const copy = field("copy", "text", true, "answers.copy");
		copy.admin = {
			label: "Copy",
			labelTranslations: { fr: "Copie" },
		};
		answers.nested = { fields: [copy] };

		const issues = validateFormValues(
			[title, answers],
			{ title: "", answers: [{ _key: "row-1", copy: "" }] },
			{ requireMissing: true, i18n }
		);

		expect(issues.map((issue) => issue.message)).toEqual([
			"Titre est obligatoire",
			"Copie est obligatoire",
		]);
		expect(invalidFieldLabels([title, answers], issues, i18n)).toEqual([
			"Titre",
			"Réponses → Ligne 1 → Copie",
		]);
	});
});

function field(
	name: string,
	type: SchemaField["type"],
	required: boolean,
	path = name
): SchemaField {
	return {
		id: `posts-${name}`,
		name,
		path,
		type,
		category: type === "array" || type === "blocks" ? "nested" : "scalar",
		required,
		unique: false,
		admin: { label: name },
	};
}

type SelectFieldFixture = SchemaField & { select: NonNullable<SchemaField["select"]> };

function selectField(name: string, required: boolean): SelectFieldFixture {
	return {
		...field(name, "select", required),
		select: { choices: [{ value: "draft", label: "Draft" }] },
	};
}

function relationshipField(name: string, required: boolean): SchemaField {
	return {
		...field(name, "relationship", required),
		category: "relationship" as const,
		relationship: { collectionId: "users", collectionSlug: "users", onDelete: "nullify" },
	};
}
