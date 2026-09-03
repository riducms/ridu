<script lang="ts">
	import { DateFormatter, getLocalTimeZone, type DateValue } from "@internationalized/date";
	import type { ComponentProps } from "svelte";

	import CalendarMonthSelect from "@admin/components/ui/calendar/calendar-month-select.svelte";
	import CalendarYearSelect from "@admin/components/ui/calendar/calendar-year-select.svelte";
	import type Calendar from "@admin/components/ui/calendar/calendar.svelte";

	let {
		captionLayout,
		months,
		monthFormat,
		years,
		yearFormat,
		month,
		locale,
		placeholder = $bindable(),
		monthIndex = 0,
	}: {
		captionLayout: ComponentProps<typeof Calendar>["captionLayout"];
		months: ComponentProps<typeof CalendarMonthSelect>["months"];
		monthFormat: ComponentProps<typeof CalendarMonthSelect>["monthFormat"];
		years: ComponentProps<typeof CalendarYearSelect>["years"];
		yearFormat: ComponentProps<typeof CalendarYearSelect>["yearFormat"];
		month: DateValue;
		placeholder: DateValue | undefined;
		locale: string;
		monthIndex?: number;
	} = $props();

	function formatYear(date: DateValue) {
		const dateObject = date.toDate(getLocalTimeZone());
		if (typeof yearFormat === "function") return yearFormat(dateObject.getFullYear());
		return new DateFormatter(locale, { year: yearFormat }).format(dateObject);
	}

	function formatMonth(date: DateValue) {
		const dateObject = date.toDate(getLocalTimeZone());
		if (typeof monthFormat === "function") return monthFormat(dateObject.getMonth() + 1);
		return new DateFormatter(locale, { month: monthFormat }).format(dateObject);
	}
</script>

{#snippet monthSelect()}
	<CalendarMonthSelect
		{months}
		{monthFormat}
		value={month.month}
		onchange={(event) => {
			if (placeholder === undefined) return;
			const selectedMonth = Number.parseInt(event.currentTarget.value);
			placeholder = placeholder.set({ month: selectedMonth }).subtract({ months: monthIndex });
		}}
	/>
{/snippet}

{#snippet yearSelect()}
	<CalendarYearSelect {years} {yearFormat} value={month.year} />
{/snippet}

{#if captionLayout === "dropdown"}
	{@render monthSelect()}
	{@render yearSelect()}
{:else if captionLayout === "dropdown-months"}
	{@render monthSelect()}
	{#if placeholder !== undefined}{formatYear(placeholder)}{/if}
{:else if captionLayout === "dropdown-years"}
	{#if placeholder !== undefined}{formatMonth(placeholder)}{/if}
	{@render yearSelect()}
{:else}
	{formatMonth(month)} {formatYear(month)}
{/if}
