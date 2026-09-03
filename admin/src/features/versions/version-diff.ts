import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import type { AdminI18n } from "@riducms/translations";

import type { AdminDocument, AdminVersion } from "@admin/core/api/admin-client";

export interface VersionDiffRow {
	path: string;
	label: string;
	before: unknown;
	after: unknown;
	changed: boolean;
}

export function versionDiffRows(
	collection: SchemaCollection,
	current: AdminVersion,
	comparison: AdminVersion | undefined,
	i18n: AdminI18n
): VersionDiffRow[] {
	const fields = comparisonFields(collection.fields);
	return [
		{
			path: "_status",
			label: i18n.t("versions:status"),
			before: comparison?.Status,
			after: current.Status,
			changed: !sameValue(comparison?.Status, current.Status),
		},
		...fields.map(({ field, path }) => {
			const before = valueAtPath(comparison?.Snapshot, path);
			const after = valueAtPath(current.Snapshot, path);
			return {
				path,
				label: i18n.text(field.admin.label || field.name, field.admin.labelTranslations),
				before,
				after,
				changed: !sameValue(before, after),
			};
		}),
	];
}

export function formatVersionValue(value: unknown, i18n: AdminI18n): string {
	if (value === undefined || value === null || value === "") return "—";
	if (typeof value === "string") return value;
	if (typeof value === "number") return i18n.formatNumber(value);
	if (typeof value === "boolean") return i18n.t(value ? "versions:true" : "versions:false");
	return JSON.stringify(value, null, 2);
}

function comparisonFields(fields: readonly SchemaField[]) {
	return fields.flatMap((field): Array<{ field: SchemaField; path: string }> => {
		if (field.type === "ui" || field.virtual !== undefined || field.join !== undefined) return [];
		if (field.type === "group" && field.nested !== undefined) {
			return comparisonFields(field.nested.fields);
		}
		return [{ field, path: field.path || field.name }];
	});
}

function valueAtPath(document: AdminDocument | undefined, path: string): unknown {
	let value: unknown = document;
	for (const segment of path.split(".")) {
		if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
		value = Reflect.get(value, segment);
	}
	return value;
}

function sameValue(left: unknown, right: unknown) {
	return JSON.stringify(left) === JSON.stringify(right);
}
