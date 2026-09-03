import { describe, expect, test } from "bun:test";
import { createAdminI18n, en } from "@riducms/translations";

import {
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
		expect(formatDateDisplay("2026-09-15", "dayOnly", i18n)).toBe("Sep 15, 2026");
		expect(formatDateDisplay("09:30", "timeOnly", i18n)).toBe("09:30");
	});

	test("uses Bits values without introducing a timezone for day-only or time-only fields", () => {
		const day = datePickerValue("2026-09-15", "dayOnly");
		const time = timeFieldValue("09:30:00");

		expect(datePickerFormValue(day, "dayOnly")).toBe("2026-09-15");
		expect(timeFieldFormValue(time)).toBe("09:30");
	});

	test("round-trips a Bits date-time selection through RFC 3339", () => {
		const pickerValue = datePickerValue("2026-09-15T09:30:00.000Z", "dayAndTime", "Europe/Paris");

		expect(datePickerFormValue(pickerValue, "dayAndTime", "Europe/Paris")).toBe(
			"2026-09-15T09:30:00.000Z"
		);
	});
});
