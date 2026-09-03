<script lang="ts">
	import { Popover as PopoverPrimitive } from "bits-ui";
	import { cn, type WithoutChildrenOrChild } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		sideOffset = 4,
		align = "center",
		portalProps,
		...restProps
	}: PopoverPrimitive.ContentProps & {
		portalProps?: WithoutChildrenOrChild<PopoverPrimitive.PortalProps>;
	} = $props();
</script>

<PopoverPrimitive.Portal {...portalProps}>
	<PopoverPrimitive.Content
		bind:ref
		data-slot="popover-content"
		{sideOffset}
		{align}
		class={cn(
			"ridu-popover-enter flex flex-col gap-4 rounded-[4px] border border-control-border bg-popover p-2 text-sm text-popover-foreground shadow-[var(--shadow-popover)] z-50 w-72 origin-(--bits-popover-content-transform-origin) outline-none",
			className
		)}
		{...restProps}
	/>
</PopoverPrimitive.Portal>
