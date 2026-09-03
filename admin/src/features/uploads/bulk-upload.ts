import type { SchemaField } from "@riducms/protocol";

import { initialFormValues } from "@admin/core/forms/form-schema";

export function bulkUploadMetadataFields(fields: readonly SchemaField[]) {
	return fields.filter(
		(field) =>
			field.category === "scalar" &&
			field.admin.readOnly !== true &&
			(field.type === "text" ||
				field.type === "textarea" ||
				(field.type === "select" && field.select?.hasMany !== true))
	);
}

export function initialBulkUploadValues(
	filename: string,
	fields: readonly SchemaField[],
	useAsTitle: string | undefined
) {
	const stem = filename
		.replace(/\.[^.]+$/, "")
		.replaceAll(/[-_]+/g, " ")
		.trim();
	const defaults = initialFormValues(fields);
	const values: Record<string, string> = {};
	for (const field of fields) {
		const defaultValue = defaults[field.name];
		values[field.name] = typeof defaultValue === "string" ? defaultValue : "";
		if (field.name === "alt" || (field.required && field.name === useAsTitle)) {
			values[field.name] = stem;
		}
	}
	return values;
}

export function uploadData(values: Readonly<Record<string, string>>) {
	return Object.fromEntries(Object.entries(values).filter(([, value]) => value.trim().length > 0));
}
