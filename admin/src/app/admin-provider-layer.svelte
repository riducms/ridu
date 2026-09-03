<script lang="ts">
	import type { AdminI18n, AdminProvider } from "@riducms/plugin";
	import type { Snippet } from "svelte";

	import AdminProviderLayer from "@admin/app/admin-provider-layer.svelte";

	interface Props {
		providers: readonly AdminProvider[];
		children: Snippet;
		i18n: AdminI18n;
		index?: number;
	}

	let { providers, children, i18n, index = 0 }: Props = $props();
	const provider = $derived(providers[index]);
</script>

{#snippet remainingView()}
	<AdminProviderLayer {providers} {children} {i18n} index={index + 1} />
{/snippet}

{#if provider === undefined}
	{@render children()}
{:else}
	<provider.component defaultView={remainingView} {i18n} />
{/if}
