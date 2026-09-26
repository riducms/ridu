import type { AdminI18n } from "@riducms/plugin";
import type { SchemaCollection, SchemaField } from "@riducms/protocol";

import type { AdminDocument } from "@admin/core/api/admin-client";
import { readDocumentPath } from "@admin/core/schema/read-document-path";
import { documentLabel } from "@admin/features/documents/document-title";
import { formatDateDisplay } from "@admin/fields/scalar/date-value";

interface CollectionListCellFormatterOptions {
	i18n: AdminI18n;
	get collections(): readonly SchemaCollection[];
	canReadField: (path: string, id: string) => boolean;
}

export function createCollectionListCellFormatter(options: CollectionListCellFormatterOptions) {
	return (document: AdminDocument, field: SchemaField) => {
		const { i18n } = options;
		if (!options.canReadField(field.path, document.id)) return "—";
		const value = readDocumentPath(document, field.path);
		if (value === undefined || value === null || value === "") return "—";
		if (field.type === "checkbox") {
			return value === true ? i18n.t("general:yes") : i18n.t("general:no");
		}
		if (field.type === "select" || field.type === "radio") {
			const values = Array.isArray(value) ? value : [value];
			const labels = values.map((candidate) => {
				const option = field.select?.options.find((item) => item.value === candidate);
				return option === undefined
					? String(candidate)
					: i18n.text(option.label, option.labelTranslations);
			});
			return Array.isArray(value) ? i18n.formatList(labels) : labels[0];
		}
		if (field.type === "array") return summarizeRows(value, "row", i18n);
		if (field.type === "blocks") return summarizeRows(value, "block", i18n);
		if (field.type === "json" || field.plugin !== undefined) return summarizeStructured(value);
		if (field.type === "date") return formatDateDisplay(value, field.date?.format, i18n);
		if (referenceField(field)) {
			return i18n.formatList(
				fieldReferences(field, value).map((reference) => {
					const collection = options.collections.find(
						(candidate) => candidate.slug === reference.collection
					);
					return reference.document === undefined
						? reference.id
						: documentLabel(collection, reference.document);
				})
			);
		}
		if (Array.isArray(value)) {
			return i18n.formatList(value.map((item) => displayReference(item, i18n)));
		}
		if (typeof value === "object") return displayReference(value, i18n);
		return String(value);
	};
}

function referenceField(field: SchemaField) {
	return field.relationship !== undefined || field.upload !== undefined || field.join !== undefined;
}

interface ListReference {
	collection: string;
	id: string;
	document?: AdminDocument;
}

function fieldReferences(field: SchemaField, value: unknown): ListReference[] {
	const values = Array.isArray(value) ? value : [value];
	return values.flatMap((candidate): ListReference[] => {
		const configuredTarget =
			field.relationship?.collectionSlug ??
			field.upload?.collectionSlug ??
			field.join?.collectionSlug;
		if (typeof candidate === "string") {
			return configuredTarget === undefined
				? []
				: [{ collection: configuredTarget, id: candidate }];
		}
		if (typeof candidate !== "object" || candidate === null) return [];
		const record = candidate as Record<string, unknown>;
		const collection = typeof record.relationTo === "string" ? record.relationTo : configuredTarget;
		if (collection === undefined) return [];
		if (record.relationTo !== undefined) {
			if (typeof record.id === "string") return [{ collection, id: record.id }];
			if (documentValue(record.id)) {
				return [{ collection, id: record.id.id, document: record.id }];
			}
			return [];
		}
		return documentValue(record) ? [{ collection, id: record.id, document: record }] : [];
	});
}

function documentValue(value: unknown): value is AdminDocument {
	return (
		typeof value === "object" && value !== null && typeof Reflect.get(value, "id") === "string"
	);
}

function displayReference(value: unknown, i18n: AdminI18n) {
	if (typeof value === "string") return value;
	if (typeof value !== "object" || value === null) return String(value ?? "—");
	const record = value as Record<string, unknown>;
	return String(
		record.title ??
			record.name ??
			record.email ??
			record.id ??
			i18n.t("collections:relatedDocument")
	);
}

function summarizeRows(value: unknown, singular: "row" | "block", i18n: AdminI18n) {
	if (!Array.isArray(value) || value.length === 0) return "—";
	const previews = value.slice(0, 2).map((row, index) => {
		if (typeof row !== "object" || row === null) return String(row);
		const record = row as Record<string, unknown>;
		return String(
			record.label ??
				record.title ??
				record.heading ??
				record.name ??
				record.blockType ??
				i18n.t(singular === "row" ? "collections:rowNumber" : "collections:blockNumber", {
					number: i18n.formatNumber(index + 1),
				})
		);
	});
	const summary = i18n.formatList(previews);
	return value.length > previews.length
		? i18n.t("collections:moreItems", {
				summary,
				count: i18n.formatNumber(value.length - previews.length),
			})
		: summary;
}

function summarizeStructured(value: unknown) {
	if (value === undefined || value === null) return "—";
	if (typeof value !== "object") return String(value);
	const text = JSON.stringify(value);
	return text.length > 72 ? `${text.slice(0, 69)}…` : text;
}
