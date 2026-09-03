<script lang="ts">
	import { DropdownMenu as DropdownMenuPrimitive } from "bits-ui";
	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: DropdownMenuPrimitive.ItemProps = $props();
</script>

<DropdownMenuPrimitive.Item
	bind:ref
	data-slot="dropdown-menu-item"
	class={cn(
		"data-[highlighted]:bg-control-hover data-[highlighted]:text-foreground flex min-h-8 cursor-default select-none items-center rounded-[3px] px-2.5 py-1.5 text-foreground-muted outline-none data-[disabled]:pointer-events-none data-[disabled]:opacity-40",
		className
	)}
	{...restProps}
/>
