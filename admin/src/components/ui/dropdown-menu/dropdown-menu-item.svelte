<script lang="ts">
	import { DropdownMenu as DropdownMenuPrimitive } from "bits-ui";
	import "@admin/components/ui/dropdown-menu/dropdown-menu.scss";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: DropdownMenuPrimitive.ItemProps = $props();
</script>

<DropdownMenuPrimitive.Item
	bind:ref
	data-slot="dropdown-menu-item"
	class={["ridu-dropdown-menu__item", className]}
	{...restProps}
/>
