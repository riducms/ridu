<script lang="ts">
	import { Select as SelectPrimitive } from "bits-ui";
	import ChevronDownIcon from "~icons/lucide/chevron-down";
	import { cn, type WithoutChild } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		children,
		size = "field",
		...restProps
	}: WithoutChild<SelectPrimitive.TriggerProps> & {
		size?: "field" | "compact" | "toolbar";
	} = $props();
</script>

<SelectPrimitive.Trigger
	bind:ref
	data-slot="select-trigger"
	data-size={size}
	class={cn(
		"gap-2 rounded-[3px] border border-control-border bg-control px-3 text-[13.5px] text-foreground-strong transition-[background-color,border-color,color] duration-150 hover:border-control-border-hover focus-visible:border-primary/65 aria-invalid:!border-destructive/65 data-[placeholder]:text-foreground-placeholder data-[size=field]:h-10 data-[size=compact]:h-9 data-[size=toolbar]:h-[34px] data-[size=toolbar]:px-2.5 data-[size=toolbar]:text-[12.5px] *:data-[slot=select-value]:flex *:data-[slot=select-value]:gap-1.5 [&_svg:not([class*='size-'])]:size-3.5 flex w-fit items-center justify-between whitespace-nowrap outline-none disabled:cursor-not-allowed disabled:border-control-border-disabled disabled:bg-control-disabled disabled:opacity-45 *:data-[slot=select-value]:line-clamp-1 *:data-[slot=select-value]:flex *:data-[slot=select-value]:items-center [&_svg]:pointer-events-none [&_svg]:shrink-0",
		className
	)}
	{...restProps}
>
	{@render children?.()}
	<ChevronDownIcon class="size-3.5 text-foreground-soft pointer-events-none" />
</SelectPrimitive.Trigger>
