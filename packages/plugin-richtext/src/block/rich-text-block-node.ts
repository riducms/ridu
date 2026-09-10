import type { Decorator } from "@hvniel/lexical-svelte";
import {
	$applyNodeReplacement,
	$getDocument,
	DecoratorNode,
	type LexicalNode,
	type NodeKey,
	type SerializedLexicalNode,
} from "lexical";
import RichTextBlockCard from "@plugin-richtext/block/rich-text-block-card.svelte";
import { isRecord } from "@plugin-richtext/field/rich-text-blocks";

/** Retains the received envelope verbatim so historical content can be exported safely. */
export type SerializedBlockNode = SerializedLexicalNode & Record<string, unknown>;

export class BlockNode extends DecoratorNode<Decorator<typeof RichTextBlockCard>> {
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
	decorate(): Decorator<typeof RichTextBlockCard> {
		return {
			component: RichTextBlockCard,
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
