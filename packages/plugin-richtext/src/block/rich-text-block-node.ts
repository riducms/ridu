import { isRecord } from "@riducms/protocol";
import type { Decorator } from "@hvniel/lexical-svelte";
import {
	$applyNodeReplacement,
	$getDocument,
	$getNodeByKey,
	DecoratorNode,
	type LexicalNode,
	type NodeKey,
	type SerializedLexicalNode,
} from "lexical";
import RichTextBlock from "@plugin-richtext/block/rich-text-block.svelte";

/** Retains the received envelope verbatim so historical content can be exported safely. */
export type SerializedBlockNode = SerializedLexicalNode & Record<string, unknown>;

export class BlockNode extends DecoratorNode<Decorator<typeof RichTextBlock>> {
	__envelope: SerializedBlockNode;

	static getType(): string {
		return "block";
	}
	static clone(node: BlockNode): BlockNode {
		return new BlockNode(node.__envelope, node.__key);
	}
	static importJSON(serializedNode: SerializedBlockNode): BlockNode {
		return $applyNodeReplacement(new BlockNode(structuredClone(serializedNode)));
	}
	constructor(envelope: SerializedBlockNode, key?: NodeKey) {
		super(key);
		this.__envelope = envelope;
	}
	exportJSON(): SerializedBlockNode {
		return structuredClone(this.getLatest().__envelope);
	}
	createDOM(): HTMLElement {
		const element = $getDocument().createElement("div");
		element.className = "ridu-richtext-embedded";
		element.contentEditable = "false";
		return element;
	}
	updateDOM(): false {
		return false;
	}
	isInline(): false {
		return false;
	}
	isKeyboardSelectable(): true {
		return true;
	}
	getFields(): Record<string, unknown> {
		const fields = this.getLatest().__envelope.fields;
		return isRecord(fields) ? fields : {};
	}
	setFields(fields: Record<string, unknown>): this {
		const writable = this.getWritable();
		writable.__envelope = { type: "block", version: 1, fields: structuredClone(fields) };
		return writable;
	}
	decorate(): Decorator<typeof RichTextBlock> {
		return {
			component: RichTextBlock,
			props: {
				nodeKey: this.getKey(),
				fields: this.getFields(),
				validEnvelope:
					this.__envelope.type === "block" &&
					this.__envelope.version === 1 &&
					Object.keys(this.__envelope).every(
						(key) => key === "type" || key === "version" || key === "fields"
					) &&
					typeof this.getFields()._key === "string" &&
					String(this.getFields()._key).trim() !== "",
			},
		};
	}
}

export function createBlockNode(fields: Record<string, unknown>): BlockNode {
	return $applyNodeReplacement(
		new BlockNode({ type: "block", version: 1, fields: structuredClone(fields) })
	);
}
export function isBlockNode(node: LexicalNode | null | undefined): node is BlockNode {
	return node instanceof BlockNode;
}

export interface BlockNameUpdate {
	nodeKey: NodeKey;
	identity: string;
	value: string;
}

// @lexical-scope
export function updateBlockName(update: BlockNameUpdate): boolean {
	const node = $getNodeByKey(update.nodeKey);
	if (!isBlockNode(node) || node.getFields()._key !== update.identity) return false;
	node.setFields({ ...node.getFields(), blockName: update.value });
	return true;
}
