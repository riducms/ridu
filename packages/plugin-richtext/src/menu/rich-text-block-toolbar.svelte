<script lang="ts">
	import { getAdminI18n, type FieldAuthoringHost } from "@riducms/plugin";
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
	import { DraggableBlockPlugin, useLexicalComposerContext } from "@hvniel/lexical-svelte";
	import { mergeRegister } from "@lexical/utils";
	import {
		$createParagraphNode,
		$createTextNode,
		$getNearestNodeFromDOMNode,
		$getNodeByKey,
		$getRoot,
		$getSelection,
		$isParagraphNode,
		$isRangeSelection,
		COMMAND_PRIORITY_LOW,
		KEY_ARROW_DOWN_COMMAND,
		KEY_ARROW_UP_COMMAND,
		type LexicalNode,
		type NodeKey,
	} from "lexical";
	import type { Attachment } from "svelte/attachments";

	import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import RichTextMenu from "@plugin-richtext/menu/rich-text-menu.svelte";
	import {
		buildRichTextOptions,
		filterRichTextOptions,
		type RichTextMenuOption,
	} from "@plugin-richtext/menu/rich-text-options";

	type PickerState = {
		insertBefore: boolean;
		targetNodeKey: NodeKey;
	};

	let {
		anchorElement,
		authoring,
		config,
	}: {
		anchorElement: HTMLElement;
		authoring: FieldAuthoringHost | undefined;
		config: RichTextConfig;
	} = $props();
	const i18n = getAdminI18n();
	const editor = useLexicalComposerContext()[0];
	let draggableElement = $state.raw<HTMLElement | null>(null);
	let pickerState = $state.raw<PickerState | null>(null);
	let pickerOpen = $state(false);
	let query = $state("");
	let menuElement = $state.raw<HTMLElement | null>(null);
	let movementAnnouncement = $state("");
	let restorePickerFocus = true;
	// The mounted field's authoring surface and feature set are stable.
	// svelte-ignore state_referenced_locally
	const baseOptions = buildRichTextOptions(editor, config, i18n, authoring);
	const options = $derived(filterRichTextOptions(baseOptions, query, i18n.language));

	function isOnToolbar(element: HTMLElement) {
		return element.closest(".ridu-richtext-block-toolbar") !== null;
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

		editor.update(() => {
			const target = $getNodeByKey(state.targetNodeKey);
			if (target === null) return;
			const placeholder = $createParagraphNode();
			const text = $createTextNode("");
			placeholder.append(text);
			if (state.insertBefore) target.insertBefore(placeholder);
			else target.insertAfter(placeholder);
			text.select();
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

	const focusSearchInput: Attachment<HTMLInputElement> = (element) => {
		element.focus();
	};

	$effect(() =>
		mergeRegister(
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
		)
	);
</script>

<PopoverRoot open={pickerOpen} onOpenChange={handlePickerOpenChange}>
	<DraggableBlockPlugin
		{anchorElement}
		isOnMenu={isOnToolbar}
		onElementChanged={(element) => (draggableElement = element)}
	>
		{#snippet menu(attach)}
			<ToolbarRoot
				class="ridu-richtext-block-toolbar absolute top-0 start-0 z-2 flex! h-6 cursor-grab items-center gap-[0.2rem] text-foreground-faint opacity-0 transition-[opacity,color] duration-120 will-change-[transform,opacity] hover:text-foreground-muted focus-within:text-foreground-muted active:cursor-grabbing"
				aria-label={i18n.t("plugin.richtext:editor.blockActions")}
				{@attach attach}
				{@attach attachMenuElement}
			>
				<span
					class="ridu-richtext-block-grip grid h-5 w-[0.8rem] grid-cols-[repeat(2,2px)] grid-rows-[repeat(3,2px)] place-content-center gap-[2px]"
					title={i18n.t("plugin.richtext:editor.dragToMoveBlock")}
					aria-hidden="true"
				>
					<span></span>
					<span></span>
					<span></span>
					<span></span>
					<span></span>
					<span></span>
				</span>
				<TooltipRoot>
					<TooltipTrigger>
						{#snippet child({ props })}
							<ToolbarButton
								{...props}
								class="grid size-5 cursor-pointer place-items-center rounded-[3px] border border-transparent bg-transparent p-0 text-sm leading-none text-foreground-muted hover:border-control-border hover:bg-control-hover hover:text-foreground focus-visible:border-control-border focus-visible:bg-control-hover focus-visible:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45"
								type="button"
								aria-label={i18n.t("plugin.richtext:editor.addBlock")}
								aria-haspopup="dialog"
								aria-expanded={pickerOpen}
								aria-controls={pickerOpen ? "ridu-richtext-block-picker" : undefined}
								onclick={openPicker}
							>
								<span aria-hidden="true">+</span>
							</ToolbarButton>
						{/snippet}
					</TooltipTrigger>
					<TooltipContent>{i18n.t("plugin.richtext:editor.addBlock")}</TooltipContent>
				</TooltipRoot>
			</ToolbarRoot>
		{/snippet}

		{#snippet targetLine(attach)}
			<div
				class="pointer-events-none absolute top-0 left-0 z-2 h-0.5 rounded-full bg-primary opacity-0 will-change-transform"
				{@attach attach}
			></div>
		{/snippet}
	</DraggableBlockPlugin>

	{#if pickerOpen && menuElement !== null}
		<PopoverContent
			id="ridu-richtext-block-picker"
			class="w-[min(19rem,calc(100vw-2rem))] animate-[ridu-richtext-menu-in_150ms_ease] motion-reduce:animate-none"
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
					class="box-border h-[2.6rem] border-0 border-b border-control-border bg-control px-[0.8rem] text-[13px] text-foreground outline-none placeholder:text-foreground-faint focus:border-b-primary/48"
					aria-label={i18n.t("plugin.richtext:editor.filterBlocks")}
					placeholder={i18n.t("plugin.richtext:editor.filterBlocksPlaceholder")}
					bind:value={query}
					onkeydown={handlePickerKeydown}
					{@attach focusSearchInput}
				/>
				{#if options.length > 0}
					<RichTextMenu {options} onSelect={selectOption} embedded command />
				{:else}
					<p class="p-4 text-xs text-foreground-faint">
						{i18n.t("plugin.richtext:editor.noMatchingBlocks")}
					</p>
				{/if}
			</CommandRoot>
		</PopoverContent>
	{/if}
</PopoverRoot>

<p class="sr-only" aria-live="polite" aria-atomic="true" data-richtext-movement-status>
	{movementAnnouncement}
</p>
