import {
	resolveBlockTypes,
	type SchemaCollection,
	type SchemaField,
	type SchemaManifest,
} from "@riducms/protocol";
import type { AdminI18n } from "@riducms/plugin";
import { schemaFieldDefault, schemaFieldRequiredOnCreate } from "@admin/core/schema/field-default";
import { isUploadMetadataField } from "@admin/features/uploads/upload-document-contracts";

export type ReferenceField = {
	path: string;
	type: string;
	outputType: string;
	requiredOnCreate: boolean;
	writable: boolean;
	localized: boolean;
	description: string;
	constraints: string[];
	jsonSchema?: unknown;
};

export type CollectionReferenceSchema = {
	fields: ReferenceField[];
	create: Record<string, unknown>;
	update: Record<string, unknown>;
	omitted: string[];
	filter?: { path: string; value: string | number | boolean };
};

/** Project the canonical manifest into documentation, never form or validation state. */
export function collectionReferenceSchema(
	collection: SchemaCollection,
	manifest: SchemaManifest,
	i18n: AdminI18n
): CollectionReferenceSchema {
	const fields: ReferenceField[] = [];
	const omitted: string[] = [];
	const active = new Set<SchemaField>();
	const pluginFieldTypes = new Map(
		manifest.plugins.flatMap((plugin) => plugin.fieldTypes ?? []).map((type) => [type.key, type])
	);

	function writable(field: SchemaField, root: boolean) {
		return (
			!["ui", "join", "virtual"].includes(field.type) &&
			!(root && collection.capabilities.upload && isUploadMetadataField(field.name))
		);
	}

	function visit(
		items: SchemaField[],
		prefix = "",
		depth = 0,
		inheritedLocale = false,
		inheritedWrite = true,
		ancestorRequiredOnCreate = true
	) {
		for (const field of items) {
			if (field.type === "ui") continue;
			const path = prefix + field.name;
			const localized = inheritedLocale || field.localized === true;
			const input = inheritedWrite && writable(field, prefix === "");
			const requiredOnCreate = ancestorRequiredOnCreate && schemaFieldRequiredOnCreate(field);
			const plugin =
				field.plugin === undefined ? undefined : pluginFieldTypes.get(field.plugin.key);
			const constraints: string[] = [];
			const text = field.text ?? field.textarea ?? field.code;
			const rows = field.list ?? field.nested ?? field.blocks;
			const blocks = resolveBlockTypes(field.blocks);

			if (text?.minLength !== undefined)
				constraints.push(i18n.t("apiReference:minLength", { value: text.minLength }));
			if (text?.maxLength !== undefined)
				constraints.push(i18n.t("apiReference:maxLength", { value: text.maxLength }));
			if (field.number?.min !== undefined)
				constraints.push(i18n.t("apiReference:minimum", { value: field.number.min }));
			if (field.number?.max !== undefined)
				constraints.push(i18n.t("apiReference:maximum", { value: field.number.max }));
			if (rows?.minRows !== undefined)
				constraints.push(i18n.t("apiReference:minItems", { value: rows.minRows }));
			if (rows?.maxRows !== undefined)
				constraints.push(i18n.t("apiReference:maxItems", { value: rows.maxRows }));
			if (field.select)
				constraints.push(
					i18n.t("apiReference:allowedValues", {
						values: field.select.options.map((option) => JSON.stringify(option.value)).join(", "),
					})
				);
			if (field.unique) constraints.push(i18n.t("apiReference:unique"));
			if (field.dynamicDefault) constraints.push(i18n.t("apiReference:dynamicDefault"));
			else if (field.default !== undefined)
				constraints.push(i18n.t("apiReference:defaultValue", { value: field.default }));
			if (field.queryRestricted) constraints.push(i18n.t("apiReference:queryRestricted"));
			if (field.date)
				constraints.push(
					field.date.format === "time"
						? "HH:mm[:ss]"
						: field.date.format === "date"
							? "YYYY-MM-DD"
							: "RFC 3339"
				);

			const targets =
				field.relationship?.targets?.map((target) => target.collectionSlug) ??
				[field.relationship?.collectionSlug ?? field.upload?.collectionSlug].filter(
					(slug): slug is string => slug !== undefined
				);
			if (targets.length)
				constraints.push(i18n.t("apiReference:references", { collections: targets.join(", ") }));
			if (field.relationship?.polymorphic) constraints.push(i18n.t("apiReference:polymorphic"));
			if (field.type === "array") constraints.push(i18n.t("apiReference:rowIdentity"));
			if (field.type === "blocks")
				constraints.push(
					i18n.t("apiReference:blockDiscriminator", {
						values: blocks.map((block) => block.slug).join(", "),
					})
				);

			fields.push({
				path,
				type: fieldType(field, plugin?.typescriptInput),
				outputType: fieldType(field, plugin?.typescriptOutput, true),
				requiredOnCreate,
				writable: input,
				localized,
				description: i18n.text(field.admin.description ?? "", field.admin.descriptionTranslations),
				constraints,
				...(plugin?.jsonSchema === undefined ? {} : { jsonSchema: plugin.jsonSchema }),
			});

			// Recursive named block families stay finite; their discriminator row documents the edge.
			if (depth >= 4 || active.has(field)) {
				if (field.nested || field.blocks) constraints.push(i18n.t("apiReference:furtherNesting"));
				continue;
			}
			active.add(field);
			if (field.nested)
				visit(
					field.nested.fields,
					`${path}${field.type === "array" ? "[]" : ""}.`,
					depth + 1,
					localized,
					input,
					requiredOnCreate
				);
			for (const block of blocks)
				visit(
					block.fields,
					`${path}[${block.slug}].`,
					depth + 1,
					localized,
					input,
					requiredOnCreate
				);
			active.delete(field);
		}
	}

	function sample(items: SchemaField[], prefix = "", depth = 0): Record<string, unknown> {
		const result: Record<string, unknown> = {};
		for (const field of items) {
			if (!writable(field, prefix === "")) continue;
			const path = prefix + field.name;
			if (field.dynamicDefault) continue;

			const textRules = field.text ?? field.textarea ?? field.code;
			const largeExample =
				(field.nested?.minRows ?? field.blocks?.minRows ?? field.list?.minRows ?? 0) > 3 ||
				(textRules?.minLength ?? 0) > 256;
			const unsupportedEmail =
				field.type === "email" &&
				((textRules?.minLength ?? 0) > 76 || (textRules?.maxLength ?? Infinity) < 6);
			if (
				depth >= 4 ||
				active.has(field) ||
				field.type === "plugin" ||
				largeExample ||
				unsupportedEmail
			) {
				omitted.push(path);
				continue;
			}
			active.add(field);
			const blocks = resolveBlockTypes(field.blocks);
			let value: unknown = scalarExample(field);
			if (field.type === "group") value = sample(field.nested?.fields ?? [], `${path}.`, depth + 1);
			if (field.type === "array")
				value =
					field.required || (field.nested?.minRows ?? 0) > 0
						? Array.from({ length: Math.min(3, Math.max(1, field.nested?.minRows ?? 0)) }, () =>
								sample(field.nested?.fields ?? [], `${path}[].`, depth + 1)
							)
						: [];
			if (field.type === "blocks") {
				const block = blocks[0];
				value =
					block && (field.required || (field.blocks?.minRows ?? 0) > 0)
						? Array.from({ length: Math.max(1, field.blocks?.minRows ?? 0) }, () => ({
								blockType: block.slug,
								...sample(block.fields, `${path}[${block.slug}].`, depth + 1),
							}))
						: [];
			}
			active.delete(field);
			if (value !== undefined) result[field.name] = value;
		}
		return result;
	}

	visit(collection.fields);
	const create = sample(collection.fields);
	const update = { ...create };
	const filterField = collection.fields.find(
		(field) =>
			!field.queryRestricted &&
			writable(field, true) &&
			["text", "email", "number", "checkbox", "select", "radio"].includes(field.type) &&
			!field.select?.hasMany
	);
	const value = filterField ? create[filterField.name] : undefined;
	const filter =
		filterField &&
		(typeof value === "string" || typeof value === "number" || typeof value === "boolean")
			? { path: filterField.name, value }
			: undefined;

	return { fields, create, update, omitted: [...new Set(omitted)], filter };
}

