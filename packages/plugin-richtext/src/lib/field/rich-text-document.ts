import { isRecord } from "@riducms/protocol";
import { documentRecoveryIssue, type RichTextDocument } from "@riducms/sdk/richtext";
import type { RichTextEditorExtension } from "#lib/editor/rich-text-extension.js";
import type { RichTextEditorFeature } from "#lib/field/rich-text-config.js";

const editorNodeTypes = ["root", "paragraph", "heading", "quote", "text", "linebreak"];
const featureNodeTypes: Record<RichTextEditorFeature, readonly string[]> = {
	links: ["link"],
	lists: ["list", "listitem"],
	code: ["code"],
	"horizontal-rule": ["horizontalrule"],
};

/**
 * Where a stored value has a node the editor can't open, or undefined when it can (or when the
 * value is empty). The editor opens its own nodes, its features' and its extensions'.
 */
export function editorRecoveryIssue(
	value: unknown,
	features: readonly RichTextEditorFeature[],
	extensions: readonly RichTextEditorExtension[]
) {
	if (value === undefined || value === null) return undefined;
	const types = new Set([
		...editorNodeTypes,
		...features.flatMap((feature) => featureNodeTypes[feature]),
		...extensions.flatMap((extension) => extension.nodes ?? []).map((node) => node.getType()),
	]);
	return documentRecoveryIssue(value, types);
}

/**
 * Whether a stored value is a document with no blocks, which the renderer shows without block
 * components. Uploads and relationships render as nothing until the editor mounts.
 */
export function isBlocklessDocument(value: unknown): value is RichTextDocument {
	const types = [
		...editorNodeTypes,
		...Object.values(featureNodeTypes).flat(),
		"upload",
		"relationship",
	];
	return documentRecoveryIssue(value, new Set(types)) === undefined;
}

/** Whether a document has nothing to show, so the editor shows its placeholder instead. */
export function isEmptyDocument(document: RichTextDocument) {
	const [first, ...rest] = document.root.children;
	return (
		first === undefined ||
		(rest.length === 0 && first.type === "paragraph" && first.children.length === 0)
	);
}

export function initialEditorState(value: unknown) {
	if (!isRecord(value) || !isRecord(value.root) || documentRecoveryIssue(value) !== undefined)
		return null;
	return JSON.stringify({ root: withLexicalDefaults(value.root) });
}

// Lexical imports absent serialized properties as undefined: a list without `start` numbers
// its items NaN, and a text node without `mode` has none. Supply Lexical's own defaults so
// every document the server accepts exports as one it accepts again. Stored values are not
// mutated.
function withLexicalDefaults(node: Record<string, unknown>): Record<string, unknown> {
	if (node.type === "text")
		return {
			...node,
			format: node.format ?? 0,
			detail: node.detail ?? 0,
			mode: node.mode ?? "normal",
			style: node.style ?? "",
		};
	if (!Array.isArray(node.children)) return node;
	const children = (node.type === "root" ? rootChildren(node.children) : node.children).map(
		(child) => (isRecord(child) ? withLexicalDefaults(child) : child)
	);
	const element = {
		...node,
		children,
		direction: node.direction ?? null,
		format: node.format ?? "",
		indent: node.indent ?? 0,
	};
	if (node.type !== "list") return element;
	return { ...element, start: typeof node.start === "number" ? node.start : 1 };
}

// Lexical's root accepts only element and decorator nodes, and an empty root has no caret.
// Wrap each run of root-level text and line breaks in a paragraph.
function rootChildren(children: readonly unknown[]): unknown[] {
	if (children.length === 0) return [{ type: "paragraph", children: [] }];
	const result: unknown[] = [];
	let run: unknown[] | undefined;
	for (const child of children) {
		if (isRecord(child) && (child.type === "text" || child.type === "linebreak")) {
			if (run === undefined) result.push({ type: "paragraph", children: (run = []) });
			run.push(child);
		} else {
			run = undefined;
			result.push(child);
		}
	}
	return result;
}
