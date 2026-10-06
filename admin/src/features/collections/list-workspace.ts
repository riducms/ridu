import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import type { AdminI18n } from "@riducms/translations";
import type { ListFilterFields } from "@admin/features/collections/list-filter-fields";

export const listPageSizes = [10, 25, 50, 100] as const;

export type ListPageSize = (typeof listPageSizes)[number];
export type ListFilterOperator =
	| "equals"
	| "notEquals"
	| "like"
	| "contains"
	| "greaterThan"
	| "greaterThanEqual"
	| "lessThan"
	| "lessThanEqual"
	| "exists"
	| "in"
	| "notIn";

export interface ListFilter {
	field: string;
	operator: ListFilterOperator;
	/**
	 * The operand, or the candidates of `in` and `notIn`. A polymorphic relationship's
	 * candidate is `collection:id`; collection slugs never contain a colon.
	 */
	value: string | string[];
}

export interface ListColumn {
	path: string;
	label: string;
	field?: SchemaField;
}

/** OR groups containing AND conditions. */
export type ListFilterGroup = ListFilter[];

export interface ListColumnSelection {
	path: string;
	active: boolean;
}

export interface ListWorkspacePreference {
	columns: ListColumnSelection[];
	limit: ListPageSize;
}

/** The resolved list view, also stored when an author saves a named view. */
export interface ListViewState extends ListWorkspacePreference {
	q: string;
	status: string;
	folder: string;
	view: "list" | "hierarchy";
	filters: ListFilterGroup[];
	sort: string;
}

const textOperators: readonly ListFilterOperator[] = [
	"equals",
	"like",
	"notEquals",
	"contains",
	"exists",
];
const orderedOperators: readonly ListFilterOperator[] = [
	"equals",
	"notEquals",
	"greaterThan",
	"greaterThanEqual",
	"lessThan",
	"lessThanEqual",
	"exists",
];
const identityOperators: readonly ListFilterOperator[] = ["equals", "notEquals", "exists"];
const membershipOperators: readonly ListFilterOperator[] = ["in", "notIn", "exists"];

/**
 * Lists, has-many fields and polymorphic relationships hold a set of items, so the server
 * filters them by membership alone.
 */
export function membershipField(field: SchemaField) {
	switch (field.type) {
		case "text-list":
		case "number-list":
			return true;
		case "select":
			return field.select?.hasMany === true;
		case "relationship":
			return field.relationship?.hasMany === true || field.relationship?.polymorphic === true;
		case "upload":
			return field.upload?.hasMany === true;
		default:
			return false;
	}
}

export function membershipOperator(operator: ListFilterOperator) {
	return operator === "in" || operator === "notIn";
}

/** Encodes one polymorphic relationship candidate for a list filter. */
export function referenceCandidate(relationTo: string, id: string) {
	return `${relationTo}:${id}`;
}

export function parseReferenceCandidate(candidate: string) {
	const cut = candidate.indexOf(":");
	if (cut <= 0) return undefined;
	return { relationTo: candidate.slice(0, cut), id: candidate.slice(cut + 1) };
}

/**
 * The `in` candidates a membership condition sends: numbers for a number list and
 * `{ relationTo, id }` references for a polymorphic relationship. Undefined when incomplete.
 */
export function membershipCandidates(filter: ListFilter, field: SchemaField) {
	if (typeof filter.value === "string" || !listFilterComplete(filter, field)) return undefined;
	const candidates = filter.value.map((candidate) => {
		if (field.relationship?.polymorphic === true) return parseReferenceCandidate(candidate)!;
		return field.type === "number-list" ? Number(candidate) : candidate;
	});
	return candidates.every(
		(candidate) => typeof candidate !== "number" || Number.isFinite(candidate)
	)
		? candidates
		: undefined;
}

/** Whether a draft condition says enough to be applied. */
export function listFilterComplete(filter: ListFilter, field: SchemaField) {
	if (filter.operator === "exists") return true;
	if (typeof filter.value === "string") return filter.value !== "";
	return (
		filter.value.length > 0 &&
		filter.value.every((candidate) =>
			field.relationship?.polymorphic === true
				? (parseReferenceCandidate(candidate)?.id ?? "") !== ""
				: candidate !== ""
		)
	);
}

