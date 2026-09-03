<script lang="ts">
	import Prism from "prismjs";
	import "prismjs/components/prism-bash";
	import "prismjs/components/prism-javascript";
	import "prismjs/components/prism-json";
	import { getAdminI18n } from "@riducms/plugin";

	let {
		value,
		language = "javascript",
		class: className = "",
	}: {
		value: string;
		language?: "javascript" | "bash" | "json";
		class?: string;
	} = $props();
	const i18n = getAdminI18n();

	const normalized = $derived(normalize(value));
	const highlighted = $derived(
		Prism.highlight(normalized, Prism.languages[language] ?? Prism.languages.javascript, language)
	);

	function normalize(source: string) {
		const lines = source.replaceAll("\t", "  ").split("\n");
		while (lines[0]?.trim() === "") lines.shift();
		while (lines.at(-1)?.trim() === "") lines.pop();
		const indentation = lines
			.filter((line) => line.trim() !== "")
			.map((line) => line.match(/^\s*/)?.[0].length ?? 0);
		const minimum = indentation.length === 0 ? 0 : Math.min(...indentation);
		return lines.map((line) => line.slice(minimum)).join("\n");
	}
</script>

<!-- Keyboard focus lets users scroll long examples without a pointer. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<pre
	class={[
		"overflow-x-auto bg-background p-4 font-mono text-[11.5px] leading-6 text-foreground-muted selection:bg-primary/25",
		className,
	]}
	tabindex="0"
	role="region"
	aria-label={i18n.t("general:codeExample")}><code
		class={`language-${language}`}>{@html highlighted}</code></pre>

<style>
	:global(.token.comment),
	:global(.token.prolog),
	:global(.token.doctype),
	:global(.token.cdata) {
		color: color-mix(in srgb, var(--foreground) 42%, transparent);
		font-style: italic;
	}

	:global(.token.punctuation) {
		color: color-mix(in srgb, var(--foreground) 62%, transparent);
	}

	:global(.token.property),
	:global(.token.tag),
	:global(.token.constant),
	:global(.token.symbol),
	:global(.token.deleted) {
		color: var(--syntax-property);
	}

	:global(.token.boolean),
	:global(.token.number) {
		color: var(--syntax-number);
	}

	:global(.token.selector),
	:global(.token.attr-name),
	:global(.token.string),
	:global(.token.char),
	:global(.token.builtin),
	:global(.token.inserted) {
		color: var(--syntax-string);
	}

	:global(.token.operator),
	:global(.token.entity),
	:global(.token.url),
	:global(.language-css .token.string),
	:global(.style .token.string) {
		color: var(--syntax-operator);
	}

	:global(.token.atrule),
	:global(.token.attr-value),
	:global(.token.keyword) {
		color: var(--syntax-keyword);
	}

	:global(.token.function),
	:global(.token.class-name) {
		color: var(--syntax-function);
	}

	:global(.token.regex),
	:global(.token.important),
	:global(.token.variable) {
		color: var(--syntax-variable);
	}
</style>
