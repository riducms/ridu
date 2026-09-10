import type {
	RichTextBlockRenderers,
	RichTextDocument,
	RichTextElementNode,
	RichTextNode,
	RichTextTextNode,
} from "#richtext/document";
import { documentRecoveryIssue } from "#richtext/document-validation";

export type { RichTextBlockRenderers } from "#richtext/document";
export { documentRecoveryIssue } from "#richtext/document-validation";

export interface RichTextRenderOptions<Payload extends { blockType: string }> {
	/** Application renderers return trusted HTML. Ordinary text is always escaped. */
	blocks?: RichTextBlockRenderers<Payload, string>;
	nodes?: Partial<
		Record<
			"upload" | "relationship",
			(node: Extract<RichTextNode<Payload>, { type: "upload" | "relationship" }>) => string
		>
	>;
	/** Unknown nodes/variants fail by default; a fallback must return visible application-owned output. */
	fallback?: (node: unknown, error: Error) => string;
}

export function escapeRichText(value: string): string {
	return value.replace(
		/[&<>"']/g,
		(character) =>
			({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[character] ??
			character
	);
}

export function safeRichTextURL(value: string): string {
	const url = value.trim();
	if (
		/[\u0000-\u001f\u007f]/u.test(value) ||
		url.startsWith("//") ||
		(/^[a-z][a-z\d+.-]*:/iu.test(url) && !/^(https?|mailto|tel):/iu.test(url))
	) {
		throw new Error("Unsafe rich-text link URL");
	}
	return url;
}

export function renderRichTextText(node: RichTextTextNode): string {
	let text = escapeRichText(node.text);
	for (const [mask, tag] of [
		[64, "sup"],
		[32, "sub"],
		[16, "code"],
		[8, "u"],
		[4, "s"],
		[2, "em"],
		[1, "strong"],
	] as const) {
		if (((node.format ?? 0) & mask) !== 0) text = `<${tag}>${text}</${tag}>`;
	}
	return text;
}

export function richTextElementTag(node: RichTextElementNode<unknown>): string {
	switch (node.type) {
		case "paragraph":
			return "p";
		case "heading":
			return /^h[1-6]$/u.test(node.tag ?? "") ? (node.tag ?? "h2") : "h2";
		case "quote":
			return "blockquote";
		case "link":
			return "a";
		case "list":
			return node.listType === "number" ? "ol" : "ul";
		case "listitem":
			return "li";
		case "code":
			return "pre";
	}
}

/** Pure synchronous rendering: no editor bundle, DOM, Svelte dependency or network requests. */
export function renderRichTextHTML<Payload extends { blockType: string } = never>(
	value: RichTextDocument<Payload>,
	options: RichTextRenderOptions<Payload> = {}
): string {
	const issue = documentRecoveryIssue(value);
	if (issue !== undefined) {
		const error = new Error(`Unsupported rich-text document envelope or budget at ${issue}`);
		if (options.fallback !== undefined) return options.fallback(value, error);
		throw error;
	}
	let count = 0;
	const render = (node: RichTextNode<Payload>, depth: number): string => {
		count += 1;
		if (depth > 64 || count > 10_000)
			throw new Error("Rich-text rendering exceeds the depth or node budget");
		try {
			switch (node.type) {
				case "block": {
					const payload = node.fields;
					const renderer = (
						options.blocks !== undefined && Object.hasOwn(options.blocks, payload.blockType)
							? options.blocks[payload.blockType as Payload["blockType"]]
							: undefined
					) as ((block: Payload) => string) | undefined;
					if (renderer === undefined)
						throw new Error(
							`No renderer registered for rich-text block ${JSON.stringify(payload.blockType)}`
						);
					return renderer(payload);
				}
				case "text":
					return renderRichTextText(node);
				case "linebreak":
					return "<br>";
				case "horizontalrule":
					return "<hr>";
				case "upload":
				case "relationship": {
					const renderer = options.nodes?.[node.type];
					if (renderer === undefined)
						throw new Error(
							`No renderer registered for rich-text node ${JSON.stringify(node.type)}`
						);
					return renderer(node);
				}
				case "paragraph":
				case "heading":
				case "quote":
				case "link":
				case "list":
				case "listitem":
				case "code": {
					const tag = richTextElementTag(node);
					const children = node.children.map((child) => render(child, depth + 1)).join("");
					let attributes =
						node.type === "link"
							? ` href="${escapeRichText(safeRichTextURL(node.url ?? ""))}"`
							: "";
					if (["center", "right", "justify", "start", "end"].includes(node.format ?? ""))
						attributes += ` data-align="${node.format}"`;
					if ((node.indent ?? 0) > 0)
						attributes += ` data-indent="${Math.min(8, Math.floor(node.indent ?? 0))}"`;
					if (node.type === "list" && node.listType === "check")
						attributes += ' data-list-type="check"';
					if (node.type === "listitem" && node.checked !== undefined)
						attributes += ` data-checked="${node.checked}"`;
					return node.type === "code"
						? `<pre${attributes}><code>${children}</code></pre>`
						: `<${tag}${attributes}>${children}</${tag}>`;
				}
				default:
					throw new Error(
						`Unsupported rich-text node ${JSON.stringify((node as { type: string }).type)}`
					);
			}
		} catch (error) {
			if (options.fallback === undefined) throw error;
			return options.fallback(node, error instanceof Error ? error : new Error(String(error)));
		}
	};
	return value.root.children.map((node) => render(node, 0)).join("");
}
