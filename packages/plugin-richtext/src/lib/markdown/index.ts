import { $isCodeNode, CodeNode } from "@lexical/code-core";
import {
	$createHorizontalRuleNode,
	$isHorizontalRuleNode,
	HorizontalRuleNode,
} from "@lexical/extension";
import { LinkNode } from "@lexical/link";
import { ListItemNode, ListNode } from "@lexical/list";
import { $convertFromMarkdownString, type ElementTransformer } from "@lexical/markdown";
import { HeadingNode, QuoteNode } from "@lexical/rich-text";
import { documentRecoveryIssue, type RichTextDocument } from "@riducms/sdk/richtext";
import { $generateNodesFromRawText, $getRoot, createEditor } from "lexical";

import { richTextEditorFeatures, type RichTextEditorFeature } from "#lib/field/rich-text-config.js";
import { isBlocklessDocument } from "#lib/field/rich-text-document.js";
import { richTextMarkdownTransformers } from "#lib/field/rich-text-markdown.js";
import { lexicalDocument } from "#lib/field/rich-text-value.js";

// The editor's own divider shortcut, on the DOM-free node its divider extends.
const horizontalRule: ElementTransformer = {
	dependencies: [HorizontalRuleNode],
	export: (node) => ($isHorizontalRuleNode(node) ? "***" : null),
	regExp: /^(---|\*\*\*|___)\s?$/,
	replace: (parent) => {
		parent.replace($createHorizontalRuleNode());
	},
	type: "element",
};

/** How `convertMarkdownToLexical` converts: which of the editor's features it may use. */
export interface RichTextMarkdownOptions {
	/** The Go field's features. Markdown for anything else stays text, as in the editor. */
	features?: readonly RichTextEditorFeature[] | undefined;
}

/**
 * A document from Markdown, such as a language model's answer or imported content, made the way
 * the editor's Markdown shortcuts make it. It runs without a browser, so a server action or script
 * can save it, and the editor can open it.
 *
 * @param markdown The Markdown. Markdown for a feature the field doesn't have stays text.
 * @param options The Go field's features, which default to all of the editor's.
 * @returns A document the field stores, with one empty paragraph for empty Markdown.
 * @example
 * ```ts
 * import { convertMarkdownToLexical } from "@riducms/plugin-richtext/markdown";
 *
 * const body = convertMarkdownToLexical(answer, { features: ["links", "lists"] });
 * await ridu.update("posts", id, { body });
 * ```
 */
export function convertMarkdownToLexical(
	markdown: string,
	options: RichTextMarkdownOptions = {}
): RichTextDocument {
	const features = options.features ?? richTextEditorFeatures;
	const editor = createEditor({
		namespace: "@riducms/plugin-richtext/markdown",
		nodes: [
			HeadingNode,
			QuoteNode,
			...(features.includes("lists") ? [ListNode, ListItemNode] : []),
			...(features.includes("links") ? [LinkNode] : []),
			...(features.includes("code") ? [CodeNode] : []),
			...(features.includes("horizontal-rule") ? [HorizontalRuleNode] : []),
		],
		onError: (error) => {
			throw error;
		},
	});
	editor.update(
		() => {
			$convertFromMarkdownString(markdown, [
				...richTextMarkdownTransformers(features),
				...(features.includes("horizontal-rule") ? [horizontalRule] : []),
			]);
			// Markdown makes a code block's lines one text node, where the editor writes line breaks.
			for (const node of $getRoot().getChildren()) {
				if (!$isCodeNode(node)) continue;
				const lines = $generateNodesFromRawText(node.getTextContent());
				node.clear().append(...lines);
			}
		},
		{ discrete: true }
	);
	const document = lexicalDocument(editor.getEditorState());
	if (!isBlocklessDocument(document))
		throw new Error(
			`Markdown produced a document the rich-text field can't store, at ${documentRecoveryIssue(document) ?? "a node it doesn't open"}.`
		);
	return document;
}
