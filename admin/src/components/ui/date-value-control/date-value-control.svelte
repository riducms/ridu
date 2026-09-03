<script lang="ts">
	import type { DateValue, Time } from "@internationalized/date";
	import type { SchemaDatePickerAppearance } from "@riducms/protocol";
	import { DateField, TimeField } from "bits-ui";
	import CalendarIcon from "~icons/lucide/calendar";

	import { buttonVariants } from "@admin/components/ui/button";
	import DateValueCalendar from "@admin/components/ui/date-value-control/date-value-calendar.svelte";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import { cn } from "@riducms/ui";
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
		appearance = "dayOnly",
		value = "",
		disabled = false,
		readonly = false,
		required = false,
		invalid = false,
		label,
		describedBy,
		class: className,
		size = "field",
		onValueChange,
	}: {
		id: string;
		name?: string;
		appearance?: SchemaDatePickerAppearance;
		value?: string;
		disabled?: boolean;
		readonly?: boolean;
		required?: boolean;
		invalid?: boolean;
		label?: string;
		describedBy?: string;
		class?: string;
		size?: ControlSize;
		onValueChange: (value: string) => void;
	} = $props();
	const i18n = getAdminI18n();

	const controlClass = $derived(
		cn(
			"flex w-full items-center rounded-lg border border-control-border bg-control text-foreground-strong transition-[background-color,border-color,color] duration-150 hover:border-control-border-hover focus-within:border-primary/65 aria-invalid:border-destructive/65 data-[disabled]:cursor-not-allowed data-[disabled]:border-control-border-disabled data-[disabled]:bg-control-disabled data-[disabled]:opacity-45 data-[readonly]:border-control-border-disabled data-[readonly]:bg-control-disabled",
			size === "field" && "h-10.5 px-3.25 text-[14.5px]",
			size === "compact" && "h-9 px-3 text-[13px]",
			size === "toolbar" && "h-[34px] px-2.5 text-[12.5px]",
			className
		)
	);
	const selectedDate = $derived(datePickerValue(value, appearance, i18n.timeZone));
	let open = $state(false);

	function updateDate(next: DateValue | undefined) {
		onValueChange(datePickerFormValue(next, appearance, i18n.timeZone));
	}

	function selectCalendarDate(next: DateValue | undefined) {
		updateDate(next);
		open = false;
	}

	function updateTime(next: Time | undefined) {
		onValueChange(timeFieldFormValue(next));
	}
</script>

{#if appearance === "timeOnly"}
	<TimeField.Root
		value={timeFieldValue(value)}
		locale={i18n.language}
		{disabled}
		{readonly}
		{required}
		errorMessageId={invalid ? describedBy : undefined}
		onValueChange={updateTime}
	>
		<TimeField.Input
			{id}
			{name}
			class={cn(controlClass, "gap-0.5 select-none")}
			data-disabled={disabled ? "" : undefined}
			data-readonly={readonly ? "" : undefined}
			aria-invalid={invalid}
			aria-label={label}
			aria-describedby={describedBy}
			aria-errormessage={invalid ? describedBy : undefined}
		>
			{#snippet children({ segments })}
				{#each segments as { part, value: segmentValue }, index (`${part}:${index}`)}
					<TimeField.Segment
						{part}
						class={part === "literal"
							? "px-0.5 text-foreground-soft"
							: "rounded px-0.5 outline-none hover:bg-control-hover focus:bg-primary/15 aria-[valuetext=Empty]:text-foreground-placeholder"}
					>
						{segmentValue}
					</TimeField.Segment>
				{/each}
			{/snippet}
		</TimeField.Input>
	</TimeField.Root>
{:else}
	<DateField.Root
		value={selectedDate}
		locale={i18n.language}
		granularity={appearance === "dayAndTime" ? "minute" : "day"}
		{disabled}
		{readonly}
		{required}
		errorMessageId={invalid ? describedBy : undefined}
		onValueChange={updateDate}
	>
		<div
			class={controlClass}
			data-disabled={disabled ? "" : undefined}
			data-readonly={readonly ? "" : undefined}
			aria-invalid={invalid}
		>
			<DateField.Input
				{id}
				{name}
				class="flex min-w-0 flex-1 items-center gap-0.5 select-none"
				aria-label={label}
				aria-describedby={describedBy}
				aria-errormessage={invalid ? describedBy : undefined}
			>
				{#snippet children({ segments })}
					{#each segments as { part, value: segmentValue }, index (`${part}:${index}`)}
						<DateField.Segment
							{part}
							class={part === "literal"
								? "px-0.5 text-foreground-soft"
								: "rounded px-0.5 outline-none hover:bg-control-hover focus:bg-primary/15 aria-[valuetext=Empty]:text-foreground-placeholder"}
						>
							{segmentValue}
						</DateField.Segment>
					{/each}
					<Popover bind:open>
						<PopoverTrigger
							type="button"
							class={buttonVariants({
								variant: "ghost",
								size: "icon-sm",
								class: "ms-auto size-7 text-foreground-soft",
							})}
							disabled={disabled || readonly}
							aria-label={i18n.t("fields:openCalendar")}
						>
							<CalendarIcon class="size-3.5" aria-hidden="true" />
						</PopoverTrigger>
						<PopoverContent align="end" sideOffset={6} class="w-auto gap-0 overflow-hidden p-0">
							<DateValueCalendar
								value={selectedDate}
								{disabled}
								{readonly}
								{required}
								locale={i18n.language}
								calendarLabel={i18n.t("fields:selectDate")}
								onValueChange={selectCalendarDate}
							/>
						</PopoverContent>
					</Popover>
				{/snippet}
			</DateField.Input>
		</div>
	</DateField.Root>
{/if}