export function listColumnFields(collection: SchemaCollection | undefined) {
	return (collection?.fields ?? []).flatMap((field) => {
		if (field.type === "ui") return [];
		if (field.type === "group") return groupColumnFields(field);
		return [field];
	});
}

export function bulkEditableListFields(fields: readonly SchemaField[]) {
	return fields.filter(
		(field) =>
			field.category === "scalar" &&
			!field.localized &&
			!field.unique &&
			field.virtual === undefined &&
			!field.admin.hidden &&
			!field.admin.readOnly &&
			field.admin.condition === undefined &&
			(field.type !== "select" || !field.select?.hasMany) &&
			["text", "textarea", "email", "date", "number", "checkbox", "select", "radio"].includes(
				field.type
			)
	);
}

export function sortableField(field: SchemaField) {
	return (
		field.queryRestricted !== true &&
		field.virtual === undefined &&
		field.join === undefined &&
		field.type !== "text-list" &&
		field.type !== "number-list" &&
		field.type !== "array" &&
		field.type !== "blocks" &&
		field.type !== "group" &&
		field.type !== "json" &&
		field.plugin === undefined &&
		field.type !== "point" &&
		field.type !== "code"
	);
}

export function defaultListColumns(
	collection: SchemaCollection | undefined,
	fields: readonly ListColumn[]
) {
	const byName = new Map(fields.map((field) => [field.path, field.path]));
	for (const field of fields) {
		if (
			field.field !== undefined &&
			field.path === field.field.name &&
			!byName.has(field.field.name)
		)
			byName.set(field.field!.name, field.path);
	}
	const configured = (collection?.admin.defaultColumns ?? []).flatMap((name) => {
		const path = byName.get(name);
		return path === undefined ? [] : [path];
	});
	if (configured.length > 0) return configured;
	return [...new Set([fields[0]?.path ?? "id", "id", "updatedAt", "createdAt"])];
}

export function filterOperatorsFor(field: SchemaField): readonly ListFilterOperator[] {
	if (membershipField(field)) return membershipOperators;
	if (field.type === "number" || field.type === "date") return orderedOperators;
	if (field.type === "text" || field.type === "textarea" || field.type === "email") {
		return textOperators;
	}
	return identityOperators;
}

export function filterOperatorLabel(operator: ListFilterOperator, i18n: AdminI18n) {
	const labels: Record<ListFilterOperator, Parameters<AdminI18n["t"]>[0]> = {
		in: "collections:operatorIsAnyOf",
		notIn: "collections:operatorIsNoneOf",
		equals: "collections:operatorEquals",
		notEquals: "collections:operatorNotEquals",
		like: "collections:operatorLike",
		contains: "collections:operatorContains",
		greaterThan: "collections:operatorGreaterThan",
		greaterThanEqual: "collections:operatorGreaterThanEqual",
		lessThan: "collections:operatorLessThan",
		lessThanEqual: "collections:operatorLessThanEqual",
		exists: "collections:operatorExists",
	};
	return i18n.t(labels[operator]);
}

/** Resolves a filter's dotted field path without enumerating the schema. */
export type ListFilterFieldLookup = Pick<ListFilterFields, "resolve">;

export function normalizeListFilters(
	value: unknown,
	fields: ListFilterFieldLookup
): ListFilterGroup[] {
	if (!Array.isArray(value)) return [];
	return value
		.map((group) => normalizeFilterGroup(group, fields))
		.filter((group) => group.length > 0);
}

function normalizeFilterGroup(value: unknown, fields: ListFilterFieldLookup): ListFilter[] {
	if (!Array.isArray(value)) return [];
	return value.flatMap((candidate) => {
		if (typeof candidate !== "object" || candidate === null) return [];
		const fieldName = Reflect.get(candidate, "field");
		const operator = Reflect.get(candidate, "operator");
		const rawValue = Reflect.get(candidate, "value");
		if (typeof fieldName !== "string" || typeof operator !== "string") return [];
		const field = fields.resolve(fieldName)?.field;
		if (
			field === undefined ||
			!filterOperatorsFor(field).includes(operator as ListFilterOperator)
		) {
			return [];
		}
		const scalar = (raw: unknown) =>
			typeof raw === "string" || typeof raw === "number" || typeof raw === "boolean"
				? String(raw)
				: "";
		return [
			{
				field: fieldName,
				operator: operator as ListFilterOperator,
				value: membershipOperator(operator as ListFilterOperator)
					? (Array.isArray(rawValue) ? rawValue : [rawValue]).map(scalar).filter(Boolean)
					: scalar(rawValue),
			},
		];
	});
}

