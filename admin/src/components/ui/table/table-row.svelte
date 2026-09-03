<script lang="ts">
	import { cn, type WithElementRef } from "@riducms/ui";
	import type { HTMLAttributes } from "svelte/elements";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLTableRowElement>> = $props();
</script>

<tr
	bind:this={ref}
	data-slot="table-row"
	class={cn(
		"border-b border-control-border transition-colors hover:bg-control data-[state=selected]:bg-primary/5",
		className
	)}
	{...restProps}
>
	{@render children?.()}
</tr>
