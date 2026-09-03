<script lang="ts">
	import type { AdminBrandComponentProps } from "@riducms/plugin";

	let { surface }: AdminBrandComponentProps = $props();
</script>

{#if surface === "loginLogo"}
	<span class="font-serif text-[27px] italic text-foreground">Ridu extension</span>
	<span class="font-mono text-[9.5px] tracking-[0.18em] text-primary uppercase">Admin</span>
{:else if surface === "navigationLogo"}
	<span>Ridu plugin</span>
{:else}
	<span
		class="font-mono grid size-6 shrink-0 place-items-center rounded-full bg-primary/15 text-[9px] font-semibold text-primary"
		aria-hidden="true"
	>
		PX
	</span>
{/if}