function fieldType(field: SchemaField, pluginType?: string, output = false): string {
	if (field.list || field.select?.hasMany)
		return field.type === "number-list" ? "number[]" : "string[]";
	if (field.relationship || field.upload) {
		const value = field.relationship?.polymorphic
			? output
				? "{ relationTo, id: string | object }"
				: "{ relationTo, id }"
			: output
				? "string (ID) | object"
				: "string (ID)";
		return field.relationship?.hasMany || field.upload?.hasMany ? `(${value})[]` : value;
	}
	switch (field.type) {
		case "number":
			return "number";
		case "checkbox":
			return "boolean";
		case "point":
			return "[longitude, latitude]";
		case "group":
			return "object";
		case "array":
		case "blocks":
			return "object[]";
		case "json":
			return "JSON";
		case "plugin":
			return pluginType ?? "JSON";
		case "join":
			return "object[]";
		case "virtual":
			return field.virtual?.valueType ?? "JSON";
		default:
			return "string";
	}
}

function scalarExample(field: SchemaField): unknown {
	const defaultValue = schemaFieldDefault(field);
	if (defaultValue.present) return defaultValue.value;
	const numeric = Math.min(
		field.number?.max ?? Infinity,
		Math.max(field.number?.min ?? -Infinity, 1)
	);
	const textRules = field.text ?? field.textarea ?? field.code;
	const text = "Example "
		.repeat(Math.ceil(Math.max(7, textRules?.minLength ?? 0) / 8))
		.trim()
		.padEnd(textRules?.minLength ?? 0, "x")
		.slice(0, textRules?.maxLength ?? 256);
	const itemCount = Math.min(
		field.list?.maxRows ?? Infinity,
		Math.max(1, field.list?.minRows ?? 0)
	);
	switch (field.type) {
		case "number":
			return numeric;
		case "checkbox":
			return true;
		case "number-list":
			return Array.from({ length: itemCount }, () => numeric);
		case "text-list":
			return Array.from({ length: itemCount }, () => text);
		case "email": {
			const length = Math.min(
				textRules?.maxLength ?? Infinity,
				Math.max(18, textRules?.minLength ?? 0)
			);
			const domain = length >= 13 ? "@example.com" : "@a.co";
			return "person".padEnd(length - domain.length, "x").slice(0, length - domain.length) + domain;
		}
		case "date":
			return field.date?.format === "time"
				? "12:00"
				: field.date?.format === "date-time"
					? "2026-01-01T12:00:00Z"
					: "2026-01-01";
		case "select":
		case "radio":
			return field.select?.hasMany
				? field.select.options.slice(0, 1).map((option) => option.value)
				: field.select?.options[0]?.value;
		case "point":
			return [-0.12, 51.5];
		case "relationship": {
			const value = field.relationship?.polymorphic
				? {
						relationTo: field.relationship.targets?.[0]?.collectionSlug ?? "COLLECTION",
						id: "RELATED_ID",
					}
				: "RELATED_ID";
			return field.relationship?.hasMany ? [value] : value;
		}
		case "upload":
			return field.upload?.hasMany ? ["UPLOAD_ID"] : "UPLOAD_ID";
		case "json":
			return {};
		default:
			return text;
	}
}
