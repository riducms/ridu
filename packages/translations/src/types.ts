import type { enMessages } from "./languages/en";

export type PluralCategory = Intl.LDMLPluralRule;
export type PluralMessage = Readonly<Partial<Record<PluralCategory, string>> & { other: string }>;
export type TranslationMessage = string | PluralMessage;
export type CoreTranslationKey = keyof typeof enMessages;
export type CoreTranslationCatalog = Readonly<Record<CoreTranslationKey, TranslationMessage>>;
export type TranslationVariables = Readonly<Record<string, string | number>>;
export type PluginTranslationKey = `plugin.${string}:${string}`;
export type AdminTranslationKey = CoreTranslationKey | PluginTranslationKey;

export interface TranslationLanguage {
	code: string;
	label: string;
	rtl?: boolean;
	messages: CoreTranslationCatalog;
}

export interface AdminI18n {
	readonly language: string;
	readonly direction: "ltr" | "rtl";
	readonly timeZone?: string;
	text: (canonical: string, translations?: Readonly<Record<string, string>>) => string;
	t: (key: AdminTranslationKey, variables?: TranslationVariables) => string;
	formatDate: (value: Date | string | number, options?: Intl.DateTimeFormatOptions) => string;
	formatNumber: (value: number, options?: Intl.NumberFormatOptions) => string;
	formatRelativeTime: (value: number, unit: Intl.RelativeTimeFormatUnit) => string;
	formatList: (values: readonly string[], options?: Intl.ListFormatOptions) => string;
}

export interface PluginMessageCatalog {
	fallback: Readonly<Record<string, TranslationMessage>>;
	translations?: Readonly<Record<string, Readonly<Record<string, TranslationMessage>>>>;
}
