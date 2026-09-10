<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { useLexicalComposerContext, useLexicalEditable } from "@hvniel/lexical-svelte";
	import {
		$createParagraphNode,
		$getRoot,
		$getSelection,
		$isParagraphNode,
		$isRangeSelection,
	} from "lexical";

	const editor = useLexicalComposerContext()[0];
	const isEditable = useLexicalEditable();
	const i18n = getAdminI18n();
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
					const selection = $getSelection();
					if ($isRangeSelection(selection)) selection.insertText("/");
				});
			},
			{ defaultSelection: "rootEnd" }
		);
	}
</script>

{#if !readOnly}
	<button
		class="mt-[0.55rem] inline-flex cursor-text items-center gap-[0.35rem] border-0 bg-transparent py-[0.35rem] font-mono text-[11px] leading-[1.2] text-foreground-sub transition-colors duration-150 hover:text-foreground-muted focus-visible:rounded focus-visible:text-foreground-muted focus-visible:outline-2 focus-visible:outline-ring/60 focus-visible:outline-offset-3 disabled:cursor-not-allowed disabled:opacity-45"
		type="button"
		disabled={!isEditable()}
		onclick={focusAtEnd}
	>
		<span aria-hidden="true">+</span>
		{i18n.t("plugin.richtext:editor.addBlock")}
		<span class="text-foreground-sub max-[34rem]:hidden">
			{i18n.t("plugin.richtext:editor.footerHint")}
		</span>
	</button>
{/if}
