import { cloneSchemaField, type SchemaCollection } from "@riducms/protocol";
import { cloneFormValue } from "@admin/core/forms/form-schema";

/**
 * Detached collection schemas for extension boundaries. Fields are cloned with
 * `cloneSchemaField`, which keeps each block container's lazy selection of the
 * manifest's definitions; a plain value clone would drop those bindings.
 */
export function cloneSchemaCollections(
	collections: readonly SchemaCollection[]
): SchemaCollection[] {
	return collections.map(({ fields, ...metadata }) => ({
		...(cloneFormValue(metadata) as Omit<SchemaCollection, "fields">),
		fields: fields.map(cloneSchemaField),
	}));
}
