<script lang="ts">
	import { Calendar as CalendarPrimitive } from "bits-ui";

	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: CalendarPrimitive.HeadCellProps = $props();
</script>

<CalendarPrimitive.HeadCell
	bind:ref
	class={cn(
		"font-mono w-(--calendar-cell-size) text-center text-[9px] font-medium tracking-[0.08em] text-foreground-faint uppercase",
		className
	)}
	{...restProps}
/>
