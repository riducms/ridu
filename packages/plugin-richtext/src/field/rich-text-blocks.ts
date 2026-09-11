import { resolveBlockTypes } from "@riducms/protocol";
import type { SchemaBlockType, SchemaField } from "@riducms/protocol";

/** The allowlist comes from resolved executable schemas, never plugin settings. */
export function richTextBlockTypes(field: SchemaField): readonly SchemaBlockType[] {
	return (
		resolveBlockTypes(
			field.plugin?.embeddedTrees
				?.find((tree) => tree.key === "blocks")
				?.cases.find((entry) => entry.tagValue === "block")
		) ?? []
	);
}

export function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function blockSummary(fields: Record<string, unknown>, type?: SchemaBlockType): string {
	// Named cards render their guarded header instead. Never mirror an editorial name
	// into plugin-owned text, because the configured field may be unreadable.
	if (type?.admin?.nameField !== undefined) return "";
	const summary = type?.admin?.rowLabel;
	if (summary !== undefined) {
		const value = summary
			.split(".")
			.reduce<unknown>((value, key) => (isRecord(value) ? value[key] : undefined), fields);
		if (typeof value === "string" && value.trim() !== "") return value;
	}
	for (const field of type?.fields ?? []) {
		const value = fields[field.name];
		if (
			(field.type === "text" || field.type === "textarea") &&
			typeof value === "string" &&
			value.trim() !== ""
		)
			return value;
	}
	return "";
}
