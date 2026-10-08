import { $isLinkNode, TOGGLE_LINK_COMMAND } from "@lexical/link";
import {
	$isListNode,
	INSERT_CHECK_LIST_COMMAND,
	INSERT_ORDERED_LIST_COMMAND,
	INSERT_UNORDERED_LIST_COMMAND,
} from "@lexical/list";
import {
	$createHeadingNode,
	$createQuoteNode,
	$isHeadingNode,
	$isQuoteNode,
	type HeadingTagType,
} from "@lexical/rich-text";
import { $setBlocksType } from "@lexical/selection";
import { $findMatchingParent } from "@lexical/utils";
import {
	$createParagraphNode,
	$getSelection,
	$isElementNode,
	$isRangeSelection,
	$isTextNode,
	FORMAT_ELEMENT_COMMAND,
	INDENT_CONTENT_COMMAND,
	OUTDENT_CONTENT_COMMAND,
	type EditorState,
	type ElementFormatType,
	type ElementNode,
	type LexicalEditor,
	type TextFormatType,
} from "lexical";

import { OPEN_LINK_EDITOR_COMMAND } from "#lib/menu/rich-text-commands.js";

export const richTextTextFormats = [
	"bold",
	"italic",
	"underline",
	"strikethrough",
	"subscript",
	"superscript",
	"code",
] as const satisfies readonly TextFormatType[];

export type RichTextTextFormat = (typeof richTextTextFormats)[number];

export type RichTextBlockType =
	"paragraph" | HeadingTagType | "quote" | "ordered" | "unordered" | "check";

/** What the toolbars show for the editor's current selection. */
export interface RichTextToolbarSelection {
	/** `none` when the editor has no text selection, `caret` for a collapsed one. */
	readonly kind: "none" | "caret" | "range";
	readonly formats: ReadonlySet<RichTextTextFormat>;
	readonly link: boolean;
	readonly blockType: RichTextBlockType;
	readonly alignment: ElementFormatType;
	readonly canOutdent: boolean;
}

export const emptyToolbarSelection: RichTextToolbarSelection = {
	kind: "none",
	formats: new Set(),
	link: false,
	blockType: "paragraph",
	alignment: "left",
	canOutdent: false,
};

/** Reads the toolbar state from an editor state, or from the editor's current state. */
export function readToolbarSelection(
	editor: LexicalEditor,
	editorState: EditorState = editor.getEditorState()
): RichTextToolbarSelection {
	return editorState.read($readToolbarSelection, { editor });
}

// @lexical-scope
function $readToolbarSelection(): RichTextToolbarSelection {
	const selection = $getSelection();
	if (!$isRangeSelection(selection)) return emptyToolbarSelection;
	const anchor = selection.anchor.getNode();
	const block = $findMatchingParent(
		anchor,
		(node): node is ElementNode => $isElementNode(node) && !node.isInline()
	);
	const list = $findMatchingParent(anchor, $isListNode);
	const blockType: RichTextBlockType = $isHeadingNode(block)
		? block.getTag()
		: $isQuoteNode(block)
			? "quote"
			: $isListNode(list)
				? list.getListType() === "number"
					? "ordered"
					: list.getListType() === "check"
						? "check"
						: "unordered"
				: "paragraph";
	return {
		kind: selection.isCollapsed() ? "caret" : "range",
		formats: new Set(richTextTextFormats.filter((format) => selection.hasFormat(format))),
		link: $selectionIsLinked(),
		blockType,
		alignment: block?.getFormatType() || "left",
		canOutdent: (block?.getIndent() ?? 0) > 0,
	};
}

// @lexical-scope
function $selectionIsLinked() {
	const selection = $getSelection();
	if (!$isRangeSelection(selection)) return false;
	if (selection.isCollapsed())
		return $findMatchingParent(selection.anchor.getNode(), $isLinkNode) !== null;
	const [start, end] = selection.isBackward()
		? [selection.focus, selection.anchor]
		: [selection.anchor, selection.focus];
	// Boundary nodes can be included without contributing any selected characters.
	const selectedText = selection
		.getNodes()
		.filter($isTextNode)
		.filter((node) => {
			const size = node.getTextContentSize();
			if (size === 0) return false;
			if (start.type === "text" && start.key === node.getKey() && start.offset === size)
				return false;
			return !(end.type === "text" && end.key === node.getKey() && end.offset === 0);
		});
	return (
		selectedText.length > 0 &&
		selectedText.every((node) => $findMatchingParent(node, $isLinkNode) !== null)
	);
}

/** Whether two snapshots render identically, so unchanged typing does not re-render toolbars. */
export function sameToolbarSelection(
	left: RichTextToolbarSelection,
	right: RichTextToolbarSelection
) {
	return (
		left.kind === right.kind &&
		left.link === right.link &&
		left.blockType === right.blockType &&
		left.alignment === right.alignment &&
		left.canOutdent === right.canOutdent &&
		left.formats.size === right.formats.size &&
		[...left.formats].every((format) => right.formats.has(format))
	);
}

export function formatText(editor: LexicalEditor, format: RichTextTextFormat) {
	editor.update(() => {
		const selection = $getSelection();
		if ($isRangeSelection(selection)) selection.formatText(format);
	});
}

export function changeBlockType(editor: LexicalEditor, type: RichTextBlockType) {
	if (!editor.isEditable()) return;
	editor.update(() => {
		const selection = $getSelection();
		if (!$isRangeSelection(selection)) return;
		if (type === "ordered") editor.dispatchCommand(INSERT_ORDERED_LIST_COMMAND, undefined);
		else if (type === "unordered") editor.dispatchCommand(INSERT_UNORDERED_LIST_COMMAND, undefined);
		else if (type === "check") editor.dispatchCommand(INSERT_CHECK_LIST_COMMAND, undefined);
		else
			$setBlocksType(selection, () =>
				type === "paragraph"
					? $createParagraphNode()
					: type === "quote"
						? $createQuoteNode()
						: $createHeadingNode(type)
			);
	});
}

export function alignText(editor: LexicalEditor, format: ElementFormatType) {
	if (editor.isEditable()) editor.dispatchCommand(FORMAT_ELEMENT_COMMAND, format);
}

export function changeIndent(editor: LexicalEditor, direction: "indent" | "outdent") {
	editor.dispatchCommand(
		direction === "indent" ? INDENT_CONTENT_COMMAND : OUTDENT_CONTENT_COMMAND,
		undefined
	);
}

/** Removes the link from the selection, or opens the link editor to add one. */
export function toggleLink(editor: LexicalEditor, linked: boolean) {
	if (!editor.isEditable()) return;
	if (linked) editor.dispatchCommand(TOGGLE_LINK_COMMAND, null);
	else editor.dispatchCommand(OPEN_LINK_EDITOR_COMMAND, undefined);
}
