import { scalarLiteralValue } from "@admin/core/forms/scalar-literal";
import { readDocumentPath } from "@admin/core/schema/read-document-path";
import type { FieldReferenceFilter } from "@riducms/plugin";
import type { SchemaField } from "@riducms/protocol";

export type RelationshipFilter = FieldReferenceFilter | readonly FieldReferenceFilter[] | undefined;

export function buildRelationshipWhere(
	searchField: string | undefined,
	search: string,
	filter: RelationshipFilter
): Record<string, unknown> | undefined {
	const predicates: Record<string, unknown>[] = [];
	const query = search.trim();
	if (searchField !== undefined && query !== "") {
		predicates.push({ [searchField]: { like: query } });
	}
	const filters = filter === undefined ? [] : Array.isArray(filter) ? filter : [filter];
	for (const candidate of filters) {
		predicates.push({ [candidate.field]: { [candidate.operator]: candidate.value } });
	}
	if (predicates.length === 0) return undefined;
	if (predicates.length === 1) return predicates[0];
	return { and: predicates };
}

export function combineRelationshipFilters(
	...filters: readonly RelationshipFilter[]
): RelationshipFilter {
	const combined = filters.flatMap((filter) =>
		filter === undefined ? [] : Array.isArray(filter) ? filter : [filter]
	);
	return combined.length === 0 ? undefined : combined;
}

export function relationshipOptionFilters(
	field: SchemaField,
	values: Record<string, unknown> | ((path: string) => unknown),
	targetCollection: string | undefined
): readonly FieldReferenceFilter[] | undefined {
	const reference = field.relationship ?? field.upload;
	const configured = reference?.optionFilters ?? [];
	const filters = configured.flatMap((candidate) => {
		if (candidate.collectionSlug !== undefined && candidate.collectionSlug !== targetCollection) {
			return [];
		}
		const value =
			candidate.value === undefined
				? candidate.sourcePath === undefined
					? undefined
					: typeof values === "function"
						? values(candidate.sourcePath)
						: readDocumentPath(values, candidate.sourcePath)
				: scalarLiteralValue(candidate.value);
		if (typeof value !== "string" && typeof value !== "number" && typeof value !== "boolean") {
			return [];
		}
		return [
			{
				field: candidate.targetPath,
				operator: candidate.operator ?? "equals",
				value,
			} satisfies FieldReferenceFilter,
		];
	});
	return filters.length === 0 ? undefined : filters;
}
