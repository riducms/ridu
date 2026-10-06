import {
	bindSchemaManifest,
	type SchemaBlockLabels,
	type SchemaBlockType,
	type SchemaField,
} from "@riducms/protocol";

/**
 * Binds `fields` as one collection's fields whose block containers select `blocks`
 * from the manifest registry, as the admin runtime binds a prepared manifest.
 * Definition fields use definition-relative paths and IDs (`title`,
 * `block-hero-title`); their placement views derive absolute ones
 * (`layout.hero.title`, `pages-layout-hero-title`). The fields are bound in place;
 * the definitions are copied first because binding freezes them, and fixtures
 * often share their field objects with other schemas.
 */
export function bindBlockFields<const Fields extends readonly SchemaField[]>(
	blocks: SchemaBlockType[],
	fields: Fields,
	resource = "pages"
): Fields {
	bindSchemaManifest({
		blocks: structuredClone(blocks),
		globals: [],
		collections: [
			{
				id: resource,
				slug: resource,
				labels: { singular: resource, plural: resource },
				admin: {},
				capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
				fields: [...fields],
			},
		],
	});
	return fields;
}

/** Binds one field as {@link bindBlockFields} does. */
export function bindBlockField<Field extends SchemaField>(
	blocks: SchemaBlockType[],
	field: Field,
	resource = "pages"
): Field {
	return bindBlockFields(blocks, [field], resource)[0]!;
}

/**
 * A registry definition whose fields, including nested group and array children,
 * take their definition-relative paths and IDs from their names, as the server
 * resolves them: `links` / `block-hero-links`, `links.label` / `block-hero-links-label`.
 */
export function blockDefinition(
	slug: string,
	fields: SchemaField[],
	labels: SchemaBlockLabels = { singular: slug, plural: slug }
): SchemaBlockType {
	return { slug, labels, fields: definitionFields(slug, fields, []) };
}

function definitionFields(slug: string, fields: SchemaField[], prefix: string[]): SchemaField[] {
	return fields.map((field) => {
		const path = [...prefix, field.name];
		const result: SchemaField = {
			...field,
			path: path.join("."),
			id: definitionFieldID(slug, path),
		};
		if (field.nested)
			result.nested = {
				...field.nested,
				fields: definitionFields(slug, field.nested.fields, path),
			};
		return result;
	});
}

function definitionFieldID(slug: string, path: string[]) {
	const segments = path.map((part) =>
		part
			.replace(/([A-Z])/g, "-$1")
			.replace(/[_-]+/g, "-")
			.replace(/^-/, "")
			.toLowerCase()
	);
	return ["block", slug, ...segments].join("-");
}
