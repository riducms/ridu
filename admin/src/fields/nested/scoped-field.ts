import type { SchemaField } from "@riducms/protocol";

const canonicalAccessPath = Symbol.for("@riducms/admin/canonicalAccessPath");
type AccessScopedField = SchemaField & { [canonicalAccessPath]?: string };

export function fieldAccessPath(field: SchemaField) {
	return (field as AccessScopedField)[canonicalAccessPath] ?? field.path;
}

/**
 * Rebinds a manifest field tree to one concrete repeating-row path.
 *
 * Manifest paths deliberately omit runtime array indexes and block variants.
 * The form controller, however, addresses the concrete row. Rebinding the
 * complete subtree keeps nested groups/arrays/blocks on that same row while
 * preserving every other piece of manifest metadata.
 */
export function scopeRepeatedRowField(
	field: SchemaField,
	rowPath: string,
	instance: string,
	readOnly = false
): SchemaField {
	return scopeField(field, `${rowPath}.${field.name}`, instance, readOnly);
}

function scopeField(
	field: SchemaField,
	path: string,
	instance: string,
	readOnly: boolean
): SchemaField {
	const scoped: AccessScopedField = {
		...field,
		[canonicalAccessPath]: fieldAccessPath(field),
		id: `${field.id}-${instance}`,
		path,
		admin: scopeAdmin(field.admin, instance, readOnly),
		...(field.nested === undefined
			? {}
			: {
					nested: {
						...field.nested,
						fields: field.nested.fields.map((child) =>
							scopeField(child, `${path}.${child.name}`, instance, readOnly)
						),
					},
				}),
		...(field.blocks === undefined
			? {}
			: {
					blocks: {
						...field.blocks,
						types: field.blocks.types.map((block) => ({
							...block,
							fields: block.fields.map((child) =>
								scopeField(child, `${path}.${child.name}`, instance, readOnly)
							),
						})),
					},
				}),
	};
	return scoped;
}

function scopeAdmin(field: SchemaField["admin"], instance: string, readOnly: boolean) {
	return {
		...field,
		...(readOnly ? { readOnly: true } : {}),
		...(field.row === undefined ? {} : { row: { id: `${field.row.id}-${instance}` } }),
		...(field.collapsible === undefined
			? {}
			: { collapsible: { ...field.collapsible, id: `${field.collapsible.id}-${instance}` } }),
		...(field.tabGroup === undefined
			? {}
			: { tabGroup: { id: `${field.tabGroup.id}-${instance}` } }),
	};
}
