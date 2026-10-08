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
