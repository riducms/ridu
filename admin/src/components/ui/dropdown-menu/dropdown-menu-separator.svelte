<script lang="ts">
	import { DropdownMenu as DropdownMenuPrimitive } from "bits-ui";
	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: DropdownMenuPrimitive.SeparatorProps = $props();
</script>

<DropdownMenuPrimitive.Separator
	bind:ref
	data-slot="dropdown-menu-separator"
	class={cn("-mx-1 my-1 h-px bg-control-border", className)}
	{...restProps}
/>
