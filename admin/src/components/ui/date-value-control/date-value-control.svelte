<script lang="ts">
	import type { DateValue, Time } from "@internationalized/date";
	import type { SchemaDateFormat } from "@riducms/protocol";
	import { DateField, TimeField } from "bits-ui";
	import "@admin/components/ui/date-value-control/date-value-control.scss";
	import XIcon from "~icons/lucide/x";
	import CalendarIcon from "~icons/lucide/calendar";

	import { fieldControlARIA } from "@riducms/ui";
	import DateValueCalendar from "@admin/components/ui/date-value-control/date-value-calendar.svelte";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";

	import { getAdminI18n } from "@riducms/plugin";
	import {
		datePickerFormValue,
		datePickerValue,
		timeFieldFormValue,
		timeFieldValue,
	} from "@admin/fields/scalar/date-control-value";

	type ControlSize = "field" | "compact" | "toolbar";
	let {
		id,
		name,
		appearance = "date",
		value = "",
		timeZone,
		disabled = false,
		readonly = false,
		required = false,
		invalid = false,
		hasDescription = false,
		label,
		class: className,
		size = "field",
		onValueChange,
	}: {
		id: string;
		name?: string;
		appearance?: SchemaDateFormat;
		value?: string;
		timeZone?: string;
		disabled?: boolean;
		readonly?: boolean;
		required?: boolean;
		invalid?: boolean;
		hasDescription?: boolean;
		label?: string;
		class?: string;
		size?: ControlSize;
		onValueChange: (value: string) => void;
	} = $props();
	const i18n = getAdminI18n();

	const resolvedTimeZone = $derived(timeZone ?? i18n.timeZone);
	const selectedDate = $derived(datePickerValue(value, appearance, resolvedTimeZone));
	const controlARIA = $derived(fieldControlARIA(id, hasDescription, invalid));
	let open = $state(false);
	let anchor = $state<HTMLDivElement>();
	// Only the autofocus handler reads this DOM binding.
	// svelte-ignore non_reactive_update
	let popup: HTMLElement | null = null;

	function updateDate(next: DateValue | undefined) {
		if (disabled || readonly) return;
		onValueChange(datePickerFormValue(next, appearance, resolvedTimeZone));
	}

	function selectCalendarDate(next: DateValue | undefined) {
		updateDate(next);
		if (appearance !== "date-time") open = false;
	}

	function updateTime(next: Time | undefined) {
		if (disabled || readonly) return;
		onValueChange(timeFieldFormValue(next));
	}
</script>

{#if appearance === "time"}
	<TimeField.Root
		value={timeFieldValue(value)}
		locale={i18n.language}
		{disabled}
		{readonly}
		{required}
		errorMessageId={controlARIA["aria-errormessage"]}
		onValueChange={updateTime}
	>
		<TimeField.Input
			{id}
			{name}
			class={["ridu-date-control ridu-date-control--time", `ridu-date-control--${size}`, className]}
			data-disabled={disabled ? "" : undefined}
			data-readonly={readonly ? "" : undefined}
			{...controlARIA}
			aria-label={label}
		>
			{#snippet children({ segments })}
				{#each segments as { part, value: segmentValue }, index (`${part}:${index}`)}
					<TimeField.Segment {part} class="ridu-date-control__segment">
						{segmentValue}
					</TimeField.Segment>
				{/each}
			{/snippet}
		</TimeField.Input>
	</TimeField.Root>
{:else}
	<!-- Keep the timezone projection authoritative after the author edits individual segments. -->
	<DateField.Root
		bind:value={() => selectedDate, updateDate}
		locale={i18n.language}
		granularity={appearance === "date-time" ? "minute" : "day"}
		{disabled}
		{readonly}
		{required}
		errorMessageId={controlARIA["aria-errormessage"]}
	>
		<div
			bind:this={anchor}
			class={["ridu-date-control", `ridu-date-control--${size}`, className]}
			data-disabled={disabled ? "" : undefined}
			data-readonly={readonly ? "" : undefined}
			aria-invalid={controlARIA["aria-invalid"]}
		>
			<DateField.Input
				{id}
				{name}
				class="ridu-date-control__input"
				aria-label={label}
				{...controlARIA}
			>
				{#snippet children({ segments })}
					{#each segments as { part, value: segmentValue }, index (`${part}:${index}`)}
						<DateField.Segment
							{part}
							class="ridu-date-control__segment"
							onpointerdown={() => {
								// Release the popup's focus trap before the pointer focuses this segment.
								open = false;
							}}
						>
							{segmentValue}
						</DateField.Segment>
					{/each}
					{#if value && !disabled && !readonly}
						<button
							type="button"
							class="ridu-date-control__button"
							aria-label={i18n.t("general:clearSelection")}
							onclick={() => onValueChange("")}
						>
							<XIcon />
						</button>
					{/if}
					<Popover bind:open>
						<PopoverTrigger
							type="button"
							class="ridu-date-control__button"
							disabled={disabled || readonly}
							aria-label={i18n.t("fields:openCalendar")}
						>
							<CalendarIcon />
						</PopoverTrigger>
						<PopoverContent
							bind:ref={popup}
							onOpenAutoFocus={(event) => {
								event.preventDefault();
								popup?.querySelector<HTMLElement>('[data-calendar-day][tabindex="0"]')?.focus();
							}}
							customAnchor={anchor}
							align="start"
							sideOffset={6}
							class="ridu-date-popup"
						>
							{#key resolvedTimeZone}
								<DateValueCalendar
									timeZone={resolvedTimeZone}
									value={selectedDate}
									withTime={appearance === "date-time"}
									{disabled}
									{readonly}
									{required}
									locale={i18n.language}
									calendarLabel={i18n.t("fields:selectDate")}
									onValueChange={selectCalendarDate}
								/>
							{/key}
						</PopoverContent>
					</Popover>
				{/snippet}
			</DateField.Input>
		</div>
	</DateField.Root>
{/if}
