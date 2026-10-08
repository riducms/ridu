<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { MenuContent, MenuItem, MenuRoot, MenuTrigger, ToolbarButton } from "@riducms/ui";
	import type { ElementFormatType } from "lexical";
	import ChevronDownIcon from "~icons/lucide/chevron-down";

	import type { RichTextEditorFeature } from "#lib/field/rich-text-config.js";
	import ToolbarIcon from "#lib/toolbar/toolbar-icon.svelte";
	import type { RichTextToolbarState } from "#lib/toolbar/rich-text-toolbar-state.svelte.js";
	import type { RichTextBlockType } from "#lib/toolbar/rich-text-toolbar-selection.js";

	let {
		toolbar,
		features,
		textMenuOpen = $bindable(false),
		alignMenuOpen = $bindable(false),
	}: {
		toolbar: RichTextToolbarState;
		features: readonly RichTextEditorFeature[];
		/** Whether the text style menu is open. Bound straight to the menu, so it updates before focus moves. */
		textMenuOpen?: boolean;
		/** Whether the alignment menu is open. */
		alignMenuOpen?: boolean;
	} = $props();

	const i18n = getAdminI18n();
	const buttonClass = "ridu-richtext-toolbar-button";
	const formatButtons = [
		{ format: "bold", icon: "bold", labelKey: "plugin.richtext:editor.bold" },
		{ format: "italic", icon: "italic", labelKey: "plugin.richtext:editor.italic" },
		{ format: "underline", icon: "underline", labelKey: "plugin.richtext:editor.underline" },
		{ format: "strikethrough", icon: "strike", labelKey: "plugin.richtext:editor.strikethrough" },
		{ format: "subscript", icon: "subscript", labelKey: "plugin.richtext:editor.subscript" },
		{ format: "superscript", icon: "superscript", labelKey: "plugin.richtext:editor.superscript" },
		{ format: "code", icon: "code", labelKey: "plugin.richtext:editor.inlineCode" },
	] as const;
	// The field's feature set is fixed for this mount.
	// svelte-ignore state_referenced_locally
	const blockOptions: { type: RichTextBlockType; label: () => string }[] = [
		{ type: "paragraph", label: () => i18n.t("plugin.richtext:option.paragraph.label") },
		...([1, 2, 3, 4, 5, 6] as const).map((level) => ({
			type: `h${level}` as const,
			label: () => i18n.t("plugin.richtext:editor.heading", { level }),
		})),
		...(features.includes("lists")
			? [
					{
						type: "ordered" as const,
						label: () => i18n.t("plugin.richtext:option.numberedList.label"),
					},
					{
						type: "unordered" as const,
						label: () => i18n.t("plugin.richtext:option.bulletedList.label"),
					},
					{
						type: "check" as const,
						label: () => i18n.t("plugin.richtext:option.checklist.label"),
					},
				]
			: []),
		{ type: "quote", label: () => i18n.t("plugin.richtext:option.quote.label") },
	];
	// Inline code follows the same fixed feature set.
	// svelte-ignore state_referenced_locally
	const visibleFormatButtons = formatButtons.filter(
		(button) => button.format !== "code" || features.includes("code")
	);
	const alignments = ["left", "center", "right", "justify"] as const;

	const selection = $derived(toolbar.selection);
	const alignmentIcon = $derived(
		(alignments as readonly string[]).includes(selection.alignment)
			? (selection.alignment as (typeof alignments)[number])
			: "left"
	);
	// A link can only be added to selected text; an existing link can be removed from the caret.
	const linkUnavailable = $derived(!selection.link && selection.kind !== "range");

	let restoreMenuFocus = true;

	// Apply the change while the menu is still open: a floating toolbar stays put during the
	// update and then follows the restored editor focus.
	function chooseBlockType(type: RichTextBlockType) {
		toolbar.changeBlockType(type);
		closeMenus();
	}

	function chooseAlignment(format: ElementFormatType) {
		toolbar.alignText(format);
		closeMenus();
	}

	function closeMenus() {
		textMenuOpen = false;
		alignMenuOpen = false;
		toolbar.focusEditor();
	}

	// Bits also reports a close-auto-focus as a menu opens; only return focus once a menu closed.
	function restoreEditorFocus(event: Event) {
		event.preventDefault();
		if (textMenuOpen || alignMenuOpen) return;
		const shouldRestore = restoreMenuFocus;
		restoreMenuFocus = true;
		if (shouldRestore) queueMicrotask(toolbar.focusEditor);
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
</script>

<MenuRoot bind:open={textMenuOpen}>
	<MenuTrigger>
		{#snippet child({ props })}
			<ToolbarButton
				{...props}
				class={[buttonClass, "ridu-richtext-toolbar-menu"]}
				aria-label={i18n.t("plugin.richtext:editor.textStyle")}
			>
				<ToolbarIcon name={selection.blockType} /><ChevronDownIcon width="10" />
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
		{#each blockOptions as option (option.type)}
			<MenuItem
				class="ridu-richtext-format-option"
				data-active={selection.blockType === option.type || undefined}
				onSelect={() => chooseBlockType(option.type)}
			>
				<ToolbarIcon name={option.type} />
				{option.label()}
			</MenuItem>
		{/each}
	</MenuContent>
</MenuRoot>
<span class="ridu-richtext-toolbar-separator" aria-hidden="true"></span>
<MenuRoot bind:open={alignMenuOpen}>
	<MenuTrigger>
		{#snippet child({ props })}
			<ToolbarButton
				{...props}
				class={[buttonClass, "ridu-richtext-toolbar-menu"]}
				aria-label={i18n.t("plugin.richtext:editor.alignment")}
			>
				<ToolbarIcon name={alignmentIcon} /><ChevronDownIcon width="10" />
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
		{#each alignments as format (format)}
			<MenuItem
				class="ridu-richtext-format-option"
				data-active={selection.alignment === format || undefined}
				onSelect={() => chooseAlignment(format)}
			>
				<ToolbarIcon name={format} />
				{i18n.t(`plugin.richtext:editor.align.${format}`)}
			</MenuItem>
		{/each}
	</MenuContent>
</MenuRoot>
<ToolbarButton
	class={buttonClass}
	disabled={!selection.canOutdent}
	aria-label={i18n.t("plugin.richtext:editor.outdent")}
	onclick={toolbar.outdent}
>
	<ToolbarIcon name="outdent" />
</ToolbarButton>
<ToolbarButton
	class={buttonClass}
	aria-label={i18n.t("plugin.richtext:editor.indent")}
	onclick={toolbar.indent}
>
	<ToolbarIcon name="indent" />
</ToolbarButton>
<span class="ridu-richtext-toolbar-separator" aria-hidden="true"></span>

{#each visibleFormatButtons as button (button.format)}
	<ToolbarButton
		class={buttonClass}
		type="button"
		active={selection.formats.has(button.format)}
		aria-label={i18n.t(button.labelKey)}
		aria-pressed={selection.formats.has(button.format)}
		title={i18n.t(button.labelKey)}
		onclick={() => toolbar.formatText(button.format)}
	>
		<ToolbarIcon name={button.icon} />
	</ToolbarButton>
{/each}
{#if features.includes("links")}
	<span class="ridu-richtext-toolbar-separator" aria-hidden="true"></span>
	<ToolbarButton
		class={buttonClass}
		active={selection.link}
		disabled={linkUnavailable}
		aria-label={i18n.t(
			selection.link ? "plugin.richtext:editor.removeLink" : "plugin.richtext:editor.addLink"
		)}
		aria-pressed={selection.link}
		aria-haspopup={selection.link ? undefined : "dialog"}
		onclick={toolbar.toggleLink}
	>
		<ToolbarIcon name="link" />
	</ToolbarButton>
{/if}
