import type { AdminI18n, AdminListCellRenderer } from "@riducms/plugin";
import type { SchemaCollection } from "@riducms/protocol";
import {
	listColumnFields,
	listMetadataFields,
	type ListColumn,
} from "@admin/features/collections/list-workspace";

export function resolveListCells(
	collection: SchemaCollection | undefined,
	renderers: readonly AdminListCellRenderer[]
) {
	return renderers.flatMap((cell) => {
		if (cell.collection !== collection?.slug) return [];

		const field = collection.fields.find(
			(candidate) => candidate.path === cell.field || candidate.name === cell.field
		);
		return field === undefined ? [] : [{ ...cell, field }];
	});
}

export function resolveListColumns(
	collection: SchemaCollection | undefined,
	i18n: AdminI18n,
	cells: ReturnType<typeof resolveListCells> = []
): ListColumn[] {
	return [
		...listColumnFields(collection).map((field) => {
			const cell = cells.find((cell) => cell.field.path === field.path);
			return {
				path: field.path,
				label: cell?.labelKey ? i18n.t(cell.labelKey) : (cell?.label ?? field.admin.label),
				field,
			};
		}),
		...listMetadataFields(collection, i18n).map((field) => ({
			path: field.path,
			label: field.admin.label,
			field,
		})),
	];
}
