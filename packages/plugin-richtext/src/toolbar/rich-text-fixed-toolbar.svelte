<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { ToolbarRoot } from "@riducms/ui";

	import "@plugin-richtext/toolbar/rich-text-toolbar.scss";
	import type { RichTextConfig } from "@plugin-richtext/field/rich-text-config";
	import RichTextToolbarControls from "@plugin-richtext/toolbar/rich-text-toolbar-controls.svelte";
	import type { RichTextToolbarState } from "@plugin-richtext/toolbar/rich-text-toolbar-state.svelte";

	let { toolbar, config }: { toolbar: RichTextToolbarState; config: RichTextConfig } = $props();

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
	<RichTextToolbarControls {toolbar} {config} />
</ToolbarRoot>
