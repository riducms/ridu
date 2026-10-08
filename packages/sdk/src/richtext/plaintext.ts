import type { RichTextBlockRenderers, RichTextDocument, RichTextNode } from "./document.js";
import { documentRecoveryIssue } from "./document-validation.js";
import { richTextBlockHandler } from "./render.js";

/** Text for the nodes `convertLexicalToPlaintext` can't read text from, such as custom blocks. */
export interface RichTextPlaintextOptions<Payload extends { blockType: string }> {
	/** Text for custom blocks, which have none of their own. Blocks without a converter are left out. */
	blocks?: Partial<RichTextBlockRenderers<Payload, string>>;
	/** Text for uploads and relationships, such as a caption. Without a converter they're left out. */
	nodes?: Partial<
		Record<
			"upload" | "relationship",
			(node: Extract<RichTextNode<Payload>, { type: "upload" | "relationship" }>) => string
		>
	>;
}

/**
 * A document's text without formatting, for previews, search, notifications or a language model.
 * Blocks are separated by a blank line, list items are on their own lines, and a line break is a
 * newline. Pure and synchronous, like `renderRichTextHTML`.
 *
 * @param value A stored rich-text document.
 * @param options Text for custom blocks, uploads and relationships, which are left out without it.
 * @returns The document's text.
 * @throws If the value isn't a rich-text document.
 * @example
 * ```ts
 * import { convertLexicalToPlaintext } from "@riducms/sdk/richtext";
 *
 * const preview = convertLexicalToPlaintext(post.body, {
 *   blocks: { callout: (block) => block.title ?? "" }
 * });
 * ```
 */
export function convertLexicalToPlaintext<Payload extends { blockType: string } = never>(
	value: RichTextDocument<Payload>,
	options: RichTextPlaintextOptions<Payload> = {}
): string {
	const issue = documentRecoveryIssue(value);
	if (issue !== undefined)
		throw new Error(`Unsupported rich-text document envelope or budget at ${issue}`);
	// The validation above bounds the document's depth and size.
	const convert = (node: RichTextNode<Payload>): string => {
		switch (node.type) {
			case "block":
				return richTextBlockHandler(options.blocks, node.fields)?.(node.fields) ?? "";
			case "text":
				return node.text;
			case "linebreak":
				return "\n";
			case "horizontalrule":
				return "";
			case "upload":
			case "relationship":
				return options.nodes?.[node.type]?.(node) ?? "";
			case "list":
				return lines(node.children.map(convert), "\n");
			case "paragraph":
			case "heading":
			case "quote":
			case "link":
			case "listitem":
			case "code":
				return node.children.map(convert).join("");
		}
	};
	return lines(value.root.children.map(convert), "\n\n");
}

/** Joins the parts that have text, so an empty paragraph or a divider adds no blank lines. */
function lines(parts: string[], separator: string) {
	return parts.filter((part) => part.trim() !== "").join(separator);
}
