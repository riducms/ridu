<script lang="ts">
	import type { Snippet } from "svelte";
	import "@admin/features/auth/auth.scss";

	let { brand, children }: { brand?: Snippet; children: Snippet } = $props();
</script>

<main class="ridu-auth">
	<section class="ridu-auth__content">
		{#if brand !== undefined}
			<header class="ridu-auth__brand">{@render brand()}</header>
		{/if}
		{@render children()}
	</section>
</main>
