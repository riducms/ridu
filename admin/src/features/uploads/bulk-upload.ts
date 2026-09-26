import { mapBlockTypes, type SchemaField } from "@riducms/protocol";

import { cloneFormValue, initialFormValues, type FormValues } from "@admin/core/forms/form-schema";
import { isUploadMetadataField } from "@admin/features/uploads/upload-document-contracts";

export function bulkUploadDocumentFields(fields: readonly SchemaField[]) {
	return fields.filter((field) => !isUploadMetadataField(field.name));
}

export function bulkUploadBulkEditFields(fields: readonly SchemaField[]) {
	return bulkUploadDocumentFields(fields).filter(
		(field) =>
			field.category === "scalar" &&
			field.admin.readOnly !== true &&
			field.admin.hidden !== true &&
			field.admin.condition === undefined &&
			!field.unique &&
			field.type !== "json" &&
			field.type !== "text-list" &&
			field.type !== "number-list"
	);
}

/** Give concurrent document renderers unique DOM IDs without changing form paths or access paths. */
export function bulkUploadRenderFields(fields: readonly SchemaField[], scope: string) {
	return fields.map((field) => scopeRenderField(field, scope));
}

function scopeRenderField(field: SchemaField, scope: string): SchemaField {
	return {
		...field,
		id: `${field.id}-${scope}`,
		admin: {
			...field.admin,
			...(field.admin.row === undefined
				? {}
				: { row: { ...field.admin.row, id: `${field.admin.row.id}-${scope}` } }),
			...(field.admin.collapsible === undefined
				? {}
				: {
						collapsible: {
							...field.admin.collapsible,
							id: `${field.admin.collapsible.id}-${scope}`,
						},
					}),
			...(field.admin.tabGroup === undefined
				? {}
				: {
						tabGroup: {
							...field.admin.tabGroup,
							id: `${field.admin.tabGroup.id}-${scope}`,
						},
					}),
		},
		...(field.nested === undefined
			? {}
			: {
					nested: {
						...field.nested,
						fields: field.nested.fields.map((child) => scopeRenderField(child, scope)),
					},
				}),
		...(field.blocks === undefined
			? {}
			: {
					blocks: mapBlockTypes(field.blocks, (block) => ({
						...block,
						fields: block.fields.map((child) => scopeRenderField(child, scope)),
					})),
				}),
		...(field.plugin?.embeddedTrees === undefined
			? {}
			: {
					plugin: {
						...field.plugin,
						embeddedTrees: field.plugin.embeddedTrees.map((tree) => ({
							...tree,
							cases: tree.cases.map((branch) =>
								mapBlockTypes(branch, (block) => ({
									...block,
									fields: block.fields.map((child) => scopeRenderField(child, scope)),
								}))
							),
						})),
					},
				}),
	};
}

export function initialBulkUploadValues(
	filename: string,
	fields: readonly SchemaField[],
	useAsTitle: string | undefined
): FormValues {
	const stem = filename
		.replace(/\.[^.]+$/, "")
		.replaceAll(/[-_]+/g, " ")
		.trim();
	const values = initialFormValues(fields);
	for (const field of fields) {
		if (field.name === "alt" || (field.required && field.name === useAsTitle)) {
			values[field.name] = stem;
		}
	}
	return values;
}

export function applyBulkUploadValues(
	target: { set(path: string, value: unknown): void },
	fields: readonly SchemaField[],
	values: Readonly<FormValues>
) {
	for (const field of fields) {
		if (Object.hasOwn(values, field.name))
			target.set(field.path, cloneFormValue(values[field.name]));
	}
}

export function bulkUploadFileIdentity(file: File) {
	return `${file.name}\u0000${file.size}\u0000${file.lastModified}`;
}

export function bulkUploadRemoteFilename(value: string) {
	try {
		const filename = new URL(value).pathname.split("/").filter(Boolean).at(-1);
		return filename === undefined ? "remote-upload" : decodeURIComponent(filename);
	} catch {
		return "remote-upload";
	}
}
