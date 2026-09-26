<script lang="ts">
	import { Calendar, RadioGroup } from "bits-ui";
	import {
		toCalendarDateTime,
		today,
		getLocalTimeZone,
		type DateValue,
	} from "@internationalized/date";
	import { getAdminI18n } from "@riducms/plugin";
	import ChevronLeftIcon from "~icons/lucide/chevron-left";
	import ChevronRightIcon from "~icons/lucide/chevron-right";

	let {
		value,
		timeZone,
		disabled = false,
		readonly = false,
		required = false,
		locale,
		calendarLabel,
		withTime = false,
		onValueChange,
	}: {
		value?: DateValue;
		timeZone?: string;
		disabled?: boolean;
		readonly?: boolean;
		required?: boolean;
		locale: string;
		calendarLabel: string;
		withTime?: boolean;
		onValueChange: (value: DateValue | undefined) => void;
	} = $props();

	const i18n = getAdminI18n();
	const resolvedTimeZone = $derived(timeZone ?? i18n.timeZone ?? getLocalTimeZone());
	const currentYear = $derived(today(resolvedTimeZone).year);
	// Start on the selected date (or today in this control's timezone); navigation owns it thereafter.
	// svelte-ignore state_referenced_locally
	let placeholder = $state(value ?? today(resolvedTimeZone));
	const years = $derived(
		Array.from({ length: 201 }, (_, index) => (value?.year ?? currentYear) - 100 + index)
	);
	const selectedTime = $derived(
		value && "hour" in value ? String(value.hour * 60 + value.minute) : ""
	);
	const times = $derived.by(() => {
		const formatter = new Intl.DateTimeFormat(locale, {
			hour: "numeric",
			minute: "2-digit",
			timeZone: "UTC",
		});
		return Array.from({ length: 48 }, (_, index) => {
			const minutes = index * 30;
			const date = new Date(Date.UTC(2000, 0, 1, Math.floor(minutes / 60), minutes % 60));
			return {
				value: String(minutes),
				label: formatter.format(date),
			};
		});
	});

	function selectTime(minutes: string) {
		if (disabled || readonly || minutes === "") return;
		const date = toCalendarDateTime(value ?? today(resolvedTimeZone));
		onValueChange(
			date.set({ hour: Math.floor(Number(minutes) / 60), minute: Number(minutes) % 60 })
		);
	}
</script>

<div class="ridu-date-picker">
	<Calendar.Root
		type="single"
		bind:placeholder
		{value}
		{disabled}
		{readonly}
		preventDeselect={required}
		initialFocus
		fixedWeeks
		weekdayFormat="short"
		{locale}
		{calendarLabel}
		class="ridu-date-calendar"
		{onValueChange}
	>
		{#snippet children({ months, weekdays })}
			<Calendar.Header class="ridu-date-calendar__header">
				<Calendar.PrevButton class="ridu-date-calendar__nav">
					<ChevronLeftIcon />
				</Calendar.PrevButton>
				<Calendar.MonthSelect monthFormat="long" class="ridu-date-calendar__select" />
				<Calendar.YearSelect {years} class="ridu-date-calendar__select" />
				<Calendar.NextButton class="ridu-date-calendar__nav">
					<ChevronRightIcon />
				</Calendar.NextButton>
			</Calendar.Header>
			{#each months as month (month.value)}
				<Calendar.Grid class="ridu-date-calendar__grid">
					<Calendar.GridHead>
						<Calendar.GridRow>
							{#each weekdays as day, index (index)}
								<Calendar.HeadCell>
									{day.slice(0, 2)}
								</Calendar.HeadCell>
							{/each}
						</Calendar.GridRow>
					</Calendar.GridHead>
					<Calendar.GridBody>
						{#each month.weeks as week (week)}
							<Calendar.GridRow>
								{#each week as date (date)}
									<Calendar.Cell {date} month={month.value}>
										<Calendar.Day class="ridu-date-calendar__day" />
									</Calendar.Cell>
								{/each}
							</Calendar.GridRow>
						{/each}
					</Calendar.GridBody>
				</Calendar.Grid>
			{/each}
		{/snippet}
	</Calendar.Root>
	{#if withTime}
		<div class="ridu-date-picker__time">
			<span class="ridu-date-picker__time-label">{i18n.t("fields:time")}</span>
			<RadioGroup.Root
				class="ridu-date-picker__times"
				value={selectedTime}
				onValueChange={selectTime}
				disabled={disabled || readonly}
				aria-label={i18n.t("fields:time")}
			>
				{#each times as time (time.value)}
					<RadioGroup.Item
						value={time.value}
						class="ridu-date-picker__time-option"
						{@attach (element) => {
							if (time.value === selectedTime) element.scrollIntoView({ block: "nearest" });
						}}
					>
						{time.label}
					</RadioGroup.Item>
				{/each}
			</RadioGroup.Root>
		</div>
	{/if}
</div>
