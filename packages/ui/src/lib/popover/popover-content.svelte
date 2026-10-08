<script lang="ts">
	import { Popover } from "bits-ui";

	import "#lib/popover/popover.scss";

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
		class={["ridu-popover-content", className]}
		{...restProps}
	/>
</Popover.Portal>
