<script lang="ts">
	import { DropdownMenu as DropdownMenuPrimitive } from "bits-ui";
	import "@admin/components/ui/dropdown-menu/dropdown-menu.scss";

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
		class={["ridu-dropdown-menu", className]}
		{...restProps}
	/>
</DropdownMenuPrimitive.Portal>
