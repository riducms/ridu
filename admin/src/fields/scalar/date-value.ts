import type { SchemaDatePickerAppearance } from "@riducms/protocol";
import type { AdminI18n } from "@riducms/plugin";

export function formatDateDisplay(
	value: unknown,
	appearance: SchemaDatePickerAppearance | undefined,
	i18n: AdminI18n
) {
	const encoded = String(value ?? "");
	if (encoded === "") return "—";
	if (appearance === "timeOnly") return encoded.slice(0, 5);
	const date = new Date(
		appearance === "dayOnly" || appearance === undefined ? `${encoded}T00:00:00Z` : encoded
	);
	if (Number.isNaN(date.valueOf())) return encoded;
	if (appearance === "dayAndTime") {
		return i18n.formatDate(date, {
			dateStyle: "medium",
			timeStyle: "short",
		});
	}
	return i18n.formatDate(date, {
		dateStyle: "medium",
		timeZone: "UTC",
	});
}
