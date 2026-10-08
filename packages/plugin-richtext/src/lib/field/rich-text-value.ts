import { isRecord } from "@riducms/protocol";
import { documentRecoveryIssue, type RichTextDocument } from "@riducms/sdk/richtext";
import type { EditorState } from "lexical";

/**
 * An editor state as the stored document, before validation. The JSON round trip drops undefined
 * optional properties, which Lexical keeps after importing sparse JSON.
 */
export function lexicalDocument(editorState: EditorState): unknown {
	const root: unknown = JSON.parse(JSON.stringify(editorState.toJSON().root));
	return { version: 1, root: portableNode(root) };
}

// Lexical remembers the Markdown that made a node, such as a `*` list marker, as node state under
// `$`, and writes a tab as a `tab` node. The document keeps neither: the state goes, and a tab is
// text. Only nodes are walked, so block payloads keep their own keys.
function portableNode(node: unknown): unknown {
	if (!isRecord(node)) return node;
	const portable = Object.fromEntries(Object.entries(node).filter(([key]) => key !== "$"));
	if (portable.type === "tab") portable.type = "text";
	if (Array.isArray(portable.children)) portable.children = portable.children.map(portableNode);
	return portable;
}

/** Decode a stored field value; embedded payload semantics remain schema-owned. */
export function decodeRichTextDocument(value: unknown): RichTextDocument<unknown> {
	const issue = documentRecoveryIssue(value);
	if (issue !== undefined) throw new Error(`Invalid rich-text document at ${issue}.`);
	return value as RichTextDocument<unknown>;
}

/** Compare JSON field data without treating server object-key ordering as an edit. */
export function equalRichTextValues(left: unknown, right: unknown): boolean {
	if (Object.is(left, right)) return true;
	if (left === null || right === null || typeof left !== "object" || typeof right !== "object")
		return false;
	if (Array.isArray(left))
		return (
			Array.isArray(right) &&
			left.length === right.length &&
			left.every((value, index) => equalRichTextValues(value, right[index]))
		);
	if (Array.isArray(right)) return false;

	const before = left as Record<string, unknown>;
	const after = right as Record<string, unknown>;
	// Undefined optional properties are absent from the serialized wire value.
	const keys = Object.keys(before).filter((key) => before[key] !== undefined);
	return (
		keys.length === Object.keys(after).filter((key) => after[key] !== undefined).length &&
		keys.every((key) => Object.hasOwn(after, key) && equalRichTextValues(before[key], after[key]))
	);
}
