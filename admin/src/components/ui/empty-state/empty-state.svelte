<script lang="ts">
	import type { Snippet } from "svelte";
	import type { HTMLAttributes } from "svelte/elements";

	import { cn, type WithElementRef } from "@riducms/ui";

	let {
		ref = $bindable(null),
		title,
		description,
		children,
		class: className,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
		title: string;
		description: string;
		children?: Snippet;
	} = $props();
</script>

<div
	bind:this={ref}
	data-slot="empty-state"
	class={cn("grid min-h-52 place-items-center px-5 py-10 text-center", className)}
	{...restProps}
>
	<div class="max-w-md">
		<h2 class="font-serif text-[22px] text-foreground-label">{title}</h2>
		<p class="mt-2 text-[14px] leading-5 text-foreground-sub">{description}</p>
		{#if children}
			<div class="mt-5 flex justify-center">{@render children()}</div>
		{/if}
	</div>
</div>
