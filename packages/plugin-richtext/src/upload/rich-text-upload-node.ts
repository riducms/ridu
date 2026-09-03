import type { Decorator } from "@hvniel/lexical-svelte";
import { DecoratorBlockNode } from "@hvniel/lexical-svelte";
import {
	$applyNodeReplacement,
	$getDocument,
	type ElementFormatType,
	type LexicalNode,
	type LexicalUpdateJSON,
	type NodeKey,
	type SerializedLexicalNode,
	type Spread,
} from "lexical";

import RichTextUploadNodeComponent from "@plugin-richtext/upload/rich-text-upload-node.svelte";

export type SerializedUploadNode = Spread<
	{
		caption?: string;
		format: ElementFormatType;
		id: string;
		relationTo: string;
	},
	SerializedLexicalNode
>;

export class UploadNode extends DecoratorBlockNode {
	__caption: string;
	__relationTo: string;
	__documentID: string;

	static getType(): string {
		return "upload";
	}

	static clone(node: UploadNode): UploadNode {
		return new UploadNode(
			node.__relationTo,
			node.__documentID,
			node.__caption,
			node.__format,
			node.__key
		);
	}

	static importJSON(serializedNode: SerializedUploadNode): UploadNode {
		return createUploadNode({
			caption: serializedNode.caption ?? "",
			documentID: serializedNode.id,
			format: serializedNode.format,
			relationTo: serializedNode.relationTo,
		}).updateFromJSON(serializedNode);
	}

	constructor(
		relationTo: string,
		documentID: string,
		caption = "",
		format?: ElementFormatType,
		key?: NodeKey
	) {
		super(format, key);
		this.__relationTo = relationTo;
		this.__documentID = documentID;
		this.__caption = caption;
	}

	updateFromJSON(serializedNode: LexicalUpdateJSON<SerializedUploadNode>): this {
		const node = super.updateFromJSON(serializedNode);
		node.__relationTo = serializedNode.relationTo;
		node.__documentID = serializedNode.id;
		node.__caption = serializedNode.caption ?? "";
		return node;
	}

	exportJSON(): SerializedUploadNode {
		return {
			...super.exportJSON(),
			...(this.__caption === "" ? {} : { caption: this.__caption }),
			id: this.__documentID,
			relationTo: this.__relationTo,
		};
	}

	createDOM(): HTMLElement {
		const element = $getDocument().createElement("div");
		element.className = "ridu-richtext-upload";
		return element;
	}

	decorate(): Decorator<typeof RichTextUploadNodeComponent> {
		return {
			component: RichTextUploadNodeComponent,
			props: {
				caption: this.__caption,
				documentID: this.__documentID,
				nodeKey: this.getKey(),
				relationTo: this.__relationTo,
			},
		};
	}

	getDocumentID(): string {
		return this.getLatest().__documentID;
	}

	getCaption(): string {
		return this.getLatest().__caption;
	}

	getRelationTo(): string {
		return this.getLatest().__relationTo;
	}

	setReference(relationTo: string, documentID: string): this {
		const writable = this.getWritable();
		writable.__relationTo = relationTo;
		writable.__documentID = documentID;
		return writable;
	}

	setCaption(caption: string): this {
		const writable = this.getWritable();
		writable.__caption = caption;
		return writable;
	}
}

export function createUploadNode({
	caption = "",
	documentID,
	format,
	relationTo,
}: {
	caption?: string;
	documentID: string;
	format?: ElementFormatType;
	relationTo: string;
}): UploadNode {
	return $applyNodeReplacement(new UploadNode(relationTo, documentID, caption, format));
}

export function isUploadNode(node: LexicalNode | null | undefined): node is UploadNode {
	return node instanceof UploadNode;
}
