import { describe, expect, test } from "bun:test";
import type { SchemaField, SchemaLocale } from "@riducms/protocol";

import {
	contentLocaleLabel,
	withContentLocaleLabel,
} from "@admin/fields/localized-field-presentation";

const locales: readonly SchemaLocale[] = [
	{ code: "en", label: "English" },
	{ code: "fr", label: "French", fallbackLocale: ["en"] },
];

const field: SchemaField = {
	id: "posts-title",
	name: "title",
	path: "title",
	type: "text",
	category: "scalar",
	required: true,
	unique: false,
	localized: true,
	admin: { label: "Title" },
};

describe("localized field presentation", () => {
	test("keeps the active content locale visible without mutating schema metadata", () => {
		const presented = withContentLocaleLabel(field, contentLocaleLabel(locales, "fr"));

		expect(presented.admin.label).toBe("Title — French");
		expect(field.admin.label).toBe("Title");
	});

	test("leaves non-localized fields unchanged", () => {
		const unlocalized = { ...field, localized: false };
		expect(withContentLocaleLabel(unlocalized, "French")).toBe(unlocalized);
	});

	test("falls back to the locale code when manifest display metadata is unavailable", () => {
		expect(contentLocaleLabel(locales, "ar")).toBe("ar");
		expect(contentLocaleLabel(locales, undefined)).toBeUndefined();
	});
});
