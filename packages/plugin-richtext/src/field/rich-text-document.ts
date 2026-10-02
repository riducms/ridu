import { isRecord } from "@riducms/protocol";
import { documentRecoveryIssue } from "@riducms/sdk/richtext";
import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";

export function editorRecoveryIssue(value: unknown, config: RichTextConfig) {
	if (value === undefined || value === null) return undefined;
	const types = new Set(["root", "paragraph", "heading", "quote", "text", "linebreak", "block"]);
	const featureTypes = {
		links: ["link"],
		lists: ["list", "listitem"],
		code: ["code"],
		"horizontal-rule": ["horizontalrule"],
		uploads: ["upload"],
		relationships: ["relationship"],
		blocks: ["block"],
	};
	for (const feature of config.features) for (const type of featureTypes[feature]) types.add(type);
	return documentRecoveryIssue(value, types);
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
