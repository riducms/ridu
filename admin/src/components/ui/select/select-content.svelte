<script lang="ts">
	import { Select as SelectPrimitive } from "bits-ui";
	import { type WithoutChild, type WithoutChildrenOrChild } from "@riducms/ui";
	import SelectScrollDownButton from "@admin/components/ui/select/select-scroll-down-button.svelte";
	import SelectScrollUpButton from "@admin/components/ui/select/select-scroll-up-button.svelte";

	import "@admin/components/ui/select/select.scss";

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
		class={["ridu-select__content", className]}
		{...restProps}
	>
		<SelectScrollUpButton />
		<SelectPrimitive.Viewport class="ridu-select__viewport">
			{@render children?.()}
		</SelectPrimitive.Viewport>
		<SelectScrollDownButton />
	</SelectPrimitive.Content>
</SelectPrimitive.Portal>
