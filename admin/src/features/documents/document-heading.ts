import type { SchemaCollection, SchemaField } from "@riducms/protocol";

export function documentHeadingField(
	collection: SchemaCollection | undefined,
	canRead: (path: string) => boolean
): SchemaField | undefined {
	if (collection === undefined || collection.capabilities.upload) return undefined;

	const configured = collection.admin.useAsTitle;
	if (configured !== undefined) {
		const field = collection.fields.find(
			(candidate) => candidate.name === configured && canRead(candidate.path)
		);
		if (field !== undefined) return field;
	}

	return collection.fields.find(
		(field) => canRead(field.path) && (field.type === "text" || field.type === "select")
	);
}
