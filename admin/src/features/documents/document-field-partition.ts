import type { SchemaField } from "@riducms/protocol";

export interface DocumentFieldPartition {
	content: SchemaField[];
	sidebar: SchemaField[];
}

// Root layout metadata changes presentation only. The manifest remains in its
// canonical order, while each visual region preserves the order of its fields.
export function partitionDocumentFields(fields: readonly SchemaField[]): DocumentFieldPartition {
	const content: SchemaField[] = [];
	const sidebar: SchemaField[] = [];
	for (const field of fields) {
		if (field.admin.hidden === true) continue;
		(field.admin.sidebar === true ? sidebar : content).push(field);
	}
	return { content, sidebar };
}
