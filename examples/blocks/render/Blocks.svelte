<script lang="ts">
	import { RichText } from "@riducms/plugin-richtext/svelte";
	import type { PagesLayout } from "../generated/ridu.generated";
	let { blocks }: { blocks: PagesLayout } = $props();

	// New generated variants fail type checking; stale runtime data stays visible.
	function unhandledBlock(_block: never): string {
		return "No renderer for this block. Add its renderer to Blocks.svelte.";
	}
</script>

{#each blocks as block (block._key)}
	{#if block.blockType === "hero"}
		<header><h1>{block.heading ?? ""}</h1></header>
	{:else if block.blockType === "content"}
		<section>
			<h2>{block.title ?? ""}</h2>
			{#if block.body}
				<RichText value={block.body}>
					{#snippet fallback(_node: unknown, error: Error)}
						<p role="alert">{error.message}</p>
					{/snippet}
				</RichText>
			{/if}
			{#each block.links ?? [] as link}
				<p>{link.label ?? ""}</p>
			{/each}
		</section>
	{:else if block.blockType === "media"}
		<figure>
			{#if block.asset && typeof block.asset !== "string"}
				<img src={block.asset.url} alt={block.asset.title} />
			{/if}
			<figcaption>{block.caption ?? ""}</figcaption>
		</figure>
	{:else if block.blockType === "cta"}
		<aside>{block.label ?? ""}</aside>
	{:else}
		<p role="alert">{unhandledBlock(block)}</p>
	{/if}
{/each}
