import type { SchemaField } from "@riducms/protocol";
import {
	normalizeListFilters,
	type ListFilterGroup,
} from "@admin/features/collections/list-workspace";

/** Translate the list control's draft values into the SDK's typed query operands. */
export function referenceListWhere(
	groups: readonly ListFilterGroup[],
	fields: readonly SchemaField[]
) {
	const predicates = normalizeListFilters(groups, fields).flatMap<Record<string, unknown>>(
		(group) => {
			const conditions = group.flatMap((filter) => {
				const field = fields.find((field) => field.path === filter.field)!;
				let value: string | number | boolean = filter.value;

				if (filter.operator === "exists") value = value !== "false";
				else if (value === "" && field.type !== "text-list") return [];
				else if (field.type === "number" || field.type === "number-list") {
					value = Number(value);
					if (!Number.isFinite(value)) return [];
				} else if (field.type === "checkbox") value = value === "true";

				return [
					{ [filter.field]: { [filter.operator]: filter.operator === "in" ? [value] : value } },
				];
			});

			return conditions.length > 1 ? [{ and: conditions }] : conditions;
		}
	);

	return predicates.length > 1 ? { or: predicates } : predicates[0];
}
