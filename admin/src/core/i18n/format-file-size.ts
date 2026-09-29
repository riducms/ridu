import type { AdminI18n } from "@riducms/translations";

/** Formats a byte count in the largest fitting unit, such as "87 bytes", "95 kB" or "1.2 MB". */
export function formatFileSize(bytes: number, i18n: AdminI18n) {
	if (bytes < 1_024)
		return i18n.formatNumber(bytes, { style: "unit", unit: "byte", unitDisplay: "long" });
	if (bytes < 1_048_576)
		return i18n.formatNumber(bytes / 1_024, {
			maximumFractionDigits: 0,
			style: "unit",
			unit: "kilobyte",
		});
	return i18n.formatNumber(bytes / 1_048_576, {
		maximumFractionDigits: 1,
		style: "unit",
		unit: "megabyte",
	});
}
