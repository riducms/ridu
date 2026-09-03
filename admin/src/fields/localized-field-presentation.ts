import type { SchemaField, SchemaLocale } from "@riducms/protocol";

export function contentLocaleLabel(locales: readonly SchemaLocale[], code: string | undefined) {
	if (code === undefined) return undefined;
	return locales.find((locale) => locale.code === code)?.label ?? code;
}

export function withContentLocaleLabel(field: SchemaField, localeLabel: string | undefined) {
	if (field.localized !== true || localeLabel === undefined) return field;
	return {
		...field,
		admin: {
			...field.admin,
			label: `${field.admin.label} — ${localeLabel}`,
		},
	};
}
