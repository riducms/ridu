import {
	BOLD_ITALIC_STAR,
	BOLD_ITALIC_UNDERSCORE,
	BOLD_STAR,
	BOLD_UNDERSCORE,
	CHECK_LIST,
	CODE,
	HEADING,
	INLINE_CODE,
	ITALIC_STAR,
	ITALIC_UNDERSCORE,
	LINK,
	ORDERED_LIST,
	QUOTE,
	STRIKETHROUGH,
	UNORDERED_LIST,
	type TextMatchTransformer,
	type Transformer,
} from "@lexical/markdown";
import { $isLinkNode } from "@lexical/link";

import type { RichTextEditorFeature } from "#lib/field/rich-text-config.js";
import { normalizeLinkURL } from "#lib/link/link-url.js";

const safeLink: TextMatchTransformer = {
	...LINK,
	replace(textNode, match) {
		// Match Lexical's CommonMark unescaping before validation. Pass the original
		// match to its transformer so escaped text/title are still decoded only once.
		const decoded = (match[2] ?? "")
			.replace(/\\([!-/:-@[-`{-~])/g, "$1")
			.replace(/&#(\d+);/g, (_, codePoint: string) => {
				const value = Number(codePoint);
				return value <= 0x10ffff ? String.fromCodePoint(value) : "\0";
			});
		const url = normalizeLinkURL(decoded);
		if (url === undefined) return;

		const replacement = LINK.replace?.(textNode, match);
		const link = replacement?.getParent();
		if ($isLinkNode(link)) link.setURL(url);
		return replacement;
	},
};

/** Shortcuts produce only nodes enabled by this field's canonical configuration. */
export function richTextMarkdownTransformers(
	features: readonly RichTextEditorFeature[]
): Transformer[] {
	return [
		HEADING,
		QUOTE,
		...(features.includes("lists") ? [CHECK_LIST, UNORDERED_LIST, ORDERED_LIST] : []),
		...(features.includes("code") ? [CODE, INLINE_CODE] : []),
		BOLD_ITALIC_STAR,
		BOLD_ITALIC_UNDERSCORE,
		BOLD_STAR,
		BOLD_UNDERSCORE,
		ITALIC_STAR,
		ITALIC_UNDERSCORE,
		STRIKETHROUGH,
		...(features.includes("links") ? [safeLink] : []),
	];
}
