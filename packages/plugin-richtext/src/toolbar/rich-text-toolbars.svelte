<script lang="ts">
	import { Portal, useLexicalComposerContext, useLexicalEditable } from "@hvniel/lexical-svelte";

	import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import RichTextFixedToolbar from "@plugin-richtext/toolbar/rich-text-fixed-toolbar.svelte";
	import RichTextFloatingToolbar from "@plugin-richtext/toolbar/rich-text-floating-toolbar.svelte";
	import { RichTextToolbarState } from "@plugin-richtext/toolbar/rich-text-toolbar-state.svelte";

	let {
		config,
		fixedToolbarSlot,
	}: {
		config: RichTextConfig;
		/** Where the fixed toolbar renders, above the editor text. */
		fixedToolbarSlot: HTMLElement | null;
	} = $props();

	// One selection subscription serves both toolbars for this editor.
	const toolbar = new RichTextToolbarState(useLexicalComposerContext()[0]);
	const editable = useLexicalEditable();

	// Synchronizes the toolbar state with the editor while mounted; cleanup removes the listener.
	$effect(toolbar.connect);
</script>

{#if fixedToolbarSlot !== null}
	<Portal to={fixedToolbarSlot}>
		<RichTextFixedToolbar {toolbar} {config} />
	</Portal>
{/if}
{#if editable()}
	<RichTextFloatingToolbar {toolbar} {config} />
{/if}
