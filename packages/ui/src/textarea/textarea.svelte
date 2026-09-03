<script lang="ts">
	import { cn, type WithElementRef, type WithoutChildren } from "@ui/utils";
	import type { HTMLTextareaAttributes } from "svelte/elements";

	let {
		ref = $bindable(null),
		value = $bindable(),
		class: className,
		"data-slot": dataSlot = "textarea",
		...restProps
	}: WithoutChildren<WithElementRef<HTMLTextareaAttributes>> = $props();
</script>

<textarea
	bind:this={ref}
	data-slot={dataSlot}
	class={cn(
		"resize-y rounded-[3px] border border-control-border bg-control px-3 py-2 text-[13.5px] leading-5 text-foreground-strong caret-foreground transition-[background-color,border-color,color] duration-150 hover:border-control-border-hover focus-visible:border-ring aria-invalid:border-destructive/65 flex field-sizing-content min-h-10 w-full outline-none placeholder:text-foreground-placeholder disabled:cursor-not-allowed disabled:border-control-border-disabled disabled:bg-control-disabled disabled:text-foreground-soft read-only:border-control-border-disabled read-only:bg-control-disabled read-only:text-foreground-label",
		className
	)}
	bind:value
	{...restProps}></textarea>
