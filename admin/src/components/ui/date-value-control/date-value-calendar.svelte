<script lang="ts">
	import type { DateValue } from "@internationalized/date";

	import { Calendar } from "@admin/components/ui/calendar";

	let {
		value,
		disabled = false,
		readonly = false,
		required = false,
		locale,
		calendarLabel,
		onValueChange,
	}: {
		value?: DateValue;
		disabled?: boolean;
		readonly?: boolean;
		required?: boolean;
		locale: string;
		calendarLabel: string;
		onValueChange: (value: DateValue | undefined) => void;
	} = $props();
</script>

<Calendar
	type="single"
	{value}
	{disabled}
	{readonly}
	preventDeselect={required}
	initialFocus
	fixedWeeks
	captionLayout="dropdown"
	{locale}
	{calendarLabel}
	onValueChange={(next) => onValueChange(next as DateValue | undefined)}
/>
