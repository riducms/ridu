import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import type { AdminDocument } from "@admin/core/api/admin-client";

/** Display/search identity shared by lists, pickers and editors. */
export function documentTitleField(
	collection: SchemaCollection | undefined,
	canRead: (path: string) => boolean = () => true
): SchemaField | undefined {
	const fields = collection?.fields.filter((field) => canRead(field.path)) ?? [];
	return (
		fields.find((field) => field.name === collection?.admin.useAsTitle) ??
		fields.find((field) => field.name === "title" || field.name === "name") ??
		fields.find((field) => field.type === "text") ??
		fields.find((field) => ["email", "select", "radio"].includes(field.type))
	);
}

export function documentLabel(
	collection: SchemaCollection | undefined,
	document: AdminDocument
): string {
	const field = documentTitleField(collection);
	const value = field === undefined ? undefined : document[field.name];
	return value === undefined || value === null || value === "" ? document.id : String(value);
}
