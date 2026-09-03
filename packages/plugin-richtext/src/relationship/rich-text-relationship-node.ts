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

import RichTextRelationshipNodeComponent from "@plugin-richtext/relationship/rich-text-relationship-node.svelte";

export type SerializedRelationshipNode = Spread<
	{
		format: ElementFormatType;
		id: string;
		relationTo: string;
	},
	SerializedLexicalNode
>;

export class RelationshipNode extends DecoratorBlockNode {
	__relationTo: string;
	__documentID: string;

	static getType(): string {
		return "relationship";
	}

	static clone(node: RelationshipNode): RelationshipNode {
		return new RelationshipNode(node.__relationTo, node.__documentID, node.__format, node.__key);
	}

	static importJSON(serializedNode: SerializedRelationshipNode): RelationshipNode {
		return createRelationshipNode({
			documentID: serializedNode.id,
			format: serializedNode.format,
			relationTo: serializedNode.relationTo,
		}).updateFromJSON(serializedNode);
	}

	constructor(relationTo: string, documentID: string, format?: ElementFormatType, key?: NodeKey) {
		super(format, key);
		this.__relationTo = relationTo;
		this.__documentID = documentID;
	}

	updateFromJSON(serializedNode: LexicalUpdateJSON<SerializedRelationshipNode>): this {
		const node = super.updateFromJSON(serializedNode);
		node.__relationTo = serializedNode.relationTo;
		node.__documentID = serializedNode.id;
		return node;
	}

	exportJSON(): SerializedRelationshipNode {
		return {
			...super.exportJSON(),
			id: this.__documentID,
			relationTo: this.__relationTo,
		};
	}

	createDOM(): HTMLElement {
		const element = $getDocument().createElement("div");
		element.className = "ridu-richtext-relationship";
		return element;
	}

	decorate(): Decorator<typeof RichTextRelationshipNodeComponent> {
		return {
			component: RichTextRelationshipNodeComponent,
			props: {
				documentID: this.__documentID,
				nodeKey: this.getKey(),
				relationTo: this.__relationTo,
			},
		};
	}

	setReference(relationTo: string, documentID: string): this {
		const writable = this.getWritable();
		writable.__relationTo = relationTo;
		writable.__documentID = documentID;
		return writable;
	}
}

export function createRelationshipNode({
	documentID,
	format,
	relationTo,
}: {
	documentID: string;
	format?: ElementFormatType;
	relationTo: string;
}): RelationshipNode {
	return $applyNodeReplacement(new RelationshipNode(relationTo, documentID, format));
}

export function isRelationshipNode(node: LexicalNode | null | undefined): node is RelationshipNode {
	return node instanceof RelationshipNode;
}
