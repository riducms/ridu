<script lang="ts">
	import { isEqualMonth, type DateValue } from "@internationalized/date";
	import { Calendar as CalendarPrimitive } from "bits-ui";
	import type { Snippet } from "svelte";

	import CalendarCaption from "@admin/components/ui/calendar/calendar-caption.svelte";
	import CalendarCell from "@admin/components/ui/calendar/calendar-cell.svelte";
	import CalendarDay from "@admin/components/ui/calendar/calendar-day.svelte";
	import CalendarGridRow from "@admin/components/ui/calendar/calendar-grid-row.svelte";
	import CalendarGrid from "@admin/components/ui/calendar/calendar-grid.svelte";
	import CalendarHeadCell from "@admin/components/ui/calendar/calendar-head-cell.svelte";
	import CalendarHeader from "@admin/components/ui/calendar/calendar-header.svelte";
	import CalendarMonth from "@admin/components/ui/calendar/calendar-month.svelte";
	import CalendarMonths from "@admin/components/ui/calendar/calendar-months.svelte";
	import CalendarNav from "@admin/components/ui/calendar/calendar-nav.svelte";
	import CalendarNextButton from "@admin/components/ui/calendar/calendar-next-button.svelte";
	import CalendarPrevButton from "@admin/components/ui/calendar/calendar-prev-button.svelte";
	import { cn, type ButtonVariant, type WithoutChildrenOrChild } from "@riducms/ui";

	let {
		ref = $bindable(null),
		value = $bindable(),
		placeholder = $bindable(),
		class: className,
		weekdayFormat = "short",
		buttonVariant = "ghost",
		captionLayout = "label",
		locale = "en-US",
		months: availableMonths,
		years,
		monthFormat: requestedMonthFormat,
		yearFormat = "numeric",
		day,
		disableDaysOutsideMonth = false,
		...restProps
	}: WithoutChildrenOrChild<CalendarPrimitive.RootProps> & {
		buttonVariant?: ButtonVariant;
		captionLayout?: "dropdown" | "dropdown-months" | "dropdown-years" | "label";
		months?: CalendarPrimitive.MonthSelectProps["months"];
		years?: CalendarPrimitive.YearSelectProps["years"];
		monthFormat?: CalendarPrimitive.MonthSelectProps["monthFormat"];
		yearFormat?: CalendarPrimitive.YearSelectProps["yearFormat"];
		day?: Snippet<[{ day: DateValue; outsideMonth: boolean }]>;
	} = $props();

	const monthFormat = $derived(
		requestedMonthFormat ?? (captionLayout.startsWith("dropdown") ? "short" : "long")
	);
</script>

<CalendarPrimitive.Root
	bind:value={value as never}
	bind:ref
	bind:placeholder
	{weekdayFormat}
	{disableDaysOutsideMonth}
	class={cn(
		"group/calendar bg-popover p-3 text-popover-foreground [--calendar-cell-size:2rem]",
		className
	)}
	{locale}
	{monthFormat}
	{yearFormat}
	{...restProps}
>
	{#snippet children({ months, weekdays })}
		<CalendarMonths>
			<CalendarNav>
				<CalendarPrevButton variant={buttonVariant} />
				<CalendarNextButton variant={buttonVariant} />
			</CalendarNav>
			{#each months as month, monthIndex (month.value)}
				<CalendarMonth>
					<CalendarHeader>
						<CalendarCaption
							{captionLayout}
							months={availableMonths}
							{monthFormat}
							{years}
							{yearFormat}
							month={month.value}
							bind:placeholder
							{locale}
							{monthIndex}
						/>
					</CalendarHeader>
					<CalendarGrid>
						<CalendarPrimitive.GridHead>
							<CalendarGridRow>
								{#each weekdays as weekday (weekday)}
									<CalendarHeadCell>{weekday.slice(0, 2)}</CalendarHeadCell>
								{/each}
							</CalendarGridRow>
						</CalendarPrimitive.GridHead>
						<CalendarPrimitive.GridBody>
							{#each month.weeks as weekDates (weekDates)}
								<CalendarGridRow>
									{#each weekDates as date (date)}
										<CalendarCell {date} month={month.value}>
											{#if day}
												{@render day({
													day: date,
													outsideMonth: !isEqualMonth(date, month.value),
												})}
											{:else}
												<CalendarDay />
											{/if}
										</CalendarCell>
									{/each}
								</CalendarGridRow>
							{/each}
						</CalendarPrimitive.GridBody>
					</CalendarGrid>
				</CalendarMonth>
			{/each}
		</CalendarMonths>
	{/snippet}
</CalendarPrimitive.Root>
