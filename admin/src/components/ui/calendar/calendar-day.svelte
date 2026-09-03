<script lang="ts">
	import { Calendar as CalendarPrimitive } from "bits-ui";

	import { buttonVariants } from "@admin/components/ui/button";
	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: CalendarPrimitive.DayProps = $props();
</script>

<CalendarPrimitive.Day
	bind:ref
	class={cn(
		buttonVariants({ variant: "ghost", size: "icon-sm" }),
		"size-(--calendar-cell-size) rounded-md p-0 text-[12px] font-normal text-foreground-muted",
		"hover:bg-control-hover hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/60 focus-visible:outline-offset-0",
		"data-[selected]:bg-primary data-[selected]:font-medium data-[selected]:text-primary-foreground data-[selected]:hover:bg-primary-hover data-[selected]:hover:text-primary-foreground",
		"data-[today]:not-data-[selected]:bg-primary/12 data-[today]:not-data-[selected]:font-semibold data-[today]:not-data-[selected]:text-foreground",
		"data-[outside-month]:not-data-[selected]:text-foreground-faint",
		"data-[disabled]:pointer-events-none data-[disabled]:opacity-35 data-[unavailable]:line-through",
		className
	)}
	{...restProps}
/>
