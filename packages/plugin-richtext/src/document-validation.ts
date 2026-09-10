import type { RichTextDocument } from "#richtext/document";
const base = ["type", "version"];
const element = [...base, "children", "direction", "format", "indent"];
const container = [...element, "textFormat", "textStyle"];
const properties: Readonly<Record<string, readonly string[]>> = {
	root: element,
	paragraph: container,
	heading: [...container, "tag"],
	quote: container,
	link: [...container, "url", "target", "rel", "title"],
	list: [...container, "listType", "start", "tag"],
	listitem: [...container, "value", "checked"],
	code: [...container, "language", "theme"],
	text: [...base, "text", "format", "detail", "mode", "style"],
	linebreak: base,
	horizontalrule: base,
	upload: [...base, "relationTo", "id", "caption", "format"],
	relationship: [...base, "relationTo", "id", "format"],
	block: [...base, "fields"],
};
const integers = new Set(["detail", "textFormat", "indent", "start", "value"]);
const nullableStrings = new Set(["target", "rel", "title", "direction"]);

function isRecord(value: unknown): value is Record<string, unknown> {
	return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** Return the first unsupported envelope path without interpreting schema-owned payloads.
 * Both editor admission and portable rendering use this lossless JSON boundary.
 */
export function documentRecoveryIssue(
	value: unknown,
	allowedNodeTypes?: ReadonlySet<string>
): string | undefined {
	if (!isRecord(value)) return "document";
	if (value.version !== 1) return "version";
	const extra = Object.keys(value).find((key) => key !== "version" && key !== "root");
	if (extra !== undefined) return extra;
	let count = 0;
	const visit = (node: unknown, path: string, depth: number): string | undefined => {
		if (++count > 10_000 || depth > 64) return path;
		if (!isRecord(node)) return path;
		const type = node.type;
		if (typeof type !== "string" || !Object.hasOwn(properties, type)) return `${path}.type`;
		if ((depth === 0) !== (type === "root")) return `${path}.type`;
		if (allowedNodeTypes !== undefined && !allowedNodeTypes.has(type)) return `${path}.type`;
		if (("version" in node && node.version !== 1) || (type === "block" && node.version !== 1))
			return `${path}.version`;
		for (const [key, value] of Object.entries(node)) {
			if (!properties[type]!.includes(key)) return `${path}.${key}`;
			if (key === "type" || key === "version" || key === "children") continue;
			if (key === "fields") {
				if (!isRecord(value)) return `${path}.fields`;
			} else if (integers.has(key) || (key === "format" && type === "text")) {
				if (typeof value !== "number" || !Number.isSafeInteger(value)) return `${path}.${key}`;
			} else if (key === "checked") {
				if (typeof value !== "boolean") return `${path}.${key}`;
			} else if (!(nullableStrings.has(key) && value === null) && typeof value !== "string") {
				return `${path}.${key}`;
			}
		}
		if (type === "block") return isRecord(node.fields) ? undefined : `${path}.fields`;
		if (type === "text" && typeof node.text !== "string") return `${path}.text`;
		if ("mode" in node && !["normal", "token", "segmented"].includes(String(node.mode)))
			return `${path}.mode`;
		if (
			"direction" in node &&
			node.direction !== null &&
			node.direction !== "ltr" &&
			node.direction !== "rtl"
		)
			return `${path}.direction`;
		if (
			type !== "text" &&
			"format" in node &&
			!["", "left", "start", "center", "right", "end", "justify"].includes(String(node.format))
		)
			return `${path}.format`;
		if (type === "link" && typeof node.url !== "string") return `${path}.url`;
		if (type === "heading" && !/^h[1-6]$/.test(String(node.tag))) return `${path}.tag`;
		if (type === "list" && !["bullet", "number", "check"].includes(String(node.listType)))
			return `${path}.listType`;
		if (type === "upload" || type === "relationship") {
			if (typeof node.relationTo !== "string") return `${path}.relationTo`;
			if (typeof node.id !== "string") return `${path}.id`;
		}
		if (properties[type]!.includes("children")) {
			if (!Array.isArray(node.children)) return `${path}.children`;
			for (let index = 0; index < node.children.length; index++) {
				const issue = visit(node.children[index], `${path}.children.${index}`, depth + 1);
				if (issue !== undefined) return issue;
			}
		}
		return undefined;
	};
	return visit(value.root, "root", 0);
}

/** Decode the portable envelope; embedded payload semantics remain schema-owned. */
export function decodeRichTextDocument(value: unknown): RichTextDocument<unknown> {
	const issue = documentRecoveryIssue(value);
	if (issue !== undefined) throw new Error(`Invalid rich-text document at ${issue}.`);
	return value as RichTextDocument<unknown>;
}
