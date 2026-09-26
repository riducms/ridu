<script lang="ts">
	import { DropdownMenu } from "bits-ui";
	import "@ui/menu/menu.scss";
	let {
		ref = $bindable(null),
		class: className,
		sideOffset = 4,
		align = "start",
		...restProps
	}: DropdownMenu.ContentProps = $props();
</script>

<DropdownMenu.Portal>
	<DropdownMenu.Content
		bind:ref
		{sideOffset}
		{align}
		loop
		class={["ridu-menu", className]}
		{...restProps}
	/>
</DropdownMenu.Portal>
