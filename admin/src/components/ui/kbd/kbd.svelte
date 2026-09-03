<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";

	import { cn, type WithElementRef } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLElement>> = $props();
</script>

<kbd
	bind:this={ref}
	data-slot="kbd"
	class={cn(
		"font-mono inline-flex min-h-5 items-center justify-center rounded-[4px] border border-control-border px-1.5 text-[10px] leading-none text-foreground-faint",
		className
	)}
	{...restProps}
>
	{@render children?.()}
</kbd>
