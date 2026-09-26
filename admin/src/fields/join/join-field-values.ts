import { isRecord } from "@riducms/protocol";
import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import type { AdminDocument } from "@admin/core/api/admin-client";
import { readDocumentPath } from "@admin/core/schema/read-document-path";
import { documentTitleField } from "@admin/features/documents/document-title";
import type { ListColumn, ListColumnSelection } from "@admin/features/collections/list-workspace";

export function joinDocuments(value: unknown) {
	return Array.isArray(value)
		? value.filter(
				(document): document is AdminDocument =>
					isRecord(document) && typeof document.id === "string"
			)
		: [];
}

export function defaultJoinColumns(
	field: SchemaField,
	collection: SchemaCollection | undefined,
	available: readonly ListColumn[]
): ListColumnSelection[] {
	const configured = field.join?.defaultColumns ?? collection?.admin.defaultColumns ?? [];
	const defaults =
		configured.length > 0
			? configured
			: [documentTitleField(collection)?.path ?? "id", "updatedAt"];
	const selected = new Set(defaults);
	const byPath = new Set(available.map((column) => column.path));
	return [
		...defaults.filter((path) => byPath.has(path)).map((path) => ({ path, active: true })),
		...available
			.filter((column) => !selected.has(column.path))
			.map((column) => ({ path: column.path, active: false })),
	];
}

export function joinDefaultValues(path: string, id: string) {
	const values: Record<string, unknown> = {};
	const segments = path.split(".").filter(Boolean);
	let current = values;
	for (const segment of segments.slice(0, -1)) {
		const nested: Record<string, unknown> = {};
		current[segment] = nested;
		current = nested;
	}
	const last = segments.at(-1);
	if (last !== undefined) current[last] = id;
	return values;
}

/** Resolve either a monomorphic reference or the populated { relationTo, id: document } shape. */
export function joinReference(
	value: unknown,
	field: SchemaField,
	collections: readonly SchemaCollection[]
) {
	const configured = field.relationship?.collectionSlug ?? field.upload?.collectionSlug;
	const slug =
		isRecord(value) && typeof value.relationTo === "string" ? value.relationTo : configured;
	const collection = collections.find((collection) => collection.slug === slug);
	const document = isRecord(value) ? (isRecord(value.id) ? value.id : value) : undefined;
	const id = typeof value === "string" ? value : document?.id;
	if (!collection || typeof id !== "string") return undefined;

	const title = documentTitleField(collection);
	const populated = title ? readDocumentPath(document, title.path) : undefined;
	return { collection, id, key: `${collection.slug}\u001f${id}`, populated };
}

/** Sort stored scalar values; localized cell text is never a date or numeric sort key. */
export function sortJoinDocuments(
	documents: readonly AdminDocument[],
	sort: string,
	columns: readonly ListColumn[],
	language: string,
	referenceLabel: (value: unknown, field: SchemaField) => string
) {
	if (sort === "") return documents;

	const descending = sort.startsWith("-");
	const path = descending ? sort.slice(1) : sort;
	const field = columns.find((column) => column.path === path)?.field;
	const collator = new Intl.Collator(language, { numeric: true });
	const key = (document: AdminDocument): string | number | boolean | undefined => {
		const value = readDocumentPath(document, path);
		if (value === undefined || value === null || value === "") return undefined;
		if (field?.relationship || field?.upload) return referenceLabel(value, field);
		if (field?.type === "date" && field.date?.format !== "time") {
			const time = Date.parse(String(value));
			return Number.isNaN(time) ? undefined : time;
		}
		return typeof value === "number" || typeof value === "boolean" || typeof value === "string"
			? value
			: undefined;
	};
	return documents
		.map((document) => ({ document, value: key(document) }))
		.sort((left, right) => {
			// Keep absent/redacted values last in either direction.
			if (left.value === undefined) return right.value === undefined ? 0 : 1;
			if (right.value === undefined) return -1;
			const comparison =
				typeof left.value === "number" && typeof right.value === "number"
					? left.value - right.value
					: typeof left.value === "boolean" && typeof right.value === "boolean"
						? Number(left.value) - Number(right.value)
						: collator.compare(String(left.value), String(right.value));
			return descending ? -comparison : comparison;
		})
		.map(({ document }) => document);
}
