import type { SchemaField } from "@riducms/protocol";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { indexFieldValues, rebaseFieldAccess } from "@admin/core/forms/form-issue-correlation";
import { fieldAccessPath } from "@admin/fields/nested/scoped-field";
import { evaluateFieldCondition } from "@admin/core/forms/field-condition";

/** Structural plugin writes use the same schema identity index as server issue correlation. */
export function writePluginField(form: FormController, field: SchemaField, value: unknown) {
	const before = indexFieldValues([field], { [field.name]: form.get(field.path) });
	const after = indexFieldValues([field], { [field.name]: value });
	const current = new Map(after.map((location) => [location.token, location]));
	for (const source of before) {
		const schema = source.schema,
			target = current.get(source.token);
		if (!schema || !target || JSON.stringify(source.value) === JSON.stringify(target.value))
			continue;
		if (
			schema.admin.readOnly ||
			schema.admin.hidden ||
			!form.canRead(source.path, fieldAccessPath(schema)) ||
			!form.canWrite(source.path, fieldAccessPath(schema)) ||
			(schema.admin.condition !== undefined &&
				!evaluateFieldCondition(schema.admin.condition, source.path, (path) => form.get(path)))
		)
			throw new Error(`Field ${source.path} is read-only.`);
	}
	if (form.access !== undefined) {
		form.access = { ...form.access, fields: rebaseFieldAccess(before, after, form.access.fields) };
	}
	if (field.plugin?.embeddedTrees !== undefined) form.setEmbedded(field, value, false);
	else form.set(field.path, value);
}
