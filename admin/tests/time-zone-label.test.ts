import { expect, test } from "bun:test";
import { timeZoneLabel } from "@admin/core/i18n/time-zone-label";

const winter = new Date("2028-01-15T12:00:00Z");
const summer = new Date("2028-07-15T12:00:00Z");

function platformTimeZoneName(id: string, language: string, at: Date) {
	return new Intl.DateTimeFormat(language, { timeZone: id, timeZoneName: "long" })
		.formatToParts(at)
		.find((part) => part.type === "timeZoneName")!.value;
}

function expectNamedZone(
	value: string,
	options: { id: string; label: string; language: string; at: Date; offset: string }
) {
	const platformName = platformTimeZoneName(options.id, options.language, options.at);
	expect(value).toBe(`(${options.offset}) ${options.label} (${platformName})`);
}

test("describes configured locations and uses the offset at the scheduled instant", () => {
	expectNamedZone(timeZoneLabel("Europe/London", "London", "en", winter), {
		id: "Europe/London",
		label: "London",
		language: "en",
		at: winter,
		offset: "UTC+00:00",
	});
	expectNamedZone(timeZoneLabel("Europe/London", "London", "en", summer), {
		id: "Europe/London",
		label: "London",
		language: "en",
		at: summer,
		offset: "UTC+01:00",
	});
	expectNamedZone(timeZoneLabel("America/New_York", "Eastern Time (US & Canada)", "en", winter), {
		id: "America/New_York",
		label: "Eastern Time (US & Canada)",
		language: "en",
		at: winter,
		offset: "UTC-05:00",
	});
	expectNamedZone(timeZoneLabel("America/New_York", "New York", "en", summer), {
		id: "America/New_York",
		label: "New York",
		language: "en",
		at: summer,
		offset: "UTC-04:00",
	});
});

test("describes UTC and fractional offsets without duplicating a fixed-offset name", () => {
	const utcName = platformTimeZoneName("UTC", "en", winter);
	expect(timeZoneLabel("UTC", "UTC", "en", winter)).toBe(
		utcName === "UTC" ? "(UTC+00:00) UTC" : `(UTC+00:00) UTC (${utcName})`
	);
	expectNamedZone(timeZoneLabel("Asia/Kolkata", "India", "en", winter), {
		id: "Asia/Kolkata",
		label: "India",
		language: "en",
		at: winter,
		offset: "UTC+05:30",
	});
	expect(timeZoneLabel("+05:45", "Office", "en", winter)).toBe("(UTC+05:45) Office");
	expect(timeZoneLabel("+05:45", "Bureau", "fr", winter)).toBe("(UTC+05:45) Bureau");
	expect(timeZoneLabel("+05:45", "المكتب", "ar", winter)).toBe("(UTC+05:45) المكتب");
	expect(timeZoneLabel("UTC", utcName, "en", winter)).toBe(`(UTC+00:00) ${utcName}`);
});

test("preserves translated labels and localizes the timezone name", () => {
	expectNamedZone(timeZoneLabel("Europe/London", "Londres", "fr", summer), {
		id: "Europe/London",
		label: "Londres",
		language: "fr",
		at: summer,
		offset: "UTC+01:00",
	});
});
