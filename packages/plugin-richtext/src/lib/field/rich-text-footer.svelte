<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { useLexicalComposerContext, useLexicalEditable } from "@hvniel/lexical-svelte";
	import { $createParagraphNode, $getRoot, $isParagraphNode } from "lexical";

	const editor = useLexicalComposerContext()[0];
	const isEditable = useLexicalEditable();
	const i18n = getAdminI18n();
	const componentID = $props.id();
	const hintID = `${componentID}-hint`;
	let { readOnly = false }: { readOnly?: boolean } = $props();

	function focusAtEnd() {
		if (!editor.isEditable()) return;
		editor.focus(
			() => {
				editor.update(() => {
					const root = $getRoot();
					const last = root.getLastChild();
					const paragraph =
						$isParagraphNode(last) && last.getTextContentSize() === 0
							? last
							: $createParagraphNode();
					if (paragraph.getParent() === null) root.append(paragraph);
					paragraph.selectEnd();
				});
			},
			{ defaultSelection: "rootEnd" }
		);
	}
</script>

{#if !readOnly}
	<button
		class="ridu-richtext-footer"
		type="button"
		disabled={!isEditable()}
		aria-label={i18n.t("plugin.richtext:editor.insertParagraph")}
		aria-describedby={hintID}
		onclick={focusAtEnd}
	>
		<span aria-hidden="true">+</span>
		<span id={hintID} class="ridu-richtext-announcement">
			{i18n.t("plugin.richtext:editor.footerHint")}
		</span>
	</button>
{/if}
