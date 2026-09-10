<script lang="ts">
	import { getAdminI18n, type FieldAuthoringHost } from "@riducms/plugin";
	import { PopoverContent, PopoverRoot } from "@riducms/ui";
	import {
		TypeaheadMenuPlugin,
		createBasicTypeaheadTriggerMatch,
		useLexicalComposerContext,
		type MenuResolution,
	} from "@hvniel/lexical-svelte";
	import { $isParagraphNode, type TextNode } from "lexical";

	import { getRichTextField } from "@plugin-richtext/field/rich-text-context.svelte";
	import { richTextBlockTypes } from "@plugin-richtext/field/rich-text-blocks";
	import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import RichTextMenu from "@plugin-richtext/menu/rich-text-menu.svelte";
	import {
		buildRichTextOptions,
		filterRichTextOptions,
		type RichTextMenuOption,
	} from "@plugin-richtext/menu/rich-text-options";

	let { authoring, config }: { authoring: FieldAuthoringHost | undefined; config: RichTextConfig } =
		$props();
	const i18n = getAdminI18n();
	const editor = useLexicalComposerContext()[0];
	const trigger = createBasicTypeaheadTriggerMatch("/", { allowWhitespace: true, minLength: 0 });
	// Menu options carry attachment identity and the mounted field's feature set is fixed.
	// svelte-ignore state_referenced_locally
	const baseOptions = buildRichTextOptions(
		editor,
		config,
		i18n,
		authoring,
		richTextBlockTypes(getRichTextField().field)
	);
	let query = $state<string | null>(null);
	let menuAnchor = $state.raw<{
		contextElement: HTMLElement;
		getBoundingClientRect: () => DOMRect;
	} | null>(null);
	let menuHost = $state<HTMLElement | null>(null);
	const options = $derived(filterRichTextOptions(baseOptions, query ?? "", i18n.language));

	$effect(() => {
		if (query !== null) {
			menuHost?.parentElement?.setAttribute(
				"aria-label",
				i18n.t("plugin.richtext:editor.insertBlock")
			);
		}
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
		<div bind:this={menuHost} role="presentation"></div>
		{#if menuProps.options.length > 0 && menuAnchor !== null && menuHost !== null}
			<PopoverRoot open>
				<PopoverContent
					portalTo={menuHost}
					class="w-[min(19rem,calc(100vw-2rem))] p-0 animate-[ridu-richtext-menu-in_150ms_ease] motion-reduce:animate-none"
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
						embedded
						idPrefix="typeahead-item"
					/>
				</PopoverContent>
			</PopoverRoot>
		{/if}
	{/snippet}
</TypeaheadMenuPlugin>
