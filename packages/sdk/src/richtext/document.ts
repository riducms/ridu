/** Portable JSON contracts. This entry point imports no editor, Svelte or DOM code. */
export interface RichTextDocument<Payload = never> {
	version: 1;
	root: RichTextRootNode<Payload>;
}

/** Payload is instantiated with generated input variants (required values and canonical IDs). */
export type RichTextDocumentInput<Payload = never> = RichTextDocument<Payload>;

export type RichTextElementFormat = "" | "left" | "center" | "right" | "justify" | "start" | "end";

export interface RichTextRootNode<Payload = never> {
	type: "root";
	version?: 1;
	children: RichTextNode<Payload>[];
	direction?: "ltr" | "rtl" | null;
	format?: RichTextElementFormat;
	indent?: number;
}

export type RichTextBlockNode<Payload> = [Payload] extends [never]
	? never
	: { type: "block"; version: 1; fields: Payload };

export interface RichTextTextNode {
	type: "text";
	version?: 1;
	text: string;
	format?: number;
	detail?: number;
	mode?: "normal" | "token" | "segmented";
	style?: string;
}

export interface RichTextElementNode<Payload = never> {
	type: "paragraph" | "heading" | "quote" | "link" | "list" | "listitem" | "code";
	version?: 1;
	children: RichTextNode<Payload>[];
	direction?: "ltr" | "rtl" | null;
	format?: RichTextElementFormat;
	indent?: number;
	tag?: "h1" | "h2" | "h3" | "h4" | "h5" | "h6" | "ol" | "ul";
	url?: string;
	target?: string | null;
	rel?: string | null;
	title?: string | null;
	listType?: "bullet" | "number" | "check";
	start?: number;
	value?: number;
	checked?: boolean;
	language?: string;
	theme?: string;
	textFormat?: number;
	textStyle?: string;
}

export interface RichTextReferenceNode {
	type: "upload" | "relationship";
	version?: 1;
	relationTo: string;
	id: string;
	caption?: string;
	format?: RichTextElementFormat;
}

export type RichTextNode<Payload = never> =
	| RichTextTextNode
	| RichTextElementNode<Payload>
	| RichTextReferenceNode
	| { type: "linebreak" | "horizontalrule"; version?: 1 }
	| RichTextBlockNode<Payload>;

/** Checked renderer dispatch retains the selected variant's exact payload. */
export type RichTextBlockRenderers<Payload extends { blockType: string }, Result> = {
	[Slug in Payload["blockType"]]: (block: Extract<Payload, { blockType: Slug }>) => Result;
};
