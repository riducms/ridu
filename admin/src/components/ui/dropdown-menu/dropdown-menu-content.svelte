<script lang="ts">
	import { DropdownMenu as DropdownMenuPrimitive } from "bits-ui";
	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		sideOffset = 5,
		align = "end",
		...restProps
	}: DropdownMenuPrimitive.ContentProps = $props();
</script>

<DropdownMenuPrimitive.Portal>
	<DropdownMenuPrimitive.Content
		bind:ref
		data-slot="dropdown-menu-content"
		{sideOffset}
		{align}
		loop
		class={cn(
			"ridu-popover-enter z-50 min-w-44 origin-(--bits-menu-content-transform-origin) rounded-[4px] border border-control-border bg-popover p-1 text-[12.5px] text-popover-foreground shadow-[var(--shadow-popover)] outline-none",
			className
		)}
		{...restProps}
	/>
</DropdownMenuPrimitive.Portal>
