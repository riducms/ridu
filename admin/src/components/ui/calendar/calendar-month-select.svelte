<script lang="ts">
	import { Calendar as CalendarPrimitive } from "bits-ui";
	import ChevronDownIcon from "~icons/lucide/chevron-down";

	import { cn, type WithoutChildrenOrChild } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		value,
		onchange,
		...restProps
	}: WithoutChildrenOrChild<CalendarPrimitive.MonthSelectProps> = $props();
</script>

<span
	class={cn(
		"relative flex rounded-md border border-control-border bg-control transition-colors focus-within:border-primary/60 focus-within:ring-2 focus-within:ring-primary/15",
		className
	)}
>
	<CalendarPrimitive.MonthSelect bind:ref class="absolute inset-0 opacity-0" {...restProps}>
		{#snippet child({ props, monthItems, selectedMonthItem })}
			<select {...props} {value} {onchange} data-ridu-native-exception>
				{#each monthItems as monthItem (monthItem.value)}
					<option
						value={monthItem.value}
						selected={value === undefined
							? monthItem.value === selectedMonthItem.value
							: monthItem.value === value}
					>
						{monthItem.label}
					</option>
				{/each}
			</select>
			<span
				class="flex h-8 items-center gap-1 ps-2 pe-1 text-[12px] font-medium text-foreground select-none"
				aria-hidden="true"
			>
				{monthItems.find((item) => item.value === value)?.label ?? selectedMonthItem.label}
				<ChevronDownIcon class="size-3 text-foreground-faint" />
			</span>
		{/snippet}
	</CalendarPrimitive.MonthSelect>
</span>
