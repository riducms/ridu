<script lang="ts">
	import { Select as SelectPrimitive } from "bits-ui";
	import { cn, type WithoutChild, type WithoutChildrenOrChild } from "@riducms/ui";
	import SelectScrollDownButton from "@admin/components/ui/select/select-scroll-down-button.svelte";
	import SelectScrollUpButton from "@admin/components/ui/select/select-scroll-up-button.svelte";

	let {
		ref = $bindable(null),
		class: className,
		sideOffset = 4,
		portalProps,
		children,
		preventScroll = true,
		...restProps
	}: WithoutChild<SelectPrimitive.ContentProps> & {
		portalProps?: WithoutChildrenOrChild<SelectPrimitive.PortalProps>;
	} = $props();
</script>

<SelectPrimitive.Portal {...portalProps}>
	<SelectPrimitive.Content
		bind:ref
		{sideOffset}
		{preventScroll}
		data-slot="select-content"
		class={cn(
			"ridu-popover-enter max-h-(--bits-select-content-available-height) min-w-36 rounded-[10px] border border-control-border bg-popover text-popover-foreground shadow-[var(--shadow-popover)] relative isolate z-50 overflow-x-hidden overflow-y-auto",
			className
		)}
		{...restProps}
	>
		<SelectScrollUpButton />
		<SelectPrimitive.Viewport class={cn("w-full min-w-(--bits-select-anchor-width) scroll-my-1")}>
			{@render children?.()}
		</SelectPrimitive.Viewport>
		<SelectScrollDownButton />
	</SelectPrimitive.Content>
</SelectPrimitive.Portal>