export function parseListFilters(encoded: string | null, fields: ListFilterFieldLookup) {
	if (encoded === null || encoded === "") return [];
	try {
		return normalizeListFilters(JSON.parse(encoded), fields);
	} catch {
		return [];
	}
}

export function parseListPageSize(value: string | null, fallback: ListPageSize = 10): ListPageSize {
	const parsed = Number(value);
	return listPageSizes.includes(parsed as ListPageSize) ? (parsed as ListPageSize) : fallback;
}

export function normalizeWorkspacePreference(
	value: unknown,
	fields: readonly Pick<ListColumn, "path">[],
	fallback: ListWorkspacePreference
): ListWorkspacePreference {
	if (typeof value !== "object" || value === null) return fallback;
	const columns = normalizeColumnSelection(Reflect.get(value, "columns"), fields, fallback.columns);
	const rawLimit = Reflect.get(value, "limit");
	return {
		columns,
		limit: parseListPageSize(
			typeof rawLimit === "number" ? String(rawLimit) : null,
			fallback.limit
		),
	};
}

export function normalizeColumnSelection(
	value: unknown,
	fields: readonly { path: string }[],
	fallback: readonly ListColumnSelection[]
): ListColumnSelection[] {
	const eligible = new Set(fields.map((field) => field.path));
	const seen = new Set<string>();
	const columns = (Array.isArray(value) ? value : fallback).flatMap((candidate: unknown) => {
		if (typeof candidate !== "object" || candidate === null) return [];
		const path = Reflect.get(candidate, "path"),
			active = Reflect.get(candidate, "active");
		if (
			typeof path !== "string" ||
			typeof active !== "boolean" ||
			!eligible.has(path) ||
			seen.has(path)
		)
			return [];
		seen.add(path);
		return [{ path, active }];
	});
	return [
		...columns,
		...fields
			.filter((field) => !seen.has(field.path))
			.map((field) => ({ path: field.path, active: false })),
	];
}

export function encodeColumnSelection(columns: readonly ListColumnSelection[]) {
	return columns.map((column) => (column.active ? column.path : `-${column.path}`)).join(",");
}

/** Group leaves become columns; repeated values are not flattened into table cells. */
function groupColumnFields(group: SchemaField): SchemaField[] {
	return (group.nested?.fields ?? []).flatMap((child) => {
		if (child.type === "blocks") return [];
		const field = group.queryRestricted ? { ...child, queryRestricted: true } : child;
		const labelled = {
			...field,
			admin: { ...field.admin, label: `${group.admin.label} > ${field.admin.label}` },
		};
		return child.type === "group" ? groupColumnFields(labelled) : [labelled];
	});
}

/** Queryable document metadata is owned by the runtime, rather than application fields. */
export function listMetadataFields(
	collection: SchemaCollection | undefined,
	i18n: AdminI18n
): SchemaField[] {
	const fields: SchemaField[] = [
		{
			id: "id",
			name: "id",
			path: "id",
			type: "text",
			category: "scalar",
			required: false,
			unique: false,
			admin: { label: "ID" },
		},
		...(["createdAt", "updatedAt"] as const).map((path) => ({
			id: path,
			name: path,
			path,
			type: "date" as const,
			date: { format: "date-time" as const },
			category: "scalar" as const,
			required: false,
			unique: false,
			admin: {
				label: i18n.t(path === "createdAt" ? "collections:createdAt" : "collections:updatedAt"),
			},
		})),
	];
	if (collection?.capabilities.versions)
		fields.push({
			id: "_status",
			name: "_status",
			path: "_status",
			type: "select",
			category: "scalar",
			required: false,
			unique: false,
			admin: {
				label: i18n.t(
					collection.fields.some((field) => field.name === "status")
						? "collections:publicationStatus"
						: "documents:status"
				),
			},
			select: {
				hasMany: false,
				options: [
					{ label: i18n.t("documents:draft"), value: "draft" },
					{ label: i18n.t("documents:published"), value: "published" },
				],
			},
		});
	return fields;
}
