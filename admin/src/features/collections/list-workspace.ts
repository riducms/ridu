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

export interface ListWorkspacePreference {
	columns: string[];
	showStatus: boolean;
	showID: boolean;
	showCreated: boolean;
	showUpdated: boolean;
	limit: ListPageSize;
}

const textOperators: readonly ListFilterOperator[] = [
	"like",
	"equals",
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

export function listColumnFields(collection: SchemaCollection | undefined, titleName?: string) {
	return (collection?.fields ?? []).flatMap((field) => {
		if (field.name === titleName || field.type === "ui") return [];
		if (field.type === "group") return nestedFields(field, "columns");
		return [field];
	});
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
	fields: readonly SchemaField[]
) {
	const byName = new Map(fields.map((field) => [field.path, field.path]));
	for (const field of fields) {
		if (field.path === field.name && !byName.has(field.name)) byName.set(field.name, field.path);
	}
	const configured = (collection?.admin.defaultColumns ?? []).flatMap((name) => {
		const path = byName.get(name);
		return path === undefined ? [] : [path];
	});
	if (configured.length > 0) return configured;
	return fields.slice(0, 2).map((field) => field.path);
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

export function normalizeListFilters(value: unknown, fields: readonly SchemaField[]): ListFilter[] {
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

export function buildListFilterWhere(
	filters: readonly ListFilter[],
	fields: readonly SchemaField[]
) {
	const byName = new Map(fields.map((field) => [field.path, field]));
	return filters.flatMap((filter): Record<string, unknown>[] => {
		const field = byName.get(filter.field);
		if (field === undefined || !filterOperatorsFor(field).includes(filter.operator)) return [];
		if (filter.operator === "exists") {
			return [{ [filter.field]: { exists: filter.value !== "false" } }];
		}
		if (filter.value === "" && field.type !== "text-list") return [];
		let value: string | number | boolean = filter.value;
		if (field.type === "number" || field.type === "number-list") {
			value = Number(filter.value);
			if (!Number.isFinite(value)) return [];
		} else if (field.type === "checkbox") {
			value = filter.value === "true";
		}
		return [{ [filter.field]: { [filter.operator]: filter.operator === "in" ? [value] : value } }];
	});
}

export function parseListPageSize(value: string | null, fallback: ListPageSize = 25): ListPageSize {
	const parsed = Number(value);
	return listPageSizes.includes(parsed as ListPageSize) ? (parsed as ListPageSize) : fallback;
}

export function normalizeWorkspacePreference(
	value: unknown,
	fields: readonly SchemaField[],
	fallback: ListWorkspacePreference
): ListWorkspacePreference {
	if (typeof value !== "object" || value === null) return fallback;
	const eligible = new Set(fields.map((field) => field.path));
	const rawColumns = Reflect.get(value, "columns");
	const columns = Array.isArray(rawColumns)
		? rawColumns.filter((name): name is string => typeof name === "string" && eligible.has(name))
		: fallback.columns;
	const showStatus = Reflect.get(value, "showStatus");
	const showID = Reflect.get(value, "showID");
	const showCreated = Reflect.get(value, "showCreated");
	const showUpdated = Reflect.get(value, "showUpdated");
	const rawLimit = Reflect.get(value, "limit");
	return {
		columns,
		showStatus: typeof showStatus === "boolean" ? showStatus : fallback.showStatus,
		showID: typeof showID === "boolean" ? showID : fallback.showID,
		showCreated: typeof showCreated === "boolean" ? showCreated : fallback.showCreated,
		showUpdated: typeof showUpdated === "boolean" ? showUpdated : fallback.showUpdated,
		limit: parseListPageSize(
			typeof rawLimit === "number" ? String(rawLimit) : null,
			fallback.limit
		),
	};
}

export function visibleColumnNames(encoded: string | null, fallback: readonly string[]) {
	if (encoded === null) return [...fallback];
	const names = encoded.split(",").filter(Boolean);
	return [...new Set(names)];
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
