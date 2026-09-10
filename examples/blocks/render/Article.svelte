<script lang="ts">
	import { RichText, type RichTextBlockComponents } from "@riducms/plugin-richtext/svelte";
	import type { Articles, ArticlesBodyBlocksBlock } from "../generated/ridu.generated";
	import Callout from "./Callout.svelte";
	import CTA from "./CTA.svelte";
	import Media from "./Media.svelte";
	let { article }: { article: Articles } = $props();
	const blocks = { callout: Callout, cta: CTA, media: Media } satisfies RichTextBlockComponents<
		ArticlesBodyBlocksBlock[number]
	>;
</script>

<article>
	<h1>{article.title ?? ""}</h1>
	{#if article.body}<RichText value={article.body} {blocks} />{/if}
</article>
