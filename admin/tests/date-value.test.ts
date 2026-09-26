import { describe, expect, test } from "bun:test";
import { createAdminI18n, en } from "@riducms/translations";

import {
	dateTimeHasAmbiguousWallTime,
	datePickerFormValue,
	datePickerValue,
	timeFieldFormValue,
	timeFieldValue,
} from "@admin/fields/scalar/date-control-value";
import { formatDateDisplay } from "@admin/fields/scalar/date-value";

describe("date field presentation", () => {
	const i18n = createAdminI18n({
		languages: [en],
		language: "en",
		timeZone: "America/Los_Angeles",
	});

	test("keeps day-only and time-only values free of timezone conversion", () => {
		expect(formatDateDisplay("2026-09-15", "date", i18n)).toBe("Sep 15, 2026");
		expect(formatDateDisplay("09:30", "time", i18n)).toBe("09:30");
	});

	test("uses Bits values without introducing a timezone for day-only or time-only fields", () => {
		const day = datePickerValue("2026-09-15", "date");
		const time = timeFieldValue("09:30:00");

		expect(datePickerFormValue(day, "date")).toBe("2026-09-15");
		expect(timeFieldFormValue(time)).toBe("09:30");
	});

	test("round-trips a Bits date-time selection through RFC 3339", () => {
		const pickerValue = datePickerValue("2026-09-15T09:30:00.000Z", "date-time", "Europe/Paris");

		expect(datePickerFormValue(pickerValue, "date-time", "Europe/Paris")).toBe(
			"2026-09-15T09:30:00.000Z"
		);
	});
});

test.each([
	["2027-03-28T00:30:00.000Z", "Europe/London", "2027-03-28T00:30:00"],
	["2027-03-28T01:30:00.000Z", "Europe/London", "2027-03-28T02:30:00"],
	["2027-10-31T02:30:00.000Z", "Europe/London", "2027-10-31T02:30:00"],
	["2027-09-21T23:30:00.000Z", "Asia/Kolkata", "2027-09-22T05:00:00"],
	["2027-09-21T23:30:00.000Z", "+05:30", "2027-09-22T05:00:00"],
])("converts %s through %s", (instant, zone, wallTime) => {
	const value = datePickerValue(instant, "date-time", zone);
	expect(value?.toString()).toBe(wallTime);
	expect(datePickerFormValue(value, "date-time", zone)).toBe(instant);
});

test("detects both occurrences of a repeated daylight-saving wall time", () => {
	expect(dateTimeHasAmbiguousWallTime("2027-10-31T00:30:00.000Z", "Europe/London")).toBe(true);
	expect(dateTimeHasAmbiguousWallTime("2027-10-31T01:30:00.000Z", "Europe/London")).toBe(true);
	expect(dateTimeHasAmbiguousWallTime("2027-10-31T02:30:00.000Z", "Europe/London")).toBe(false);
});
