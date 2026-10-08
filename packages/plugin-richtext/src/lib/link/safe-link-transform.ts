import { LinkNode } from "@lexical/link";
import type { LexicalEditor } from "lexical";

import { normalizeLinkURL } from "#lib/link/link-url.js";

/** Prevent imported HTML from persisting links outside Ridu's portable URL contract. */
export function registerSafeLinkTransform(editor: LexicalEditor): () => void {
	return editor.registerNodeTransform(LinkNode, (link) => {
		if (normalizeLinkURL(link.getURL()) !== undefined) return;

		const parent = link.getParent();
		if (parent === null) {
			link.remove();
			return;
		}
		const children = link.getChildren();
		parent.splice(link.getIndexWithinParent(), 0, children);
		link.remove();
	});
}
