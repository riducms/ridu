<script lang="ts">
	import type { Snippet } from "svelte";

	let {
		brand,
		children,
		wide = false,
	}: { brand?: Snippet; children: Snippet; wide?: boolean } = $props();
</script>

<main class="grid min-h-screen place-items-center bg-background px-5 py-12 sm:px-8">
	<section class={["w-full", wide ? "max-w-180" : "max-w-120"]}>
		{#if brand !== undefined}
			<header class="mb-10 flex items-center">{@render brand()}</header>
		{/if}
		{@render children()}
	</section>
</main>
