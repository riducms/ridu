<script lang="ts">
	import { RichText, type RichTextBlockComponents } from "@riducms/plugin-richtext/svelte";
	import type { Callout, CalloutDetailBlocksBlock } from "../generated/ridu.generated";
	import CTA from "./CTA.svelte";
	let { block }: { block: Callout } = $props();
	const blocks = { cta: CTA } satisfies RichTextBlockComponents<CalloutDetailBlocksBlock[number]>;
</script>

<aside>
	<h2>{block.title ?? ""}</h2>
	<p>{block.message ?? ""}</p>
	{#if block.detail}<RichText value={block.detail} {blocks} />{/if}
	{#if block.aside}<RichText value={block.aside} />{/if}
</aside>
