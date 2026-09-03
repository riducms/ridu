<script lang="ts">
	import { cn, type WithElementRef } from "@riducms/ui";
	import type { HTMLThAttributes } from "svelte/elements";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLThAttributes> = $props();
</script>

<th
	bind:this={ref}
	data-slot="table-head"
	class={cn(
		"h-[31px] px-2.5 text-start align-middle text-[11.5px] font-medium whitespace-nowrap text-foreground-muted [&:has([role=checkbox])]:pe-0",
		className
	)}
	{...restProps}
>
	{@render children?.()}
</th>
