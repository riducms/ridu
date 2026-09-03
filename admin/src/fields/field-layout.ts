import type {
	SchemaField,
	SchemaFieldCollapsible,
	SchemaFieldRow,
	SchemaFieldTabGroup,
} from "@riducms/protocol";

export interface FieldLayoutGroup {
	key: string;
	row?: SchemaFieldRow;
	collapsible?: SchemaFieldCollapsible;
	tabGroup?: SchemaFieldTabGroup;
	fields: [SchemaField, ...SchemaField[]];
}

// Row metadata is deliberately carried by ordinary data fields. Grouping it
// here preserves the manifest's field order without introducing a form value.
export function createFieldLayout(
	fields: readonly SchemaField[],
	suppressedTabGroupID = ""
): FieldLayoutGroup[] {
	const groups: FieldLayoutGroup[] = [];
	for (const field of fields) {
		const row = field.admin.row;
		const collapsible = field.admin.collapsible;
		const tabGroup =
			field.admin.tabGroup?.id === suppressedTabGroupID ? undefined : field.admin.tabGroup;
		const previous = groups.at(-1);
		if (tabGroup !== undefined && previous?.tabGroup?.id === tabGroup.id) {
			previous.fields.push(field);
			continue;
		}
		if (collapsible !== undefined && previous?.collapsible?.id === collapsible.id) {
			previous.fields.push(field);
			continue;
		}
		if (row !== undefined && previous?.row?.id === row.id) {
			previous.fields.push(field);
			continue;
		}
		groups.push({
			key:
				tabGroup !== undefined
					? `tabs:${tabGroup.id}:${field.id}`
					: collapsible !== undefined
						? `collapsible:${collapsible.id}`
						: row === undefined
							? `field:${field.id}`
							: `row:${row.id}`,
			...(row === undefined ? {} : { row }),
			...(collapsible === undefined ? {} : { collapsible }),
			...(tabGroup === undefined ? {} : { tabGroup }),
			fields: [field],
		});
	}
	return groups;
}
