<script lang="ts">
	import { cn, type WithElementRef, type WithoutChildren } from "@riducms/ui";
	import { onMount } from "svelte";
	import type { HTMLAttributes } from "svelte/elements";

	const revealDelayMilliseconds = 180;

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: WithoutChildren<WithElementRef<HTMLAttributes<HTMLDivElement>>> = $props();
	let revealed = $state(false);

	onMount(() => {
		const timer = window.setTimeout(() => (revealed = true), revealDelayMilliseconds);
		return () => window.clearTimeout(timer);
	});
</script>

<div
	bind:this={ref}
	data-slot="skeleton"
	class={cn(
		"rounded bg-muted transition-opacity duration-150 ease-out motion-reduce:transition-none",
		revealed ? "opacity-100" : "opacity-0",
		className
	)}
	{...restProps}
	aria-hidden={!revealed}
></div>
