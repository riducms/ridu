<script lang="ts">
	import { Calendar as CalendarPrimitive } from "bits-ui";

	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: CalendarPrimitive.HeaderProps = $props();
</script>

<CalendarPrimitive.Header
	bind:ref
	class={cn(
		"flex h-(--calendar-cell-size) w-full items-center justify-center gap-1.5 text-[12px] font-medium",
		className
	)}
	{...restProps}
/>
