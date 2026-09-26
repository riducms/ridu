<script lang="ts">
	import { $createHeadingNode, $createQuoteNode, type HeadingTagType } from "@lexical/rich-text";
	import { $setBlocksType } from "@lexical/selection";
	import {
		INSERT_ORDERED_LIST_COMMAND,
		INSERT_UNORDERED_LIST_COMMAND,
		INSERT_CHECK_LIST_COMMAND,
	} from "@lexical/list";
	import { hasRichTextFeature, type RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import { $isLinkNode, TOGGLE_LINK_COMMAND } from "@lexical/link";
	import { $findMatchingParent, mergeRegister } from "@lexical/utils";
	import { getAdminI18n } from "@riducms/plugin";
	import {
		ToolbarButton,
		ToolbarRoot,
		MenuRoot,
		MenuTrigger,
		MenuContent,
		MenuItem,
	} from "@riducms/ui";
	import { Portal, useLexicalComposerContext, useLexicalEditable } from "@hvniel/lexical-svelte";
	import type { Attachment } from "svelte/attachments";
	import {
		$getSelection,
		$createParagraphNode,
		$isElementNode,
		$isTextNode,
		FORMAT_ELEMENT_COMMAND,
		INDENT_CONTENT_COMMAND,
		OUTDENT_CONTENT_COMMAND,
		type ElementFormatType,
		type ElementNode,
		$isRangeSelection,
		COMMAND_PRIORITY_LOW,
		KEY_TAB_COMMAND,
		SELECTION_CHANGE_COMMAND,
		type TextFormatType,
	} from "lexical";
	import "@plugin-richtext/toolbar/rich-text-toolbar.scss";
	import { OPEN_LINK_EDITOR_COMMAND } from "@plugin-richtext/menu/rich-text-commands";
	import { $isHeadingNode, $isQuoteNode } from "@lexical/rich-text";
	import { $isListNode } from "@lexical/list";
	import ToolbarIcon, { type RichTextIconName } from "@plugin-richtext/toolbar/toolbar-icon.svelte";

	let { config }: { config: RichTextConfig } = $props();
	const editor = useLexicalComposerContext()[0];
	const isEditable = useLexicalEditable();
	const i18n = getAdminI18n();
	const formatButtons = [
		{ format: "bold", icon: "bold", labelKey: "plugin.richtext:editor.bold" },
		{ format: "italic", icon: "italic", labelKey: "plugin.richtext:editor.italic" },
		{ format: "underline", icon: "underline", labelKey: "plugin.richtext:editor.underline" },
		{
			format: "strikethrough",
			icon: "strike",
			labelKey: "plugin.richtext:editor.strikethrough",
		},
		{
			format: "subscript",
			icon: "subscript",
			labelKey: "plugin.richtext:editor.subscript",
		},
		{
			format: "superscript",
			icon: "superscript",
			labelKey: "plugin.richtext:editor.superscript",
		},
		{ format: "code", icon: "code", labelKey: "plugin.richtext:editor.inlineCode" },
	] as const;
	const toolbarButtonClass = "ridu-richtext-toolbar-button";
	let textMenuOpen = $state(false);
	let alignMenuOpen = $state(false);
	let canOutdent = $state(false);
	let visible = $state(false);
	let formats = $state.raw<Set<TextFormatType>>(new Set());
	let link = $state(false);
	let blockType = $state<RichTextIconName>("paragraph");
	let alignment = $state<ElementFormatType>("left");
	let left = $state(0);
	let top = $state(0);
	let toolbar: HTMLElement | null = null;
	let restoreMenuFocus = true;
	let selectingPointer: number | undefined;
	function isOwnEditableNode(node: EventTarget | null, root: HTMLElement) {
		const element =
			node instanceof Element ? node : node instanceof Node ? node.parentElement : null;
		return element?.closest("[contenteditable]") === root;
	}

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

		const block = $findMatchingParent(
			selection.anchor.getNode(),
			(node): node is ElementNode => $isElementNode(node) && !node.isInline()
		);
		canOutdent = (block?.getIndent() ?? 0) > 0;
		alignment = block?.getFormatType() || "left";
		const list = $findMatchingParent(selection.anchor.getNode(), $isListNode);
		blockType = $isHeadingNode(block)
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
		formats = new Set(
			formatButtons.map(({ format }) => format).filter((format) => selection.hasFormat(format))
		);
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
		link =
			selectedText.length > 0 &&
			selectedText.every((node) => $findMatchingParent(node, $isLinkNode) !== null);
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

	// @lexical-scope
	function formatText(format: TextFormatType) {
		editor.update(() => {
			const selection = $getSelection();
			if ($isRangeSelection(selection)) selection.formatText(format);
		});
		queueMicrotask(updateFromEditor);
	}

	function restoreEditorFocus(event: Event) {
		event.preventDefault();
		const shouldRestore = restoreMenuFocus;
		restoreMenuFocus = true;
		if (shouldRestore) queueMicrotask(() => editor.focus());
	}

	function preserveOutsideFocus(event: Event) {
		restoreMenuFocus = false;
		const target =
			event.target instanceof Element
				? event.target.closest<HTMLElement>(
						"button, a[href], input, select, textarea, [tabindex]:not([tabindex='-1'])"
					)
				: null;
		if (target !== null) window.setTimeout(() => target.focus());
	}

	function changeBlock(type: string) {
		if (!editor.isEditable()) return;
		editor.update(() => {
			const selection = $getSelection();
			if (!$isRangeSelection(selection)) return;
			if (type === "ordered") editor.dispatchCommand(INSERT_ORDERED_LIST_COMMAND, undefined);
			else if (type === "unordered")
				editor.dispatchCommand(INSERT_UNORDERED_LIST_COMMAND, undefined);
			else if (type === "check") editor.dispatchCommand(INSERT_CHECK_LIST_COMMAND, undefined);
			else
				$setBlocksType(selection, () =>
					type === "paragraph"
						? $createParagraphNode()
						: type === "quote"
							? $createQuoteNode()
							: $createHeadingNode(type as HeadingTagType)
				);
		});
		textMenuOpen = false;
		editor.focus();
	}

	function alignText(format: ElementFormatType) {
		if (!editor.isEditable()) return;
		editor.dispatchCommand(FORMAT_ELEMENT_COMMAND, format);
		alignMenuOpen = false;
		editor.focus();
	}

	function toggleLink() {
		if (!editor.isEditable()) return;
		if (link) editor.dispatchCommand(TOGGLE_LINK_COMMAND, null);
		else editor.dispatchCommand(OPEN_LINK_EDITOR_COMMAND, undefined);
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
			<MenuRoot bind:open={textMenuOpen}>
				<MenuTrigger>
					{#snippet child({ props })}
						<ToolbarButton
							{...props}
							class="ridu-richtext-toolbar-button ridu-richtext-toolbar-menu"
							aria-label={i18n.t("plugin.richtext:editor.textStyle")}
						>
							<ToolbarIcon name={blockType} /><ChevronDownIcon width="10" />
						</ToolbarButton>
					{/snippet}
				</MenuTrigger>
				<MenuContent
					class="ridu-richtext-format-menu"
					sideOffset={0}
					align="start"
					onCloseAutoFocus={restoreEditorFocus}
					onInteractOutside={preserveOutsideFocus}
				>
					<MenuItem
						class="ridu-richtext-format-option"
						data-active={blockType === "paragraph" || undefined}
						onSelect={() => changeBlock("paragraph")}
					>
						<ToolbarIcon name="paragraph" />
						{i18n.t("plugin.richtext:option.paragraph.label")}
					</MenuItem>
					{#each [1, 2, 3, 4, 5, 6] as level (level)}
						<MenuItem
							class="ridu-richtext-format-option"
							data-active={blockType === `h${level}` || undefined}
							onSelect={() => changeBlock(`h${level}`)}
						>
							<ToolbarIcon name={`h${level}` as RichTextIconName} />
							{i18n.t("plugin.richtext:editor.heading", { level })}
						</MenuItem>
					{/each}
					{#if hasRichTextFeature(config, "lists")}
						<MenuItem
							class="ridu-richtext-format-option"
							data-active={blockType === "ordered" || undefined}
							onSelect={() => changeBlock("ordered")}
						>
							<ToolbarIcon name="ordered" />
							{i18n.t("plugin.richtext:option.numberedList.label")}
						</MenuItem>
						<MenuItem
							class="ridu-richtext-format-option"
							data-active={blockType === "unordered" || undefined}
							onSelect={() => changeBlock("unordered")}
						>
							<ToolbarIcon name="unordered" />
							{i18n.t("plugin.richtext:option.bulletedList.label")}
						</MenuItem>
						<MenuItem
							class="ridu-richtext-format-option"
							data-active={blockType === "check" || undefined}
							onSelect={() => changeBlock("check")}
						>
							<ToolbarIcon name="check" />
							{i18n.t("plugin.richtext:option.checklist.label")}
						</MenuItem>
					{/if}
					<MenuItem
						class="ridu-richtext-format-option"
						data-active={blockType === "quote" || undefined}
						onSelect={() => changeBlock("quote")}
					>
						<ToolbarIcon name="quote" />
						{i18n.t("plugin.richtext:option.quote.label")}
					</MenuItem>
				</MenuContent>
			</MenuRoot>
			<span class="ridu-richtext-toolbar-separator" aria-hidden="true"></span>
			<MenuRoot bind:open={alignMenuOpen}>
				<MenuTrigger>
					{#snippet child({ props })}
						<ToolbarButton
							{...props}
							class="ridu-richtext-toolbar-button ridu-richtext-toolbar-menu"
							aria-label={i18n.t("plugin.richtext:editor.alignment")}
						>
							<ToolbarIcon
								name={alignment === "center" || alignment === "right" || alignment === "justify"
									? alignment
									: "left"}
							/><ChevronDownIcon width="10" />
						</ToolbarButton>
					{/snippet}
				</MenuTrigger>
				<MenuContent
					class="ridu-richtext-format-menu"
					sideOffset={0}
					align="start"
					onCloseAutoFocus={restoreEditorFocus}
					onInteractOutside={preserveOutsideFocus}
				>
					{#each ["left", "center", "right", "justify"] as format (format)}
						<MenuItem
							class="ridu-richtext-format-option"
							data-active={alignment === format || undefined}
							onSelect={() => alignText(format as ElementFormatType)}
						>
							<ToolbarIcon name={format as RichTextIconName} />
							{i18n.t(`plugin.richtext:editor.align.${format}`)}
						</MenuItem>
					{/each}
				</MenuContent>
			</MenuRoot>
			<ToolbarButton
				class={toolbarButtonClass}
				disabled={!canOutdent}
				aria-label={i18n.t("plugin.richtext:editor.outdent")}
				onclick={() => editor.dispatchCommand(OUTDENT_CONTENT_COMMAND, undefined)}
			>
				<ToolbarIcon name="outdent" />
			</ToolbarButton>
			<ToolbarButton
				class={toolbarButtonClass}
				aria-label={i18n.t("plugin.richtext:editor.indent")}
				onclick={() => editor.dispatchCommand(INDENT_CONTENT_COMMAND, undefined)}
			>
				<ToolbarIcon name="indent" />
			</ToolbarButton>
			<span class="ridu-richtext-toolbar-separator" aria-hidden="true"></span>

			{#each formatButtons.filter((button) => button.format !== "code" || hasRichTextFeature(config, "code")) as button (button.format)}
				<ToolbarButton
					class={toolbarButtonClass}
					type="button"
					active={formats.has(button.format)}
					aria-label={i18n.t(button.labelKey)}
					aria-pressed={formats.has(button.format)}
					title={i18n.t(button.labelKey)}
					onclick={() => formatText(button.format)}
				>
					<ToolbarIcon name={button.icon} />
				</ToolbarButton>
			{/each}
			<span class="ridu-richtext-toolbar-separator" aria-hidden="true"></span>
			{#if hasRichTextFeature(config, "links")}
				<ToolbarButton
					class={toolbarButtonClass}
					active={link}
					aria-label={i18n.t(
						link ? "plugin.richtext:editor.removeLink" : "plugin.richtext:editor.addLink"
					)}
					aria-pressed={link}
					aria-haspopup={link ? undefined : "dialog"}
					onclick={toggleLink}
				>
					<ToolbarIcon name="link" />
				</ToolbarButton>
			{/if}
		</ToolbarRoot>
	</Portal>
{/if}
