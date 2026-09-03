import {
	getLocalTimeZone,
	parseAbsolute,
	parseDate,
	parseTime,
	toCalendarDate,
	toCalendarDateTime,
	type DateValue,
	type Time,
} from "@internationalized/date";
import type { SchemaDatePickerAppearance } from "@riducms/protocol";

/**
 * Converts a Ridu wire value into the timezone-free value used by Bits' date
 * segments. Day-only values deliberately never pass through `Date`.
 */
export function datePickerValue(
	value: unknown,
	appearance: SchemaDatePickerAppearance | undefined,
	timeZone = getLocalTimeZone()
): DateValue | undefined {
	const encoded = String(value ?? "");
	if (encoded === "") return undefined;
	try {
		if (appearance === "dayAndTime") {
			return toCalendarDateTime(parseAbsolute(encoded, timeZone));
		}
		return parseDate(encoded.slice(0, 10));
	} catch {
		return undefined;
	}
}

/** Converts a Bits date selection to Ridu's canonical wire value. */
export function datePickerFormValue(
	value: DateValue | undefined,
	appearance: SchemaDatePickerAppearance | undefined,
	timeZone = getLocalTimeZone()
) {
	if (value === undefined) return "";
	if (appearance === "dayAndTime") {
		return toCalendarDateTime(value).toDate(timeZone).toISOString();
	}
	return toCalendarDate(value).toString();
}

/** Time-only fields remain pure clock values; a timezone must never be introduced. */
export function timeFieldValue(value: unknown): Time | undefined {
	const encoded = String(value ?? "");
	if (encoded === "") return undefined;
	try {
		return parseTime(encoded);
	} catch {
		return undefined;
	}
}

export function timeFieldFormValue(value: Time | undefined) {
	return value?.toString().slice(0, 5) ?? "";
}
