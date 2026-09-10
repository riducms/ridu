<script lang="ts">
	import { cn, type WithElementRef } from "@ui/utils";
	import type { HTMLInputAttributes, HTMLInputTypeAttribute } from "svelte/elements";

	type InputType = Exclude<HTMLInputTypeAttribute, "file">;

	type Props = WithElementRef<
		Omit<HTMLInputAttributes, "type"> &
			({ type: "file"; files?: FileList } | { type?: InputType; files?: undefined })
	>;

	let {
		ref = $bindable(null),
		value = $bindable(),
		type,
		files = $bindable(),
		class: className,
		"data-slot": dataSlot = "input",
		...restProps
	}: Props = $props();
</script>

{#if type === "file"}
	<input
		bind:this={ref}
		data-slot={dataSlot}
		class={cn(
			"h-10 rounded-[3px] border border-control-border bg-control px-3 py-1 text-[13.5px] text-foreground-strong caret-foreground transition-[background-color,border-color,color] duration-150 file:h-7 file:rounded-[3px] file:border-0 file:bg-control-hover file:px-3 file:text-[13px] file:font-medium file:text-foreground-muted hover:border-control-border-hover focus-visible:border-ring aria-invalid:!border-destructive/65 w-full min-w-0 outline-none file:inline-flex placeholder:text-foreground-placeholder disabled:pointer-events-none disabled:cursor-not-allowed disabled:border-control-border-disabled disabled:bg-control-disabled disabled:text-foreground-soft",
			className
		)}
		type="file"
		bind:files
		bind:value
		{...restProps}
	/>
{:else}
	<input
		bind:this={ref}
		data-slot={dataSlot}
		class={cn(
			"h-10 rounded-[3px] border border-control-border bg-control px-3 py-1 text-[13.5px] text-foreground-strong caret-foreground transition-[background-color,border-color,color] duration-150 hover:border-control-border-hover focus-visible:border-ring aria-invalid:!border-destructive/65 w-full min-w-0 outline-none placeholder:text-foreground-placeholder disabled:pointer-events-none disabled:cursor-not-allowed disabled:border-control-border-disabled disabled:bg-control-disabled disabled:text-foreground-soft read-only:border-control-border-disabled read-only:bg-control-disabled read-only:text-foreground-label",
			className
		)}
		{type}
		bind:value
		{...restProps}
	/>
{/if}
