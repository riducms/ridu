import type { SchemaField } from "@riducms/protocol";

import { initialFormValues, type FormValues } from "@admin/core/forms/form-schema";

/** Initialize each selected scalar so clearing it remains an explicit bulk patch. */
export function initialBulkEditValues(
	fields: readonly SchemaField[],
	current: Readonly<FormValues> = {}
): FormValues {
	const values = initialFormValues(fields, current as FormValues);
	for (const field of fields) {
		if (field.category !== "scalar" || Object.hasOwn(values, field.name)) continue;
		values[field.name] = emptyScalarValue(field);
	}
	return values;
}

function emptyScalarValue(field: SchemaField): unknown {
	if (field.type === "checkbox") return false;
	if (field.type === "number" || field.type === "point") return null;
	if (field.type === "select" && field.select?.hasMany) return [];
	return "";
}
