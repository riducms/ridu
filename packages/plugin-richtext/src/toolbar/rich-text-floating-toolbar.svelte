<script lang="ts">
	import { mergeRegister } from "@lexical/utils";
	import { getAdminI18n } from "@riducms/plugin";
	import { ToolbarRoot } from "@riducms/ui";
	import { Portal, useLexicalComposerContext, useLexicalEditable } from "@hvniel/lexical-svelte";
	import type { Attachment } from "svelte/attachments";
	import {
		$getSelection,
		$isRangeSelection,
		COMMAND_PRIORITY_LOW,
		KEY_TAB_COMMAND,
		SELECTION_CHANGE_COMMAND,
	} from "lexical";

	import "@plugin-richtext/toolbar/rich-text-toolbar.scss";
	import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import RichTextToolbarControls from "@plugin-richtext/toolbar/rich-text-toolbar-controls.svelte";
	import type { RichTextToolbarState } from "@plugin-richtext/toolbar/rich-text-toolbar-state.svelte";

	let { toolbar: toolbarState, config }: { toolbar: RichTextToolbarState; config: RichTextConfig } =
		$props();

	const editor = useLexicalComposerContext()[0];
	const isEditable = useLexicalEditable();
	const i18n = getAdminI18n();

	let textMenuOpen = $state(false);
	let alignMenuOpen = $state(false);
	let visible = $state(false);
	let left = $state(0);
	let top = $state(0);
	let toolbar: HTMLElement | null = null;
	let selectingPointer: number | undefined;

	function isOwnEditableNode(node: EventTarget | null, root: HTMLElement) {
		const element =
			node instanceof Element ? node : node instanceof Node ? node.parentElement : null;
		return element?.closest("[contenteditable]") === root;
	}

	// The shared toolbar state owns formatting; this toolbar only decides whether and where to show.
	// @lexical-scope
	function $updateToolbar() {
		// Keep the portal out of the pointer path until the native selection gesture ends.
		if (selectingPointer !== undefined) {
			visible = false;
			return;
		}
		if (textMenuOpen || alignMenuOpen) return;
		const selection = $getSelection();
		const domSelection = editor._window?.getSelection() ?? window.getSelection();
		const root = editor.getRootElement();
		if (
			!isEditable() ||
			editor.isComposing() ||
			!$isRangeSelection(selection) ||
			selection.isCollapsed() ||
			selection.getTextContent().replaceAll("\n", "") === "" ||
			domSelection === null ||
			domSelection.rangeCount === 0 ||
			root === null ||
			!isOwnEditableNode(domSelection.anchorNode, root) ||
			(!root.contains(document.activeElement) && !toolbar?.contains(document.activeElement))
		) {
			visible = false;
			return;
		}
		visible = true;
		positionToolbar(domSelection.getRangeAt(0).getBoundingClientRect());
	}

	function positionToolbar(selectionRect: DOMRect) {
		const width = toolbar?.offsetWidth ?? 190;
		const height = toolbar?.offsetHeight ?? 38;
		left = Math.max(
			12,
			Math.min(
				window.innerWidth - width - 12,
				selectionRect.left + selectionRect.width / 2 - width / 2
			)
		);
		const preferredTop = selectionRect.bottom + 6;
		const maximumTop = window.innerHeight - height - 12;
		top = Math.max(12, preferredTop <= maximumTop ? preferredTop : selectionRect.top - height - 6);
	}

	function updateFromEditor() {
		editor.getEditorState().read($updateToolbar, { editor });
	}

	const attachToolbar: Attachment<HTMLElement> = (element) => {
		toolbar = element;
		updateFromEditor();
		return () => {
			if (toolbar === element) toolbar = null;
		};
	};

	function focusToolbar(event: KeyboardEvent) {
		if (event.shiftKey || !visible || toolbar === null) return false;
		const firstControl = toolbar.querySelector<HTMLElement>(
			"button:not([disabled]), a[href], input:not([disabled]), [tabindex]:not([tabindex='-1'])"
		);
		if (firstControl === null) return false;
		event.preventDefault();
		firstControl.focus();
		return true;
	}

	function beginPointerSelection(event: PointerEvent) {
		const root = editor.getRootElement();
		if (event.button !== 0 || root === null || !isOwnEditableNode(event.target, root)) return;
		selectingPointer = event.pointerId;
		visible = false;
	}

	function endPointerSelection(event: PointerEvent) {
		if (event.pointerId !== selectingPointer) return;
		selectingPointer = undefined;
		updateFromEditor();
	}

	function resetPointerSelection() {
		selectingPointer = undefined;
		updateFromEditor();
	}

	function preserveSelection(event: MouseEvent) {
		if (event.target instanceof HTMLInputElement) return;
		event.preventDefault();
	}

	$effect(() => {
		document.addEventListener("pointerdown", beginPointerSelection, true);
		document.addEventListener("pointerup", endPointerSelection, true);
		document.addEventListener("pointercancel", endPointerSelection, true);
		document.addEventListener("selectionchange", updateFromEditor);
		document.addEventListener("focusin", updateFromEditor);
		document.addEventListener("scroll", updateFromEditor, true);
		window.addEventListener("blur", resetPointerSelection);
		window.addEventListener("resize", updateFromEditor);
		const unregister = mergeRegister(
			editor.registerUpdateListener(({ editorState }) => editorState.read($updateToolbar)),
			editor.registerCommand(KEY_TAB_COMMAND, focusToolbar, COMMAND_PRIORITY_LOW),
			editor.registerCommand(
				SELECTION_CHANGE_COMMAND,
				() => {
					$updateToolbar();
					return false;
				},
				COMMAND_PRIORITY_LOW
			)
		);
		return () => {
			unregister();
			document.removeEventListener("pointerdown", beginPointerSelection, true);
			document.removeEventListener("pointerup", endPointerSelection, true);
			document.removeEventListener("pointercancel", endPointerSelection, true);
			document.removeEventListener("selectionchange", updateFromEditor);
			document.removeEventListener("focusin", updateFromEditor);
			document.removeEventListener("scroll", updateFromEditor, true);
			window.removeEventListener("blur", resetPointerSelection);
			window.removeEventListener("resize", updateFromEditor);
		};
	});
</script>

{#if visible}
	<Portal>
		<ToolbarRoot
			{@attach attachToolbar}
			class="ridu-richtext-floating-toolbar"
			aria-label={i18n.t("plugin.richtext:editor.formatText")}
			tabindex={-1}
			style={`left: ${left}px; top: ${top}px;`}
			onmousedown={preserveSelection}
		>
			<RichTextToolbarControls
				toolbar={toolbarState}
				{config}
				bind:textMenuOpen
				bind:alignMenuOpen
			/>
		</ToolbarRoot>
	</Portal>
{/if}
