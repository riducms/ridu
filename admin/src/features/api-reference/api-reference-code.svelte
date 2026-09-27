<script lang="ts" module>
	import { tokenize as tokenizeTypeScript } from "@twinkleplop/typescript";
	import { tokenize as tokenizeBash } from "@twinkleplop/bash";
	import { tokenize as tokenizeGo } from "@twinkleplop/go";
	import "@fontsource-variable/martian-mono";
	import type { ReferenceLanguage } from "@admin/features/api-reference/api-reference-examples";

	// Keep the grammars in the drawer's lazy chunk and reuse their tokenizers across examples.
	const tokenizers = {
		typescript: tokenizeTypeScript(),
		curl: tokenizeBash(),
		go: tokenizeGo(),
	};

	function highlightedTokens(code: string, language: ReferenceLanguage) {
		const { tokens, token_types } = tokenizers[language](code);
		const parts: { content: string; type?: string }[] = [];
		let offset = 0;

		for (let index = 0; index < tokens.length; index += 3) {
			const start = tokens[index + 1];
			const end = tokens[index + 2];
			if (start > offset) parts.push({ content: code.slice(offset, start) });
			parts.push({ content: code.slice(start, end), type: token_types[tokens[index]] });
			offset = end;
		}

		if (offset < code.length) parts.push({ content: code.slice(offset) });
		return parts;
	}
</script>

<script lang="ts">
	let { code, language }: { code: string; language: ReferenceLanguage } = $props();
	const highlighted = $derived(highlightedTokens(code, language));
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex (Scrollable examples need a keyboard scroll target.) -->
<pre class="ridu-api-reference__code" dir="ltr" tabindex="0"><code>{#each highlighted as token}<span
				class={token.type}>{token.content}</span>{/each}</code></pre>

<style lang="scss">
	@layer ridu.components {
		.ridu-api-reference__code {
			margin: 0;
			padding: 18px 20px;
			max-height: 430px;
			overflow: auto;
			background: #0d0a0c;
			color: #e9e6e1;
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

			:global(.comment) {
				color: #7d7a8c;
				font-style: italic;
			}

			:global(.keyword),
			:global(.decorator) {
				color: #ff7eb6;
			}

			:global(.string),
			:global(.string_escape),
			:global(.template),
			:global(.regex) {
				color: #b5e48c;
			}

			:global(.number),
			:global(.boolean),
			:global(.constant),
			:global(.property) {
				color: #c4a5ff;
			}

			:global(.function) {
				color: #8fb4ff;
				font-weight: 700;
			}

			:global(.type),
			:global(.class_name) {
				color: #7fdbec;
				font-style: italic;
			}

			:global(.parameter) {
				color: #ffb977;
				font-style: italic;
			}

			:global(.builtin),
			:global(.namespace) {
				color: #8fb4ff;
			}

			:global(.punctuation),
			:global(.operator) {
				color: #80807e;
			}
		}
	}
</style>
