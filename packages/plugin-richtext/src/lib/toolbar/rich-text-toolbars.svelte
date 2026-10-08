<script lang="ts">
	import { Portal, useLexicalComposerContext, useLexicalEditable } from "@hvniel/lexical-svelte";

	import type { RichTextEditorFeature } from "#lib/field/rich-text-config.js";
	import RichTextFixedToolbar from "#lib/toolbar/rich-text-fixed-toolbar.svelte";
	import RichTextFloatingToolbar from "#lib/toolbar/rich-text-floating-toolbar.svelte";
	import { RichTextToolbarState } from "#lib/toolbar/rich-text-toolbar-state.svelte.js";

	let {
		features,
		fixedToolbarSlot,
		floating,
	}: {
		features: readonly RichTextEditorFeature[];
		/** Where the fixed toolbar renders, above the editor text. */
		fixedToolbarSlot: HTMLElement | null;
		/** Shows the toolbar over a selection. */
		floating: boolean;
	} = $props();

	// One selection subscription serves both toolbars for this editor.
	const toolbar = new RichTextToolbarState(useLexicalComposerContext()[0]);
	const editable = useLexicalEditable();

	// Synchronizes the toolbar state with the editor while mounted; cleanup removes the listener.
	$effect(toolbar.connect);
</script>

{#if fixedToolbarSlot !== null}
	<Portal to={fixedToolbarSlot}>
		<RichTextFixedToolbar {toolbar} {features} />
	</Portal>
{/if}
{#if floating && editable()}
	<RichTextFloatingToolbar {toolbar} {features} />
{/if}
