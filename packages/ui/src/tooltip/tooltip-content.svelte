<script lang="ts">
	import { Tooltip as TooltipPrimitive } from "bits-ui";

	import "@ui/tooltip/tooltip.scss";

	let {
		ref = $bindable(null),
		class: className,
		side = "top",
		sideOffset = 6,
		children,
		...restProps
	}: TooltipPrimitive.ContentProps = $props();
</script>

<TooltipPrimitive.Portal>
	<TooltipPrimitive.Content
		bind:ref
		data-slot="tooltip-content"
		{side}
		{sideOffset}
		class={["ridu-tooltip", className]}
		{...restProps}
	>
		{@render children?.()}
		<TooltipPrimitive.Arrow>
			{#snippet child({ props })}
				<div class="ridu-tooltip__arrow" {...props}></div>
			{/snippet}
		</TooltipPrimitive.Arrow>
	</TooltipPrimitive.Content>
</TooltipPrimitive.Portal>
