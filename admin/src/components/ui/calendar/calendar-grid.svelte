<script lang="ts">
	import { Calendar as CalendarPrimitive } from "bits-ui";

	import { cn } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: CalendarPrimitive.GridProps = $props();
</script>

<CalendarPrimitive.Grid
	bind:ref
	class={cn("mt-3 flex w-full flex-col gap-1 border-collapse", className)}
	{...restProps}
/>
