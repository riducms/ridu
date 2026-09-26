import type { AdminI18n } from "@riducms/translations";

export function formatAdminDateTime(value: string, i18n: AdminI18n) {
	const date = new Date(value);
	if (!Number.isFinite(date.valueOf())) return value;
	if (i18n.language.startsWith("en")) {
		const day = Number(i18n.formatDate(date, { day: "numeric" }));
		const suffix =
			day % 100 >= 11 && day % 100 <= 13 ? "th" : ({ 1: "st", 2: "nd", 3: "rd" }[day % 10] ?? "th");
		return `${i18n.formatDate(date, { month: "long" })} ${day}${suffix} ${i18n.formatDate(date, { year: "numeric" })}, ${i18n.formatDate(date, { hour: "numeric", minute: "2-digit" })}`;
	}
	return i18n.formatDate(date, {
		year: "numeric",
		month: "long",
		day: "numeric",
		hour: "numeric",
		minute: "2-digit",
	});
}
