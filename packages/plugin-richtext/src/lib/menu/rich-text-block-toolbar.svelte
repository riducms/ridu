<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import {
		CommandInput,
		CommandRoot,
		PopoverContent,
		PopoverRoot,
		ToolbarButton,
		ToolbarRoot,
		TooltipContent,
		TooltipRoot,
		TooltipTrigger,
	} from "@riducms/ui";
	import { Portal, useLexicalComposerContext } from "@hvniel/lexical-svelte";
	import { mergeRegister } from "@lexical/utils";
	import {
		$createParagraphNode,
		$getNearestNodeFromDOMNode,
		$getNodeByKey,
		$getRoot,
		$getSelection,
		$isParagraphNode,
		$isRangeSelection,
		COMMAND_PRIORITY_LOW,
		KEY_ARROW_DOWN_COMMAND,
		KEY_ARROW_UP_COMMAND,
		IS_FIREFOX,
		type LexicalNode,
		type NodeKey,
	} from "lexical";
	import type { Attachment } from "svelte/attachments";

	import type { RichTextEditorExtension } from "#lib/editor/rich-text-extension.js";
	import type { RichTextEditorFeature } from "#lib/field/rich-text-config.js";
	import { INSERT_BLOCK_COMMAND } from "#lib/menu/rich-text-commands.js";
	import RichTextMenu from "#lib/menu/rich-text-menu.svelte";
	import {
		buildRichTextOptions,
		filterRichTextOptions,
		type RichTextMenuOption,
	} from "#lib/menu/rich-text-options.js";

	type PickerState = {
		insertBefore: boolean;
		targetNodeKey: NodeKey;
	};

	type DropTarget = {
		element: HTMLElement;
		before: boolean;
		empty: boolean;
		rect: DOMRect;
	};

	let {
		anchorElement,
		features,
		extensions,
		hideDraggableBlockElement,
		hideAddBlockButton,
	}: {
		anchorElement: HTMLElement;
		features: readonly RichTextEditorFeature[];
		extensions: readonly RichTextEditorExtension[];
		hideDraggableBlockElement: boolean;
		hideAddBlockButton: boolean;
	} = $props();
	const i18n = getAdminI18n();
	const editor = useLexicalComposerContext()[0];
	let draggableElement = $state.raw<HTMLElement | null>(null);
	let pickerState = $state.raw<PickerState | null>(null);
	let pickerOpen = $state(false);
	const pickerID = $props.id();
	let query = $state("");
	let menuElement = $state.raw<HTMLElement | null>(null);
	let targetLineElement: HTMLElement | null = null;
	let highlightElement: HTMLElement | null = null;
	let highlightAnimation: Animation | undefined;
	let highlightFrame: number | undefined;
	let draggedNodeKey: NodeKey | null = null;
	let focusFrame: number | undefined;
	let movementAnnouncement = $state("");
	let restorePickerFocus = true;
	// The mounted editor's extensions and feature set are stable.
	// svelte-ignore state_referenced_locally
	const baseOptions = buildRichTextOptions(editor, features, i18n, extensions);
	const options = $derived(filterRichTextOptions(baseOptions, query, i18n.language));
	// Keyboard block movement stays available when both handle controls are hidden.
	const shown = $derived(
		draggableElement !== null && !(hideDraggableBlockElement && hideAddBlockButton)
	);
	const dragDataFormat = "application/x-ridu-richtext-block";

	function isOnToolbar(element: HTMLElement) {
		return element.closest(".ridu-richtext-block-toolbar") !== null;
	}

	function hideTargetLine() {
		if (targetLineElement === null) return;
		targetLineElement.style.opacity = "0";
	}

	function handleDragStart(event: DragEvent) {
		if (draggableElement === null || event.dataTransfer === null) {
			event.preventDefault();
			return;
		}

		editor.read(() => {
			draggedNodeKey = $getNearestNodeFromDOMNode(draggableElement!)?.getKey() ?? null;
		});
		if (draggedNodeKey === null) {
			event.preventDefault();
			return;
		}
		event.dataTransfer.effectAllowed = "move";
		event.dataTransfer.setData(dragDataFormat, draggedNodeKey);
		event.dataTransfer.setDragImage(draggableElement, 0, 0);
		if (IS_FIREFOX) editor.getRootElement()?.focus({ preventScroll: true });
	}

	function handleDragEnd() {
		if (draggedNodeKey === null) return;
		draggedNodeKey = null;
		draggableElement = null;
		hideTargetLine();
		const view = anchorElement.ownerDocument.defaultView;
		if (view === null) return;
		if (focusFrame !== undefined) view.cancelAnimationFrame(focusFrame);
		// Native drag completion can move focus after the drop handler has run.
		focusFrame = view.requestAnimationFrame(() => {
			focusFrame = undefined;
			editor.focus();
			const root = editor.getRootElement();
			if (root !== null && root.ownerDocument.activeElement !== root) {
				root.focus({ preventScroll: true });
			}
		});
	}

	function withinEditorBuffer(
		clientX: number,
		clientY: number,
		horizontalBuffer: number,
		verticalBuffer: number
	) {
		const rect = anchorElement.getBoundingClientRect();
		return (
			clientX >= rect.left - horizontalBuffer &&
			clientX <= rect.right + horizontalBuffer &&
			clientY >= rect.top - verticalBuffer &&
			clientY <= rect.bottom + verticalBuffer
		);
	}

	function isFromAnotherEditor(target: EventTarget | null) {
		const element =
			target instanceof Element ? target : target instanceof Node ? target.parentElement : null;
		return element?.closest(".ridu-richtext-canvas") !== anchorElement;
	}

	function isOwnedDropSurface(target: EventTarget | null) {
		return target instanceof Node && anchorElement.contains(target) && !isFromAnotherEditor(target);
	}

	function findNearestBlock(clientY: number) {
		const keys = editor.read(() => $getRoot().getChildrenKeys());
		const blocks = keys
			.map((key) => editor.getElementByKey(key))
			.filter((element): element is HTMLElement => element !== null);
		if (blocks.length === 0) return null;

		let low = 0;
		let high = blocks.length;
		while (low < high) {
			const middle = (low + high) >>> 1;
			const rect = blocks[middle].getBoundingClientRect();
			if (rect.bottom < clientY) low = middle + 1;
			else high = middle;
		}

		const next = blocks[Math.min(low, blocks.length - 1)];
		const previous = blocks[Math.max(0, low - 1)];
		const nextRect = next.getBoundingClientRect();
		const previousRect = previous.getBoundingClientRect();
		const nextDistance = Math.max(nextRect.top - clientY, clientY - nextRect.bottom, 0);
		const previousDistance = Math.max(previousRect.top - clientY, clientY - previousRect.bottom, 0);
		return previousDistance < nextDistance ? previous : next;
	}

	function findHoveredBlock(target: EventTarget | null, clientY: number) {
		const root = editor.getRootElement();
		if (root === null) return null;
		if (target instanceof Element && root.contains(target)) {
			let element: Element | null = target;
			while (element !== null && element.parentElement !== root) element = element.parentElement;
			if (element instanceof HTMLElement) {
				const key = editor.read(() => {
					const node = $getNearestNodeFromDOMNode(element!);
					return node?.getParent()?.is($getRoot()) ? node.getKey() : null;
				});
				if (key !== null && editor.getElementByKey(key) === element) return element;
			}
		}

		const block = findNearestBlock(clientY);
		if (block === null) return null;
		const rect = block.getBoundingClientRect();
		const style = block.ownerDocument.defaultView?.getComputedStyle(block);
		const marginTop = Number.parseFloat(style?.marginTop ?? "") || 0;
		const marginBottom = Number.parseFloat(style?.marginBottom ?? "") || 0;
		return clientY >= rect.top - marginTop && clientY <= rect.bottom + marginBottom ? block : null;
	}

	function findDropTarget(clientY: number) {
		const element = findNearestBlock(clientY);
		if (element === null) return null;
		const rect = element.getBoundingClientRect();
		const empty = editor.read(() => {
			const node = $getNearestNodeFromDOMNode(element);
			return $isParagraphNode(node) && node.isEmpty();
		});
		return {
			element,
			before: clientY < rect.top + rect.height / 2,
			empty,
			rect,
		};
	}

	// @lexical-scope
	function $dropWouldMove(source: LexicalNode, target: LexicalNode, drop: DropTarget) {
		if (source.is(target)) return false;
		if (drop.empty) return true;
		return drop.before
			? !target.getPreviousSibling()?.is(source)
			: !target.getNextSibling()?.is(source);
	}

	function positionTargetLine(target: DropTarget) {
		if (targetLineElement === null) return;
		const anchorRect = anchorElement.getBoundingClientRect();
		const view = anchorElement.ownerDocument.defaultView;
		if (view === null) return;
		const margin = (element: Element | null, edge: "marginTop" | "marginBottom") =>
			element === null ? 0 : Number.parseFloat(view.getComputedStyle(element)[edge]) || 0;
		const gap = target.before
			? Math.max(
					margin(target.element, "marginTop"),
					margin(target.element.previousElementSibling, "marginBottom")
				)
			: Math.max(
					margin(target.element, "marginBottom"),
					margin(target.element.nextElementSibling, "marginTop")
				);
		const top = target.empty
			? target.rect.top + target.rect.height / 2
			: target.before
				? target.rect.top - gap / 2
				: target.rect.bottom + gap / 2;
		targetLineElement.style.width = `${target.rect.width}px`;
		targetLineElement.style.transform = `translate(${target.rect.left - anchorRect.left}px, ${top - anchorRect.top + anchorElement.scrollTop - 2}px)`;
		targetLineElement.style.opacity = "0.8";
	}

	function highlightMovedBlock(key: NodeKey) {
		const view = anchorElement.ownerDocument.defaultView;
		if (view === null) return;
		if (highlightFrame !== undefined) view.cancelAnimationFrame(highlightFrame);
		highlightAnimation?.cancel();
		// Measure after Lexical reconciles the moved node into its new DOM position.
		highlightFrame = view.requestAnimationFrame(() => {
			highlightFrame = undefined;
			const block = editor.getElementByKey(key);
			if (block === null || highlightElement === null) return;
			const rect = block.getBoundingClientRect();
			const anchorRect = anchorElement.getBoundingClientRect();
			highlightElement.style.width = `${rect.width + 8}px`;
			highlightElement.style.height = `${rect.height + 8}px`;
			highlightElement.style.transform = `translate(${rect.left - anchorRect.left - 4}px, ${rect.top - anchorRect.top + anchorElement.scrollTop - 4}px)`;
			const reducedMotion = view.matchMedia("(prefers-reduced-motion: reduce)").matches;
			highlightAnimation = highlightElement.animate(
				[{ opacity: 0.1 }, { opacity: 0.1, offset: reducedMotion ? 1 : 2 / 3 }, { opacity: 0 }],
				{ duration: reducedMotion ? 1000 : 1500 }
			);
		});
	}

	function positionMenu(element: HTMLElement) {
		if (menuElement === null) return;
		const blockRect = element.getBoundingClientRect();
		const anchorRect = anchorElement.getBoundingClientRect();
		const lineHeight = Number.parseFloat(
			element.ownerDocument.defaultView?.getComputedStyle(element).lineHeight ?? ""
		);
		const height = Number.isFinite(lineHeight) ? lineHeight : blockRect.height;
		const top = blockRect.top + (height - menuElement.getBoundingClientRect().height) / 2;
		menuElement.style.transform = `translate(4px, ${top - anchorRect.top + anchorElement.scrollTop}px)`;
	}

	function updateDropIndicator(event: DragEvent) {
		if (draggedNodeKey === null) return false;
		if (!isOwnedDropSurface(event.target)) {
			hideTargetLine();
			return false;
		}
		const target = findDropTarget(event.clientY);
		if (target === null) return false;
		let canMove = false;
		editor.read(() => {
			const source = $getNodeByKey(draggedNodeKey!);
			const destination = $getNearestNodeFromDOMNode(target.element);
			canMove =
				source !== null && destination !== null && $dropWouldMove(source, destination, target);
		});
		if (canMove) positionTargetLine(target);
		else hideTargetLine();
		event.preventDefault();
		event.stopPropagation();
		return true;
	}

	function handleDragLeave(event: DragEvent) {
		if (draggedNodeKey === null) return;
		const rect = anchorElement.getBoundingClientRect();
		if (
			event.clientX < rect.left ||
			event.clientX > rect.right ||
			event.clientY < rect.top ||
			event.clientY > rect.bottom
		)
			hideTargetLine();
	}

	function handleDragEnter(event: DragEvent) {
		if (draggedNodeKey !== null && !isOwnedDropSurface(event.target)) hideTargetLine();
	}

	function handleDocumentDrop(event: DragEvent) {
		if (draggedNodeKey === null || !isOwnedDropSurface(event.target)) return;
		const target = findDropTarget(event.clientY);
		if (target === null) return;
		const sourceKey = draggedNodeKey;
		editor.update(() => {
			const source = $getNodeByKey(sourceKey);
			const destination = $getNearestNodeFromDOMNode(target.element);
			if (source === null || destination === null || !$dropWouldMove(source, destination, target))
				return;
			if (target.empty) {
				// replace() in Lexical 0.49 leaves the count stale when moving a sibling.
				destination.insertBefore(source);
				destination.remove();
			} else if (target.before) destination.insertBefore(source);
			else destination.insertAfter(source);
			highlightMovedBlock(sourceKey);
		});
		event.preventDefault();
		event.stopPropagation();
		handleDragEnd();
	}

	function closePicker() {
		pickerOpen = false;
		pickerState = null;
	}

	function handlePickerOpenChange(open: boolean) {
		if (!open) closePicker();
	}

	function restoreEditorFocus(event: Event) {
		event.preventDefault();
		if (restorePickerFocus) queueMicrotask(() => editor.focus());
		restorePickerFocus = true;
	}

	function preserveOutsideFocus(event: Event) {
		restorePickerFocus = false;
		const target =
			event.target instanceof Element
				? event.target.closest<HTMLElement>(
						"button, a[href], input, select, textarea, [tabindex]:not([tabindex='-1'])"
					)
				: null;
		if (target !== null) window.setTimeout(() => target.focus());
	}

	function closePickerFromEscape() {
		restorePickerFocus = true;
		closePicker();
	}

	function handlePickerKeydown(event: KeyboardEvent) {
		if (event.key !== "Escape") return;
		event.preventDefault();
		event.stopPropagation();
		closePickerFromEscape();
	}

	function openPicker(event: MouseEvent) {
		event.preventDefault();
		event.stopPropagation();
		if (draggableElement === null || menuElement === null) return;

		let targetNodeKey: NodeKey | null = null;
		editor.read(() => {
			targetNodeKey = $getNearestNodeFromDOMNode(draggableElement!)?.getKey() ?? null;
		});
		if (targetNodeKey === null) return;

		pickerState = {
			insertBefore: event.altKey || event.ctrlKey,
			targetNodeKey,
		};
		query = "";
		restorePickerFocus = true;
		pickerOpen = true;
	}

	// @lexical-scope
	function $moveBlock(block: LexicalNode, direction: -1 | 1) {
		const siblings = $getRoot().getChildren();
		const index = siblings.findIndex((candidate) => candidate.is(block));
		if (index < 0) return undefined;
		const targetIndex = index + direction;
		if (targetIndex < 0 || targetIndex >= siblings.length) {
			return i18n.t(
				direction < 0
					? "plugin.richtext:editor.blockAlreadyFirst"
					: "plugin.richtext:editor.blockAlreadyLast"
			);
		}
		const target = siblings[targetIndex];
		if (direction < 0) target.insertBefore(block);
		else target.insertAfter(block);
		return i18n.t("plugin.richtext:editor.blockMovedPosition", {
			position: i18n.formatNumber(targetIndex + 1),
			total: i18n.formatNumber(siblings.length),
		});
	}

	function announceMovement(message: string | undefined) {
		if (message === undefined) return;
		movementAnnouncement = "";
		queueMicrotask(() => {
			movementAnnouncement = message;
			editor.focus();
		});
	}

	function selectOption(option: RichTextMenuOption) {
		const state = pickerState;
		restorePickerFocus = option.restoreEditorFocus;
		closePicker();
		if (state === null) return;
		if (option.blockType !== undefined) {
			editor.dispatchCommand(INSERT_BLOCK_COMMAND, {
				blockType: option.blockType,
				position: state,
			});
			return;
		}

		editor.update(() => {
			const target = $getNodeByKey(state.targetNodeKey);
			if (target === null) return;
			const placeholder =
				$isParagraphNode(target) && target.isEmpty() ? target : $createParagraphNode();
			if (placeholder !== target) {
				if (state.insertBefore) target.insertBefore(placeholder);
				else target.insertAfter(placeholder);
			}
			placeholder.select();
			option.select();

			if (option.preservePlaceholder) return;
			const latestPlaceholder = placeholder.getLatest();
			if (!$isParagraphNode(latestPlaceholder)) return;
			if (latestPlaceholder.getTextContentSize() === 0) {
				latestPlaceholder.remove();
			}
		});
	}

	const attachMenuElement: Attachment<HTMLElement> = (element) => {
		menuElement = element;
		return () => {
			if (menuElement === element) menuElement = null;
		};
	};

	const attachTargetLine: Attachment<HTMLElement> = (element) => {
		targetLineElement = element;
		return () => {
			if (targetLineElement === element) targetLineElement = null;
		};
	};

	const attachHighlight: Attachment<HTMLElement> = (element) => {
		highlightElement = element;
		return () => {
			if (highlightElement === element) highlightElement = null;
		};
	};

	const focusSearchInput: Attachment<HTMLInputElement> = (element) => {
		element.focus();
	};

	$effect(() => {
		const ownerDocument = anchorElement.ownerDocument;
		function handleMouseMove(event: MouseEvent) {
			if (draggedNodeKey !== null || pickerOpen) return;
			if (event.target instanceof HTMLElement && isOnToolbar(event.target)) return;
			draggableElement =
				!isFromAnotherEditor(event.target) &&
				withinEditorBuffer(event.clientX, event.clientY, 50, 25)
					? findHoveredBlock(event.target, event.clientY)
					: null;
		}
		function handleScroll() {
			if (draggableElement !== null) positionMenu(draggableElement);
		}
		const unregisterCommands = mergeRegister(
			editor.registerCommand(
				KEY_ARROW_UP_COMMAND,
				(event) => {
					if (event === null || !event.altKey || !event.shiftKey) return false;
					const selection = $getSelection();
					if (!$isRangeSelection(selection)) return false;
					event.preventDefault();
					announceMovement($moveBlock(selection.anchor.getNode().getTopLevelElementOrThrow(), -1));
					return true;
				},
				COMMAND_PRIORITY_LOW
			),
			editor.registerCommand(
				KEY_ARROW_DOWN_COMMAND,
				(event) => {
					if (event === null || !event.altKey || !event.shiftKey) return false;
					const selection = $getSelection();
					if (!$isRangeSelection(selection)) return false;
					event.preventDefault();
					announceMovement($moveBlock(selection.anchor.getNode().getTopLevelElementOrThrow(), 1));
					return true;
				},
				COMMAND_PRIORITY_LOW
			)
		);
		ownerDocument.addEventListener("mousemove", handleMouseMove);
		ownerDocument.addEventListener("dragover", updateDropIndicator, true);
		ownerDocument.addEventListener("dragenter", handleDragEnter, true);
		ownerDocument.addEventListener("dragleave", handleDragLeave, true);
		ownerDocument.addEventListener("drop", handleDocumentDrop, true);
		ownerDocument.addEventListener("scroll", handleScroll, true);
		ownerDocument.defaultView?.addEventListener("resize", handleScroll);
		return () => {
			unregisterCommands();
			ownerDocument.removeEventListener("mousemove", handleMouseMove);
			ownerDocument.removeEventListener("dragover", updateDropIndicator, true);
			ownerDocument.removeEventListener("dragenter", handleDragEnter, true);
			ownerDocument.removeEventListener("dragleave", handleDragLeave, true);
			ownerDocument.removeEventListener("drop", handleDocumentDrop, true);
			ownerDocument.removeEventListener("scroll", handleScroll, true);
			ownerDocument.defaultView?.removeEventListener("resize", handleScroll);
			draggedNodeKey = null;
			hideTargetLine();
			if (focusFrame !== undefined) ownerDocument.defaultView?.cancelAnimationFrame(focusFrame);
			if (highlightFrame !== undefined)
				ownerDocument.defaultView?.cancelAnimationFrame(highlightFrame);
			highlightAnimation?.cancel();
		};
	});

	$effect(() => {
		if (draggableElement !== null) positionMenu(draggableElement);
	});
