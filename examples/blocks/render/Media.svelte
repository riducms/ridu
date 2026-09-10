<script lang="ts">
	import type { Media } from "../generated/ridu.generated";
	let { block }: { block: Media } = $props();
</script>

<figure>
	{#if block.asset && typeof block.asset !== "string"}
		<img src={block.asset.url} alt={block.asset.title} />
	{/if}
	<figcaption>{block.caption ?? ""}</figcaption>
</figure>
