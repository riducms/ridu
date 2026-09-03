<script lang="ts">
	import { Calendar as CalendarPrimitive } from "bits-ui";
	import ChevronDownIcon from "~icons/lucide/chevron-down";

	import { cn, type WithoutChildrenOrChild } from "@riducms/ui";

	let {
		ref = $bindable(null),
		class: className,
		value,
		...restProps
	}: WithoutChildrenOrChild<CalendarPrimitive.YearSelectProps> = $props();
</script>

<span
	class={cn(
		"relative flex rounded-md border border-control-border bg-control transition-colors focus-within:border-primary/60 focus-within:ring-2 focus-within:ring-primary/15",
		className
	)}
>
	<CalendarPrimitive.YearSelect bind:ref class="absolute inset-0 opacity-0" {...restProps}>
		{#snippet child({ props, yearItems, selectedYearItem })}
			<select {...props} {value} data-ridu-native-exception>
				{#each yearItems as yearItem (yearItem.value)}
					<option
						value={yearItem.value}
						selected={value === undefined
							? yearItem.value === selectedYearItem.value
							: yearItem.value === value}
					>
						{yearItem.label}
					</option>
				{/each}
			</select>
			<span
				class="flex h-8 items-center gap-1 ps-2 pe-1 text-[12px] font-medium text-foreground select-none"
				aria-hidden="true"
			>
				{yearItems.find((item) => item.value === value)?.label ?? selectedYearItem.label}
				<ChevronDownIcon class="size-3 text-foreground-faint" />
			</span>
		{/snippet}
	</CalendarPrimitive.YearSelect>
</span>
