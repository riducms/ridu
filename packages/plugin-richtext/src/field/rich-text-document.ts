import { documentRecoveryIssue } from "@plugin-richtext/document-validation";
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
	return JSON.stringify({ root: withElementDefaults(value.root) });
}

function withElementDefaults(node: Record<string, unknown>): Record<string, unknown> {
	if (!Array.isArray(node.children)) return node;
	const children =
		node.type === "root" && node.children.length === 0
			? [{ type: "paragraph", children: [], direction: null, format: "", indent: 0 }]
			: node.children.map((child) => (isRecord(child) ? withElementDefaults(child) : child));

	return {
		...node,
		children,
		direction: node.direction ?? null,
		format: node.format ?? "",
		indent: node.indent ?? 0,
	};
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
