import type { SchemaBlockType, SchemaField } from "@riducms/protocol";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { evaluateFieldCondition } from "@admin/core/forms/field-condition";
import { fieldAccessPath, scopeRepeatedRowField } from "@admin/fields/nested/scoped-field";

/** Header summaries observe the same child visibility policy as the content form. */
export function visibleBlockChild(form: FormController, field: SchemaField) {
	return (
		field.admin.hidden !== true &&
		form.canRead(field.path, fieldAccessPath(field)) &&
		(field.admin.condition === undefined ||
			evaluateFieldCondition(field.admin.condition, field.path, (path) => form.get(path)))
	);
}

export function blockHeaderValue(
	form: FormController,
	block: SchemaBlockType,
	path: string,
	name: string | undefined
) {
	const child = block.fields.find((field) => field.name === name);
	if (child === undefined) return "";
	const scoped = scopeRepeatedRowField(child, path, "header-summary");
	if (!visibleBlockChild(form, scoped)) return "";
	const value = form.get(scoped.path);
	return value === undefined || value === null || String(value).trim() === "" ? "" : String(value);
}
