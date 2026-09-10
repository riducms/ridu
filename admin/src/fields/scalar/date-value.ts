import type { SchemaDateFormat } from "@riducms/protocol";
import type { AdminI18n } from "@riducms/plugin";

export function formatDateDisplay(
	value: unknown,
	appearance: SchemaDateFormat | undefined,
	i18n: AdminI18n
) {
	const encoded = String(value ?? "");
	if (encoded === "") return "—";
	if (appearance === "time") return encoded.slice(0, 5);
	const date = new Date(
		appearance === "date" || appearance === undefined ? `${encoded}T00:00:00Z` : encoded
	);
	if (Number.isNaN(date.valueOf())) return encoded;
	if (appearance === "date-time") {
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
