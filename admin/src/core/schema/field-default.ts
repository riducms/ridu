import type { SchemaField } from "@riducms/protocol";

export type SchemaFieldDefault = { present: false } | { present: true; value: unknown };

/** Decode the literal-default representation used by the schema manifest. */
export function schemaFieldDefault(field: SchemaField): SchemaFieldDefault {
	if (field.type === "select" && field.select?.hasMany === true) {
		if ((field.select.defaultValues?.length ?? 0) === 0) return { present: false };
		return { present: true, value: field.select.defaultValues };
	}
	if (field.default === undefined) return { present: false };
	if (field.type === "text-list" || field.type === "number-list")
		return { present: true, value: JSON.parse(field.default) };
	if (field.type === "number") return { present: true, value: Number(field.default) };
	if (field.type === "checkbox") return { present: true, value: field.default === "true" };
	return { present: true, value: field.default };
}

/** Whether a generated create object requires this field when that object is supplied. */
export function schemaFieldRequiredOnCreate(field: SchemaField): boolean {
	const inputRequired =
		field.required ||
		((field.type === "text-list" || field.type === "number-list") &&
			(field.list?.minRows ?? 0) > 0);
	if (!inputRequired) return false;

	return !(
		field.default !== undefined ||
		field.dynamicDefault === true ||
		(field.select?.hasMany === true && (field.select.defaultValues?.length ?? 0) > 0) ||
		field.text?.slug !== undefined
	);
}
