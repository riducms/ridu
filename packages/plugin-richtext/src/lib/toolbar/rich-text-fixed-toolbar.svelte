<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { ToolbarRoot } from "@riducms/ui";

	import "#lib/toolbar/rich-text-toolbar.scss";
	import type { RichTextEditorFeature } from "#lib/field/rich-text-config.js";
	import RichTextToolbarControls from "#lib/toolbar/rich-text-toolbar-controls.svelte";
	import type { RichTextToolbarState } from "#lib/toolbar/rich-text-toolbar-state.svelte.js";

	let {
		toolbar,
		features,
	}: { toolbar: RichTextToolbarState; features: readonly RichTextEditorFeature[] } = $props();

	const i18n = getAdminI18n();

	// Buttons act on the editor's selection, so pressing one must not move focus out of it.
	function preserveSelection(event: MouseEvent) {
		if (event.target instanceof HTMLInputElement) return;
		event.preventDefault();
	}
</script>

<ToolbarRoot
	class="ridu-richtext-fixed-toolbar"
	aria-label={i18n.t("plugin.richtext:editor.toolbar")}
	onmousedown={preserveSelection}
>
	<RichTextToolbarControls {toolbar} {features} />
</ToolbarRoot>
