<script lang="ts">
	import { Calendar as CalendarPrimitive } from "bits-ui";

	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: CalendarPrimitive.HeadingProps = $props();
</script>

<CalendarPrimitive.Heading
	bind:ref
	class={cn("px-(--calendar-cell-size) text-[12px] font-medium", className)}
	{...restProps}
/>
