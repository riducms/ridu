import { $insertNodeToNearestRoot } from "@lexical/utils";
import {
	$createParagraphNode,
	$getPreviousSelection,
	$getRoot,
	$getSelection,
	$isElementNode,
	$isParagraphNode,
	$isRangeSelection,
	type LexicalNode,
} from "lexical";

/** Inserts a reference block and a usable trailing selection within a Lexical update scope. */
export function insertReferenceNode(node: LexicalNode) {
	const selection = $getSelection() ?? $getPreviousSelection();
	if (!$isRangeSelection(selection)) $getRoot().selectEnd();
	const focusNode = $isRangeSelection(selection) ? selection.focus.getNode() : undefined;

	$insertNodeToNearestRoot(node);
	const next = node.getNextSibling();
	if ($isParagraphNode(focusNode) && focusNode.getChildrenSize() === 0 && focusNode !== next) {
		focusNode.remove();
	}

	const trailing = node.getNextSibling();
	if ($isParagraphNode(trailing) && trailing.getChildrenSize() === 0) {
		trailing.select();
		return;
	}

	const paragraph = $createParagraphNode();
	node.insertAfter(paragraph);
	paragraph.select();
}

/** Removes a reference block and selects a valid neighbour within a Lexical update scope. */
export function removeReferenceNode(node: LexicalNode) {
	const next = node.getNextSibling();
	const previous = node.getPreviousSibling();
	node.remove();

	if ($isElementNode(next)) next.selectStart();
	else if ($isElementNode(previous)) previous.selectEnd();
	else {
		const paragraph = $createParagraphNode();
		$getRoot().append(paragraph);
		paragraph.select();
	}
}
