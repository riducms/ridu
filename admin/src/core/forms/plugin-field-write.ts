import type { SchemaField } from "@riducms/protocol";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { indexFieldValues } from "@admin/core/forms/form-issue-correlation";
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
		const previous = new Map(before.map((location) => [location.path, location]));
		const fields: typeof form.access.fields = {};
		for (const [path, access] of Object.entries(form.access.fields)) {
			if (path !== field.path && !path.startsWith(`${field.path}.`)) {
				fields[path] = access;
				continue;
			}
			let prefix = path;
			while (!previous.has(prefix) && prefix.includes("."))
				prefix = prefix.slice(0, prefix.lastIndexOf("."));
			const source = previous.get(prefix);
			if (!source) {
				fields[path] = access;
				continue;
			}
			const target = current.get(source.token);
			if (!target) continue;
			const suffix = path.slice(source.path.length);
			if (
				/\.\d+(?:\.|$)/.test(suffix) &&
				JSON.stringify(source.value) !== JSON.stringify(target.value)
			)
				continue;
			fields[target.path + suffix] = access;
		}
		form.access = { ...form.access, fields };
	}
	if (field.plugin?.embeddedTrees !== undefined) form.setEmbedded(field, value, false);
	else form.set(field.path, value);
}
