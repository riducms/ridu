import type { FieldAuthoringHost } from "@riducms/plugin";
import type { SchemaBlockType } from "@riducms/protocol";
import type { SerializedLexicalNode } from "lexical";
import { isRecord } from "@plugin-richtext/field/rich-text-blocks";
import { documentRecoveryIssue } from "@plugin-richtext/document-validation";

/** Validates first, then copies only declared payloads through the generic schema host. */
export function copyBlockClipboardNodes(
	nodes: unknown,
	types: readonly SchemaBlockType[],
	copy: NonNullable<FieldAuthoringHost["copySchemaPayload"]>
): { nodes: SerializedLexicalNode[]; hasBlocks: boolean } {
	if (!Array.isArray(nodes)) throw new Error("Clipboard content is not a rich-text node list.");
	const issue = documentRecoveryIssue({ version: 1, root: { type: "root", children: nodes } });
	if (issue !== undefined)
		throw new Error(
			`Clipboard content exceeds a rich-text contract or traversal limit at ${issue}.`
		);
	const result = structuredClone(nodes);
	const pending: { node: unknown; depth: number }[] = result.map((node) => ({ node, depth: 0 }));
	const blocks: Record<string, unknown>[] = [];
	let count = 0;
	while (pending.length > 0) {
		const { node, depth } = pending.pop()!;
		if (++count > 10000 || depth > 64)
			throw new Error("Clipboard content exceeds the rich-text traversal limit.");
		if (!isRecord(node) || typeof node.type !== "string" || typeof node.version !== "number")
			throw new Error("Clipboard contains a malformed rich-text node.");
		if (node.type === "block") {
			const fields = node.fields;
			if (
				node.version !== 1 ||
				!isRecord(fields) ||
				typeof fields.blockType !== "string" ||
				!types.some((type) => type.slug === fields.blockType)
			)
				throw new Error(
					"This field does not support a block type on the clipboard. Paste into a field that declares it, or migrate the content first."
				);
			blocks.push(node);
			continue;
		}
		if (Array.isArray(node.children))
			pending.push(...node.children.map((node) => ({ node, depth: depth + 1 })));
	}
	for (const node of blocks) {
		const fields = node.fields as Record<string, unknown> & { blockType: string };
		node.fields = copy(
			{ treeKey: "blocks", caseTag: "block", variantSlug: fields.blockType },
			fields
		);
	}
	return { nodes: result as SerializedLexicalNode[], hasBlocks: blocks.length > 0 };
}
