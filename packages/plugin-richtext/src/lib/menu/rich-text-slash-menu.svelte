<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { PopoverContent, PopoverRoot } from "@riducms/ui";
	import {
		TypeaheadMenuPlugin,
		createBasicTypeaheadTriggerMatch,
		useLexicalComposerContext,
		type MenuResolution,
	} from "@hvniel/lexical-svelte";
	import { $isParagraphNode, type TextNode } from "lexical";

	import type { RichTextEditorExtension } from "#lib/editor/rich-text-extension.js";
	import type { RichTextEditorFeature } from "#lib/field/rich-text-config.js";
	import RichTextMenu from "#lib/menu/rich-text-menu.svelte";
	import {
		buildRichTextOptions,
		filterRichTextOptions,
		type RichTextMenuOption,
	} from "#lib/menu/rich-text-options.js";

	let {
		features,
		extensions,
	}: {
		features: readonly RichTextEditorFeature[];
		extensions: readonly RichTextEditorExtension[];
	} = $props();
	const i18n = getAdminI18n();
	const componentID = $props.id();
	const menuLabelID = `${componentID}-label`;
	const editor = useLexicalComposerContext()[0];
	const trigger = createBasicTypeaheadTriggerMatch("/", { minLength: 0 });
	// Built once: each option's attachment keeps its identity across renders, and the editor's
	// features and extensions don't change while it's mounted.
	// svelte-ignore state_referenced_locally
	const baseOptions = buildRichTextOptions(editor, features, i18n, extensions);
	let query = $state<string | null>(null);
	let menuAnchor = $state.raw<{
		contextElement: HTMLElement;
		getBoundingClientRect: () => DOMRect;
	} | null>(null);
	let menuHost = $state.raw<HTMLElement | null>(null);
	const options = $derived(filterRichTextOptions(baseOptions, query ?? "", i18n.language));

	$effect(() => {
		const anchor = menuHost?.parentElement;
		if (anchor === undefined || anchor === null) return;
		// The upstream anchor rewrites aria-label on every query resolution.
		anchor.setAttribute("aria-labelledby", menuLabelID);
		return () => anchor.removeAttribute("aria-labelledby");
	});

	function openMenu(resolution: MenuResolution) {
		const contextElement = editor.getRootElement();
		if (contextElement === null) return;
		menuAnchor = {
			contextElement,
			getBoundingClientRect: resolution.getRect,
		};
	}

	function closeMenu() {
		menuAnchor = null;
	}

	function preserveEditorFocus(event: Event) {
		event.preventDefault();
	}

	function selectOption(
		option: RichTextMenuOption,
		nodeToRemove: TextNode | null,
		closeMenu: () => void
	) {
		const placeholder = nodeToRemove?.getParent();
		nodeToRemove?.remove();
		option.select();
		if (
			!option.preservePlaceholder &&
			$isParagraphNode(placeholder) &&
			placeholder.getTextContentSize() === 0
		) {
			placeholder.remove();
		}
		closeMenu();
	}
</script>

<TypeaheadMenuPlugin
	onQueryChange={(value) => (query = value)}
	onSelectOption={selectOption}
	onOpen={openMenu}
	onClose={closeMenu}
	{options}
	{trigger}
>
	{#snippet menu(menuProps)}
		<div bind:this={menuHost} role="presentation">
			<span id={menuLabelID} class="ridu-richtext-announcement">
				{i18n.t("plugin.richtext:editor.insertBlock")}
			</span>
		</div>
		{#if menuProps.options.length > 0 && menuAnchor !== null && menuHost !== null}
			<PopoverRoot open>
				<PopoverContent
					portalTo={menuHost}
					class="ridu-richtext-menu-popover"
					role="presentation"
					customAnchor={menuAnchor}
					side="bottom"
					align="start"
					sideOffset={3}
					collisionPadding={8}
					strategy="fixed"
					trapFocus={false}
					onOpenAutoFocus={preserveEditorFocus}
					onCloseAutoFocus={preserveEditorFocus}
				>
					<RichTextMenu
						options={menuProps.options}
						selectedIndex={menuProps.selectedIndex}
						onHighlight={(index) => {
							menuProps.setHighlightedIndex(index);
							editor
								.getRootElement()
								?.setAttribute("aria-activedescendant", `typeahead-item-${index}`);
						}}
						onSelect={menuProps.selectOption}
						idPrefix="typeahead-item"
					/>
				</PopoverContent>
			</PopoverRoot>
		{/if}
	{/snippet}
</TypeaheadMenuPlugin>
