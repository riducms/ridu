<script lang="ts">
	import { Popover } from "bits-ui";

	import { cn } from "@ui/utils";

	let {
		ref = $bindable(null),
		class: className,
		portalTo,
		sideOffset = 8,
		align = "start",
		...restProps
	}: Popover.ContentProps & { portalTo?: Element | string } = $props();
</script>

<Popover.Portal {...portalTo === undefined ? {} : { to: portalTo }}>
	<Popover.Content
		bind:ref
		data-slot="popover-content"
		{sideOffset}
		{align}
		class={cn(
			"z-80 overflow-hidden rounded-[4px] border border-control-border bg-popover text-popover-foreground shadow-[var(--shadow-popover)] outline-none origin-(--bits-popover-content-transform-origin)",
			className
		)}
		{...restProps}
	/>
</Popover.Portal>
