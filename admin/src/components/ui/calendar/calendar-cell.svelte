<script lang="ts">
	import { Calendar as CalendarPrimitive } from "bits-ui";

	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: CalendarPrimitive.CellProps = $props();
</script>

<CalendarPrimitive.Cell
	bind:ref
	class={cn(
		"relative size-(--calendar-cell-size) p-0 text-center text-[12px] focus-within:z-20",
		className
	)}
	{...restProps}
/>
