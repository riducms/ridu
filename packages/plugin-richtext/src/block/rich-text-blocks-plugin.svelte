<script lang="ts">
	import { isRecord } from "@riducms/protocol";
	import { type FieldAuthoringHost } from "@riducms/plugin";
	import type { SchemaField } from "@riducms/protocol";
	import { useLexicalComposerContext } from "@hvniel/lexical-svelte";
	import {
		$generateNodesFromSerializedNodes,
		$getClipboardDataFromSelection,
		setLexicalClipboardDataTransfer,
		$insertGeneratedNodes,
	} from "@lexical/clipboard";
	import { copyBlockClipboardNodes } from "@plugin-richtext/block/rich-text-block-clipboard";
	import { $insertNodeToNearestRoot, mergeRegister } from "@lexical/utils";
	import {
		$addUpdateTag,
		$createParagraphNode,
		$getNodeByKey,
		$getRoot,
		$getSelection,
		$isElementNode,
		$isNodeSelection,
		$isParagraphNode,
		$isRangeSelection,
		COMMAND_PRIORITY_HIGH,
		COMMAND_PRIORITY_EDITOR,
		HISTORY_PUSH_TAG,
		COPY_COMMAND,
		CUT_COMMAND,
		KEY_BACKSPACE_COMMAND,
		KEY_DELETE_COMMAND,
		PASTE_COMMAND,
		SKIP_DOM_SELECTION_TAG,
		type NodeKey,
	} from "lexical";
	import {
		createBlockNode,
		isBlockNode,
		updateBlockName,
	} from "@plugin-richtext/block/rich-text-block-node";
	import { richTextBlockTypes } from "@plugin-richtext/field/rich-text-blocks";
	import { BLOCK_FIELD_CHANGE_TAG } from "@plugin-richtext/block/rich-text-block-history";
	import {
		INSERT_BLOCK_COMMAND,
		REMOVE_BLOCK_COMMAND,
		MOVE_BLOCK_COMMAND,
		UPDATE_BLOCK_NAME_COMMAND,
	} from "@plugin-richtext/menu/rich-text-commands";
	import "@plugin-richtext/block/rich-text-block.scss";

	let { authoring, field }: { authoring: FieldAuthoringHost | undefined; field: SchemaField } =
		$props();
	const editor = useLexicalComposerContext()[0];
	let message = $state("");

	// @lexical-scope
	function $removeBlock(nodeKey: NodeKey) {
		if (!editor.isEditable()) return false;
		const node = $getNodeByKey(nodeKey);
		if (!isBlockNode(node)) return false;
		$addUpdateTag(HISTORY_PUSH_TAG);
		const next = node.getNextSibling();
		const previous = node.getPreviousSibling();
		node.remove();
		if ($isElementNode(next)) next.selectStart();
		else if ($isElementNode(previous)) previous.selectEnd();
		else {
			const paragraph = $createParagraphNode();
			if (next !== null) next.insertBefore(paragraph);
			else if (previous !== null) previous.insertAfter(paragraph);
			else $getRoot().append(paragraph);
			paragraph.select();
		}
		return true;
	}

	$effect(() => {
		return mergeRegister(
			...[COPY_COMMAND, CUT_COMMAND].map((command) =>
				editor.registerCommand(
					command,
					(event) => {
						if (command === CUT_COMMAND && !editor.isEditable()) return false;
						const selection = $getSelection();
						if (
							!(event instanceof ClipboardEvent) ||
							event.clipboardData === null ||
							!$isNodeSelection(selection) ||
							!selection.getNodes().some(isBlockNode)
						)
							return false;
						event.preventDefault();
						setLexicalClipboardDataTransfer(
							event.clipboardData,
							$getClipboardDataFromSelection(selection)
						);
						if (command === CUT_COMMAND) {
							for (const node of selection.getNodes()) {
								if (isBlockNode(node)) $removeBlock(node.getKey());
								else node.remove();
							}
						}
						return true;
					},
					COMMAND_PRIORITY_HIGH
				)
			),

			editor.registerCommand(
				PASTE_COMMAND,
				(event) => {
					if (
						!editor.isEditable() ||
						!(event instanceof ClipboardEvent) ||
						authoring?.copySchemaPayload === undefined
					)
						return false;
					const serialized = event.clipboardData?.getData("application/x-lexical-editor");
					if (!serialized) return false;
					try {
						if (serialized.length > 5 * 1024 * 1024)
							throw new Error("Clipboard content exceeds the rich-text size limit.");
						const parsed: unknown = JSON.parse(serialized);
						if (!isRecord(parsed)) return false;
						const copied = copyBlockClipboardNodes(
							parsed.nodes,
							richTextBlockTypes(field),
							authoring.copySchemaPayload
						);
						if (!copied.hasBlocks) return false;
						const selection = $getSelection();
						if (selection === null) return false;
						const nodes = $generateNodesFromSerializedNodes(copied.nodes);
						event.preventDefault();
						$addUpdateTag(HISTORY_PUSH_TAG);
						$insertGeneratedNodes(editor, nodes, selection);
						message = "";
						return true;
					} catch (error) {
						event.preventDefault();
						message =
							error instanceof Error
								? error.message
								: "This clipboard content could not be inserted safely.";
						return true;
					}
				},
				COMMAND_PRIORITY_HIGH
			),

			editor.registerCommand(
				INSERT_BLOCK_COMMAND,
				({ blockType, position }) => {
					if (authoring?.createSchemaPayload === undefined || !editor.isEditable()) return false;
					const type = richTextBlockTypes(field).find((type) => type.slug === blockType);
					if (type === undefined) return false;
					const target = position === undefined ? undefined : $getNodeByKey(position.targetNodeKey);
					if (position !== undefined && target == null) return false;
					const payload = authoring.createSchemaPayload({
						treeKey: "blocks",
						caseTag: "block",
						variantSlug: type.slug,
					});
					$addUpdateTag(HISTORY_PUSH_TAG);
					const node = createBlockNode(payload);
					if (target != null && position !== undefined) {
						if (position.insertBefore) target.insertBefore(node);
						else target.insertAfter(node);
					} else {
						const selection = $getSelection();
						if (!$isRangeSelection(selection)) $getRoot().selectEnd();
						const anchor = $isRangeSelection(selection) ? selection.anchor.getNode() : undefined;
						$insertNodeToNearestRoot(node);
						if ($isParagraphNode(anchor) && anchor.isEmpty()) anchor.remove();
						const paragraph = $createParagraphNode();
						node.insertAfter(paragraph);
						paragraph.select();
					}
					message = "";
					return true;
				},
				COMMAND_PRIORITY_EDITOR
			),
			editor.registerCommand(REMOVE_BLOCK_COMMAND, $removeBlock, COMMAND_PRIORITY_EDITOR),
			editor.registerCommand(
				MOVE_BLOCK_COMMAND,
				({ nodeKey, direction }) => {
					if (!editor.isEditable()) return false;
					const node = $getNodeByKey(nodeKey);
					if (!isBlockNode(node)) return false;
					const sibling = direction < 0 ? node.getPreviousSibling() : node.getNextSibling();
					if (sibling === null) return true;
					$addUpdateTag(HISTORY_PUSH_TAG);
					if (direction < 0) sibling.insertBefore(node);
					else sibling.insertAfter(node);
					queueMicrotask(() =>
						editor
							.getElementByKey(nodeKey)
							?.querySelector<HTMLElement>("[data-block-select]")
							?.focus()
					);
					return true;
				},
				COMMAND_PRIORITY_EDITOR
			),
			editor.registerCommand(
				UPDATE_BLOCK_NAME_COMMAND,
				(update) => {
					if (!editor.isEditable() || !updateBlockName(update)) return false;
					$addUpdateTag(update.historyTag);
					$addUpdateTag(BLOCK_FIELD_CHANGE_TAG);
					$addUpdateTag(SKIP_DOM_SELECTION_TAG);
					return true;
				},
				COMMAND_PRIORITY_EDITOR
			),
			...[KEY_BACKSPACE_COMMAND, KEY_DELETE_COMMAND].map((command) =>
				editor.registerCommand(
					command,
					(event) => {
						if (!editor.isEditable()) return false;
						const selection = $getSelection();
						if (!$isNodeSelection(selection)) return false;
						const nodes = selection.getNodes().filter(isBlockNode);
						if (nodes.length === 0) return false;
						event.preventDefault();
						for (const node of nodes) $removeBlock(node.getKey());
						return true;
					},
					COMMAND_PRIORITY_HIGH
				)
			)
		);
	});
</script>

{#if message !== ""}
	<p role="alert" class="ridu-richtext-block-message">{message}</p>
{/if}
