<script lang="ts">
	import { $isLinkNode, TOGGLE_LINK_COMMAND } from "@lexical/link";
	import { mergeRegister } from "@lexical/utils";
	import { getAdminI18n } from "@riducms/plugin";
	import {
		Button,
		Input,
		PopoverContent,
		PopoverRoot,
		PopoverTrigger,
		ToolbarButton,
		ToolbarRoot,
	} from "@riducms/ui";
	import { Portal, useLexicalComposerContext, useLexicalEditable } from "@hvniel/lexical-svelte";
	import {
		$getSelection,
		$isRangeSelection,
		COMMAND_PRIORITY_LOW,
		SELECTION_CHANGE_COMMAND,
		type TextFormatType,
	} from "lexical";
	import { tick } from "svelte";

	import ToolbarIcon from "@plugin-richtext/toolbar/toolbar-icon.svelte";

	const editor = useLexicalComposerContext()[0];
	const isEditable = useLexicalEditable();
	const i18n = getAdminI18n();
	const formatButtons: readonly {
		format: TextFormatType;
		icon: "bold" | "code" | "italic" | "strike" | "subscript" | "superscript" | "underline";
		label: string;
	}[] = [
		{ format: "bold", icon: "bold", label: i18n.t("plugin.richtext:editor.bold") },
		{ format: "italic", icon: "italic", label: i18n.t("plugin.richtext:editor.italic") },
		{ format: "underline", icon: "underline", label: i18n.t("plugin.richtext:editor.underline") },
		{
			format: "strikethrough",
			icon: "strike",
			label: i18n.t("plugin.richtext:editor.strikethrough"),
		},
		{ format: "subscript", icon: "subscript", label: i18n.t("plugin.richtext:editor.subscript") },
		{
			format: "superscript",
			icon: "superscript",
			label: i18n.t("plugin.richtext:editor.superscript"),
		},
		{ format: "code", icon: "code", label: i18n.t("plugin.richtext:editor.inlineCode") },
	];
	const toolbarButtonClass =
		"size-[28.8px] rounded-[3px] p-0 text-foreground-muted hover:bg-control-hover hover:text-foreground focus-visible:bg-control-hover focus-visible:text-foreground focus-visible:outline-none data-[active]:bg-primary/12 data-[active]:text-brand-primary-soft";
	let visible = $state(false);
	let formats = $state.raw<Set<TextFormatType>>(new Set());
	let link = $state(false);
	let linkURL = $state("");
	let linkOpen = $state(false);
	let linkDraft = $state("");
	let left = $state(0);
	let top = $state(0);
	let toolbar = $state<HTMLDivElement | null>(null);
	let linkInput = $state<HTMLInputElement | null>(null);
	let restoreLinkFocus = true;

	// @lexical-scope
	function $updateToolbar() {
		if (linkOpen) return;
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
			!root.contains(domSelection.anchorNode)
		) {
			visible = false;
			return;
		}

		formats = new Set(
			formatButtons.map(({ format }) => format).filter((format) => selection.hasFormat(format))
		);
		const anchorNode = selection.anchor.getNode();
		let linkNode = $isLinkNode(anchorNode) ? anchorNode : undefined;
		const parent = anchorNode.getParent();
		if (linkNode === undefined && $isLinkNode(parent)) linkNode = parent;
		link = linkNode !== undefined;
		linkURL = linkNode?.getURL() ?? "";
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
		const preferredTop = selectionRect.top - height - 9;
		const maximumTop = window.innerHeight - height - 12;
		top = Math.max(12, Math.min(maximumTop, preferredTop));
	}

	function updateFromEditor() {
		editor.getEditorState().read($updateToolbar, { editor });
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

	async function openLinkEditor() {
		linkDraft = linkURL || "https://";
		restoreLinkFocus = true;
		linkOpen = true;
		await tick();
		window.setTimeout(() => {
			linkInput?.focus();
			linkInput?.select();
		});
	}

	async function handleLinkOpenChange(open: boolean) {
		if (open) await openLinkEditor();
		else closeLinkEditor();
	}

	function sanitizeURL(value: string) {
		const trimmed = value.trim();
		if (/^(?:https?:|mailto:|tel:)/i.test(trimmed)) return trimmed;
		return `https://${trimmed}`;
	}

	function applyLink() {
		if (linkDraft.trim() === "" || linkDraft === "https://") return;
		editor.dispatchCommand(TOGGLE_LINK_COMMAND, sanitizeURL(linkDraft));
		closeLinkEditor();
		queueMicrotask(() => editor.focus());
	}

	function removeLink() {
		editor.dispatchCommand(TOGGLE_LINK_COMMAND, null);
		closeLinkEditor();
		queueMicrotask(() => editor.focus());
	}

	function closeLinkEditor() {
		linkOpen = false;
	}

	function handleLinkKeydown(event: KeyboardEvent) {
		if (event.key === "Enter") {
			event.preventDefault();
			applyLink();
		}
	}

	function focusLinkInput(event: Event) {
		event.preventDefault();
		linkInput?.focus();
		linkInput?.select();
	}

	function restoreEditorFocus(event: Event) {
		event.preventDefault();
		const shouldRestore = restoreLinkFocus;
		restoreLinkFocus = true;
		if (shouldRestore) queueMicrotask(() => editor.focus());
	}

	function preserveOutsideFocus(event: Event) {
		restoreLinkFocus = false;
		const target =
			event.target instanceof Element
				? event.target.closest<HTMLElement>(
						"button, a[href], input, select, textarea, [tabindex]:not([tabindex='-1'])"
					)
				: null;
		if (target !== null) window.setTimeout(() => target.focus());
	}

	$effect(() => {
		if (toolbar === null) return;
		updateFromEditor();
	});

	$effect(() => {
		document.addEventListener("selectionchange", updateFromEditor);
		document.addEventListener("scroll", updateFromEditor, true);
		window.addEventListener("resize", updateFromEditor);
		const unregister = mergeRegister(
			editor.registerUpdateListener(({ editorState }) => editorState.read($updateToolbar)),
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
			document.removeEventListener("selectionchange", updateFromEditor);
			document.removeEventListener("scroll", updateFromEditor, true);
			window.removeEventListener("resize", updateFromEditor);
		};
	});
</script>

{#if visible}
	<Portal>
		<ToolbarRoot
			bind:ref={toolbar}
			class="fixed z-80 gap-[0.1rem] rounded-[4px] border border-control-border bg-popover p-1 shadow-[var(--shadow-popover)] animate-[ridu-richtext-toolbar-in_150ms_ease] motion-reduce:animate-none"
			aria-label={i18n.t("plugin.richtext:editor.formatText")}
			tabindex={-1}
			style={`left: ${left}px; top: ${top}px;`}
			onmousedown={preserveSelection}
		>
			{#each formatButtons as button (button.format)}
				<ToolbarButton
					class={toolbarButtonClass}
					type="button"
					active={formats.has(button.format)}
					aria-label={button.label}
					aria-pressed={formats.has(button.format)}
					onclick={() => formatText(button.format)}
				>
					<ToolbarIcon name={button.icon} />
				</ToolbarButton>
			{/each}
			<span class="mx-[0.15rem] h-[1.15rem] w-px bg-border" aria-hidden="true"></span>
			<PopoverRoot open={linkOpen} onOpenChange={handleLinkOpenChange}>
				<PopoverTrigger>
					{#snippet child({ props })}
						<ToolbarButton
							{...props}
							class={toolbarButtonClass}
							type="button"
							active={link}
							aria-label={i18n.t(
								link ? "plugin.richtext:editor.editLink" : "plugin.richtext:editor.addLink"
							)}
							aria-pressed={link}
						>
							<ToolbarIcon name="link" />
						</ToolbarButton>
					{/snippet}
				</PopoverTrigger>

				<PopoverContent
					class="grid w-68 gap-[0.45rem] p-[0.65rem]"
					role="dialog"
					aria-label={i18n.t("plugin.richtext:editor.editLink")}
					trapFocus
					side="bottom"
					align="start"
					onmousedown={preserveSelection}
					onOpenAutoFocus={focusLinkInput}
					onCloseAutoFocus={restoreEditorFocus}
					onInteractOutside={preserveOutsideFocus}
				>
					<label
						class="font-mono text-[10px] tracking-[0.12em] text-foreground-faint uppercase"
						for="ridu-richtext-link-input"
					>
						{i18n.t("plugin.richtext:editor.linkURL")}
					</label>
					<Input
						id="ridu-richtext-link-input"
						class="h-[2.2rem] px-[0.65rem] font-mono text-xs"
						bind:ref={linkInput}
						bind:value={linkDraft}
						aria-label={i18n.t("plugin.richtext:editor.linkURL")}
						onkeydown={handleLinkKeydown}
					/>
					<div class="flex justify-end gap-[0.35rem]">
						{#if link}<Button
								variant="ghost"
								size="sm"
								class="me-auto h-7 px-[0.55rem] text-[11px] text-destructive hover:text-foreground"
								onclick={removeLink}
							>
								{i18n.t("plugin.richtext:editor.remove")}
							</Button>{/if}
						<Button
							variant="ghost"
							size="sm"
							class="h-7 px-[0.55rem] text-[11px]"
							onclick={closeLinkEditor}
						>
							{i18n.t("plugin.richtext:editor.cancel")}
						</Button>
						<Button size="sm" class="h-7 px-[0.55rem] text-[11px]" onclick={applyLink}>
							{i18n.t("plugin.richtext:editor.apply")}
						</Button>
					</div>
				</PopoverContent>
			</PopoverRoot>
		</ToolbarRoot>
	</Portal>
{/if}
