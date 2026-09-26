import { resolveBlockTypes, type SchemaField } from "@riducms/protocol";

export function fieldsUseLocalization(fields: readonly SchemaField[]): boolean {
	const visited = new Set<SchemaField>();
	const usesLocalization = (field: SchemaField): boolean => {
		if (visited.has(field)) return false;
		visited.add(field);
		return (
			field.localized === true ||
			field.nested?.fields.some(usesLocalization) === true ||
			resolveBlockTypes(field.blocks).some((block) => block.fields.some(usesLocalization))
		);
	};
	return fields.some(usesLocalization);
}
