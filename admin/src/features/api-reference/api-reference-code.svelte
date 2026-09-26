<script lang="ts" module>
	import { createHighlighterCoreSync } from "shiki/core";
	import { createJavaScriptRegexEngine } from "shiki/engine/javascript";
	import typescript from "shiki/langs/typescript.mjs";
	import shell from "shiki/langs/shellscript.mjs";
	import go from "shiki/langs/go.mjs";
	import { riduCodeTheme } from "@riducms/ui/code-theme";
	import "@fontsource-variable/martian-mono";

	// This module belongs to the drawer's lazy chunk. Reuse one tokenizer across examples.
	const highlighter = createHighlighterCoreSync({
		engine: createJavaScriptRegexEngine(),
		themes: [riduCodeTheme],
		langs: [typescript, shell, go],
	});
</script>

<script lang="ts">
	import type { ReferenceLanguage } from "@admin/features/api-reference/api-reference-examples";

	let { code, language }: { code: string; language: ReferenceLanguage } = $props();

	const highlighted = $derived(
		highlighter.codeToTokens(code, {
			lang: language === "curl" ? "shellscript" : language,
			theme: riduCodeTheme.name,
		})
	);
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex (Scrollable examples need a keyboard scroll target.) -->
<pre
	class="ridu-api-reference__code"
	dir="ltr"
	tabindex="0"
	style:--code-block-foreground={highlighted.fg}><code>{#each highlighted.tokens as line, index}{#if index > 0}{"\n"}{/if}{#each line as token}<span
					style:color={token.color}
					style:font-style={token.fontStyle && token.fontStyle & 1 ? "italic" : undefined}
					style:font-weight={token.fontStyle && token.fontStyle & 2
						? "700"
						: undefined}>{token.content}</span>{/each}{/each}</code></pre>

<style lang="scss">
	@layer ridu.components {
		.ridu-api-reference__code {
			margin: 0;
			padding: 18px 20px;
			max-height: 430px;
			overflow: auto;
			color: var(--code-block-foreground);
			border: 1px solid var(--border);
			border-radius: var(--radius-sm);
			font-family: "Martian Mono Variable", "Martian Mono", ui-monospace, monospace;
			font-size: 14px;
			font-variant-ligatures: none;
			line-height: 1.56;
			tab-size: 2;

			code {
				font: inherit;
			}
		}
	}
</style>
