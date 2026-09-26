<script lang="ts">
	import { Popover as PopoverPrimitive } from "bits-ui";
	import "@admin/components/ui/popover/popover.scss";
	import { type WithoutChildrenOrChild } from "@riducms/ui";

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
		class={["ridu-popover ridu-popover-enter", className]}
		{...restProps}
	/>
</PopoverPrimitive.Portal>