</script>

<PopoverRoot open={pickerOpen} onOpenChange={handlePickerOpenChange}>
	<Portal to={anchorElement}>
		<ToolbarRoot
			class={["ridu-richtext-block-toolbar", shown && "is-visible"]}
			inert={!shown}
			aria-hidden={shown ? undefined : true}
			aria-label={i18n.t("plugin.richtext:editor.blockActions")}
			{@attach attachMenuElement}
		>
			{#if !hideDraggableBlockElement}
				<ToolbarButton
					class="ridu-richtext-block-grip"
					type="button"
					draggable="true"
					title={i18n.t("plugin.richtext:editor.dragToMoveBlock")}
					aria-label={i18n.t("plugin.richtext:editor.dragToMoveBlock")}
					ondragstart={handleDragStart}
					ondragend={handleDragEnd}
				>
					<span aria-hidden="true"></span>
					<span aria-hidden="true"></span>
					<span aria-hidden="true"></span>
					<span aria-hidden="true"></span>
					<span aria-hidden="true"></span>
					<span aria-hidden="true"></span>
				</ToolbarButton>
			{/if}
			{#if !hideAddBlockButton}
				<TooltipRoot>
					<TooltipTrigger>
						{#snippet child({ props })}
							<ToolbarButton
								{...props}
								class="ridu-richtext-block-add"
								type="button"
								aria-label={i18n.t("plugin.richtext:editor.addBlock")}
								aria-haspopup="dialog"
								aria-expanded={pickerOpen}
								aria-controls={pickerOpen ? pickerID : undefined}
								onclick={openPicker}
							>
								<span aria-hidden="true">+</span>
							</ToolbarButton>
						{/snippet}
					</TooltipTrigger>
					<TooltipContent>{i18n.t("plugin.richtext:editor.addBlock")}</TooltipContent>
				</TooltipRoot>
			{/if}
		</ToolbarRoot>
		<div class="ridu-richtext-drop-line" {@attach attachTargetLine}></div>
		<div class="ridu-richtext-drop-highlight" aria-hidden="true" {@attach attachHighlight}></div>
	</Portal>

	{#if pickerOpen && menuElement !== null}
		<PopoverContent
			id={pickerID}
			class="ridu-richtext-menu-popover"
			role="dialog"
			aria-label={i18n.t("plugin.richtext:editor.insertBlock")}
			customAnchor={menuElement}
			side={i18n.direction === "rtl" ? "left" : "right"}
			align="start"
			sideOffset={8}
			strategy="fixed"
			trapFocus
			onCloseAutoFocus={restoreEditorFocus}
			onInteractOutside={preserveOutsideFocus}
		>
			<CommandRoot label={i18n.t("plugin.richtext:editor.filterBlocks")} shouldFilter={false} loop>
				<CommandInput
					type="search"
					class="ridu-richtext-picker-search"
					aria-label={i18n.t("plugin.richtext:editor.filterBlocks")}
					placeholder={i18n.t("plugin.richtext:editor.filterBlocksPlaceholder")}
					bind:value={query}
					onkeydown={handlePickerKeydown}
					{@attach focusSearchInput}
				/>
				{#if options.length > 0}
					<RichTextMenu {options} onSelect={selectOption} command />
				{:else}
					<p class="ridu-richtext-menu-empty">
						{i18n.t("plugin.richtext:editor.noMatchingBlocks")}
					</p>
				{/if}
			</CommandRoot>
		</PopoverContent>
	{/if}
</PopoverRoot>

<p
	class="ridu-richtext-announcement"
	aria-live="polite"
	aria-atomic="true"
	data-richtext-movement-status
>
	{movementAnnouncement}
</p>
