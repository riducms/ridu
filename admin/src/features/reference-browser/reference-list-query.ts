import {
	membershipCandidates,
	membershipOperator,
	normalizeListFilters,
	type ListFilterFieldLookup,
	type ListFilterGroup,
} from "@admin/features/collections/list-workspace";

/** Translate the list control's draft values into the SDK's typed query operands. */
export function referenceListWhere(
	groups: readonly ListFilterGroup[],
	fields: ListFilterFieldLookup
) {
	const predicates = normalizeListFilters(groups, fields).flatMap<Record<string, unknown>>(
		(group) => {
			const conditions = group.flatMap<Record<string, unknown>>((filter) => {
				const field = fields.resolve(filter.field)!.field;
				if (membershipOperator(filter.operator)) {
					const candidates = membershipCandidates(filter, field);
					if (candidates === undefined) return [];
					const membership = { [filter.field]: { in: candidates } };
					// The query language negates membership rather than naming its opposite.
					return [filter.operator === "notIn" ? { not: membership } : membership];
				}

				let value: string | number | boolean = typeof filter.value === "string" ? filter.value : "";
				if (filter.operator === "exists") value = value !== "false";
				else if (value === "") return [];
				else if (field.type === "number") {
					value = Number(value);
					if (!Number.isFinite(value)) return [];
				} else if (field.type === "checkbox") value = value === "true";

				return [{ [filter.field]: { [filter.operator]: value } }];
			});

			return conditions.length > 1 ? [{ and: conditions }] : conditions;
		}
	);

	return predicates.length > 1 ? { or: predicates } : predicates[0];
}
