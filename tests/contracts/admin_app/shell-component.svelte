<script lang="ts">
	import type { AdminShellComponentProps } from "@riducms/plugin";

	let { position }: AdminShellComponentProps = $props();
</script>

{#if position === "header"}
	<p class="text-[11.5px] text-primary">Plugin header</p>
{:else if position === "actions"}
	<button
		type="button"
		class="rounded-md border border-primary/20 px-2.5 py-1 text-[11px] text-primary"
	>
		Plugin action
	</button>
{:else}
	<button
		type="button"
		class="h-9 w-full rounded-md px-2.5 text-left text-[12.5px] hover:bg-white/5"
	>
		Plugin settings
	</button>
{/if}
