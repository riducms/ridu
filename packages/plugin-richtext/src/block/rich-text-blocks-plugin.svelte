<script lang="ts">
	import { getAdminI18n, type EmbeddedSchemaDraft, type FieldAuthoringHost } from "@riducms/plugin";
	import type { SchemaField } from "@riducms/protocol";
	import { useLexicalComposerContext } from "@hvniel/lexical-svelte";
	import {
		$generateNodesFromSerializedNodes,
		$getClipboardDataFromSelection,
		setLexicalClipboardDataTransfer,
		$insertGeneratedNodes,
	} from "@lexical/clipboard";
	import { copyBlockClipboardNodes } from "@plugin-richtext/block/rich-text-block-clipboard";
	import { isRecord } from "@plugin-richtext/field/rich-text-blocks";
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
		$setSelection,
		COMMAND_PRIORITY_HIGH,
		COMMAND_PRIORITY_EDITOR,
		HISTORY_PUSH_TAG,
		COPY_COMMAND,
		CUT_COMMAND,
		KEY_BACKSPACE_COMMAND,
		KEY_DELETE_COMMAND,
		PASTE_COMMAND,
		type BaseSelection,
		type NodeKey,
	} from "lexical";
	import {
		createBlockNode,
		isBlockNode,
		updateBlockName,
	} from "@plugin-richtext/block/rich-text-block-node";
	import { richTextBlockTypes } from "@plugin-richtext/field/rich-text-blocks";
	import {
		OPEN_BLOCK_EDITOR_COMMAND,
		DUPLICATE_BLOCK_COMMAND,
		REMOVE_BLOCK_COMMAND,
		MOVE_BLOCK_COMMAND,
		UPDATE_BLOCK_NAME_COMMAND,
	} from "@plugin-richtext/menu/rich-text-commands";

	let { authoring, field }: { authoring: FieldAuthoringHost | undefined; field: SchemaField } =
		$props();
	const editor = useLexicalComposerContext()[0];
	const i18n = getAdminI18n();
	let session = $state.raw<{
		draft: EmbeddedSchemaDraft;
		title: string;
		nodeKey?: NodeKey;
		selection: BaseSelection | null;
		position?: { targetNodeKey: NodeKey; insertBefore: boolean };
	}>();
	let message = $state("");

	function close() {
		session?.draft.discard();
		session = undefined;
		queueMicrotask(() => editor.focus());
	}

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

	function apply(payload: Record<string, unknown>) {
		const active = session;
		if (active === undefined || active.draft.stale || !editor.isEditable()) return;
		editor.update(
			() => {
				if (active.nodeKey !== undefined) {
					const node = $getNodeByKey(active.nodeKey);
					if (!isBlockNode(node) || node.getFields()._key !== active.draft.identity) return;
					node.setFields(payload);
					return;
				}
				if (active.position !== undefined) {
					const target = $getNodeByKey(active.position.targetNodeKey);
					if (target !== null) {
						const node = createBlockNode(payload);
						if (active.position.insertBefore) target.insertBefore(node);
						else target.insertAfter(node);
						return;
					}
				}
				const selection = active.selection;
				if (
					$isRangeSelection(selection) &&
					$getNodeByKey(selection.anchor.key) !== null &&
					$getNodeByKey(selection.focus.key) !== null
				)
					$setSelection(selection.clone());
				else $getRoot().selectEnd();
				const current = $getSelection();
				const anchor = $isRangeSelection(current) ? current.anchor.getNode() : undefined;
				const node = createBlockNode(payload);
				$insertNodeToNearestRoot(node);
				if ($isParagraphNode(anchor) && anchor.getChildrenSize() === 0) anchor.remove();
				const paragraph = $createParagraphNode();
				node.insertAfter(paragraph);
				paragraph.select();
			},
			{ tag: HISTORY_PUSH_TAG, discrete: true }
		);
		session = undefined;
		queueMicrotask(() => editor.focus());
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
				OPEN_BLOCK_EDITOR_COMMAND,
				({ nodeKey, blockType, position }) => {
					if (authoring?.beginSchemaDraft === undefined || !editor.isEditable()) return false;
					if (session !== undefined) {
						message = i18n.t("plugin.richtext:block.pendingEdit");
						return true;
					}
					const node = nodeKey === undefined ? undefined : $getNodeByKey(nodeKey);
					const fields = isBlockNode(node) ? node.getFields() : undefined;
					const variantSlug = fields?.blockType ?? blockType;
					const type = richTextBlockTypes(field).find((type) => type.slug === variantSlug);
					if (type === undefined || (nodeKey !== undefined && !isBlockNode(node))) return false;
					const draft =
						fields !== undefined && typeof fields._key === "string"
							? authoring.beginSchemaDraft({ treeKey: "blocks", identity: fields._key })
							: authoring.beginSchemaDraft({
									treeKey: "blocks",
									caseTag: "block",
									variantSlug: type.slug,
								});
					session = {
						draft,
						title: i18n.t(
							nodeKey === undefined
								? "plugin.richtext:block.insertTitle"
								: "plugin.richtext:block.editTitle",
							{ label: type.labels.singular }
						),
						...(nodeKey === undefined ? {} : { nodeKey }),
						selection: $getSelection()?.clone() ?? null,
						...(position === undefined ? {} : { position }),
					};
					message = "";
					return true;
				},
				COMMAND_PRIORITY_EDITOR
			),
			editor.registerCommand(
				DUPLICATE_BLOCK_COMMAND,
				(nodeKey) => {
					if (authoring?.copySchemaPayload === undefined || !editor.isEditable()) return false;
					const node = $getNodeByKey(nodeKey);
					if (!isBlockNode(node)) return false;
					const fields = node.getFields();
					if (typeof fields.blockType !== "string") return false;
					const payload = authoring.copySchemaPayload(
						{ treeKey: "blocks", caseTag: "block", variantSlug: fields.blockType },
						fields
					);
					$addUpdateTag(HISTORY_PUSH_TAG);
					const duplicate = createBlockNode(payload);
					node.insertAfter(duplicate);
					queueMicrotask(() =>
						editor
							.getElementByKey(duplicate.getKey())
							?.querySelector<HTMLElement>("[data-block-select]")
							?.focus()
					);
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
			),
			() => session?.draft.discard()
		);
	});
</script>

{#if message !== ""}<p role="alert" class="text-sm text-destructive">{message}</p>{/if}
{#if session !== undefined && authoring?.schemaDraftEditor !== undefined}
	{@render authoring.schemaDraftEditor({
		draft: session.draft,
		title: session.title,
		onApply: apply,
		onCancel: close,
	})}
{/if}
