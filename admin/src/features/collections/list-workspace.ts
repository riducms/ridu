import { resolveBlockTypes } from "@riducms/protocol";
import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import type { AdminI18n } from "@riducms/translations";

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
	| "in";

export interface ListFilter {
	field: string;
	operator: ListFilterOperator;
	value: string;
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

export function listColumnFields(collection: SchemaCollection | undefined) {
	return (collection?.fields ?? []).flatMap((field) => {
		if (field.type === "ui") return [];
		if (field.type === "group") return nestedFields(field, "columns");
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

export function filterableFields(fields: readonly SchemaField[]) {
	return fields.flatMap((field) => {
		if (field.queryRestricted) return [];
		if (field.type === "group" || field.type === "array" || field.type === "blocks") {
			return nestedFields(field, "filters");
		}
		return filterableLeaf(field) ? [field] : [];
	});
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
	if (field.type === "text-list" || field.type === "number-list") return ["in", "exists"];
	if (field.type === "number" || field.type === "date") return orderedOperators;
	if (field.type === "text" || field.type === "textarea" || field.type === "email") {
		return textOperators;
	}
	return identityOperators;
}

export function filterOperatorLabel(operator: ListFilterOperator, i18n: AdminI18n) {
	const labels: Record<ListFilterOperator, Parameters<AdminI18n["t"]>[0]> = {
		in: "collections:operatorIncludesItem",
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

export function normalizeListFilters(
	value: unknown,
	fields: readonly SchemaField[]
): ListFilterGroup[] {
	if (!Array.isArray(value)) return [];
	return value
		.map((group) => normalizeFilterGroup(group, fields))
		.filter((group) => group.length > 0);
}

function normalizeFilterGroup(value: unknown, fields: readonly SchemaField[]): ListFilter[] {
	if (!Array.isArray(value)) return [];
	const byName = new Map(fields.map((field) => [field.path, field]));
	return value.flatMap((candidate) => {
		if (typeof candidate !== "object" || candidate === null) return [];
		const fieldName = Reflect.get(candidate, "field");
		const operator = Reflect.get(candidate, "operator");
		const rawValue = Reflect.get(candidate, "value");
		if (typeof fieldName !== "string" || typeof operator !== "string") return [];
		const field = byName.get(fieldName);
		if (
			field === undefined ||
			!filterOperatorsFor(field).includes(operator as ListFilterOperator)
		) {
			return [];
		}
		return [
			{
				field: fieldName,
				operator: operator as ListFilterOperator,
				value:
					typeof rawValue === "string" ||
					typeof rawValue === "number" ||
					typeof rawValue === "boolean"
						? String(rawValue)
						: "",
			},
		];
	});
}

export function parseListFilters(encoded: string | null, fields: readonly SchemaField[]) {
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

function nestedFields(field: SchemaField, mode: "columns" | "filters"): SchemaField[] {
	if (mode === "filters" && field.queryRestricted) return [];
	if (field.type === "blocks") {
		if (mode === "columns") return [];
		return (resolveBlockTypes(field.blocks) ?? []).flatMap((block) =>
			block.fields.flatMap((child) =>
				withListLabel(child, `${field.admin.label} > ${block.labels.singular}`, mode)
			)
		);
	}
	return (field.nested?.fields ?? []).flatMap((child) =>
		withListLabel(
			field.queryRestricted ? { ...child, queryRestricted: true } : child,
			field.admin.label,
			mode
		)
	);
}

function withListLabel(
	field: SchemaField,
	prefix: string,
	mode: "columns" | "filters"
): SchemaField[] {
	const labelled = {
		...field,
		admin: { ...field.admin, label: `${prefix} > ${field.admin.label}` },
	};
	if (field.type === "group" || (mode === "filters" && field.type === "array")) {
		return nestedFields(labelled, mode);
	}
	if (field.type === "blocks") return mode === "filters" ? nestedFields(labelled, mode) : [];
	if (mode === "filters" && !filterableLeaf(field)) return [];
	return [labelled];
}

function filterableLeaf(field: SchemaField) {
	return (
		field.queryRestricted !== true &&
		field.virtual === undefined &&
		field.join === undefined &&
		field.plugin === undefined &&
		field.type !== "ui" &&
		field.type !== "json" &&
		field.type !== "point" &&
		field.type !== "code"
	);
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
