export { ar, arMessages } from "./languages/ar";
export { en, enMessages } from "./languages/en";
export { fr, frMessages } from "./languages/fr";
export {
	createAdminI18n,
	defineTranslationLanguage,
	extendTranslationLanguage,
	parseAcceptLanguage,
	resolvePreferredLanguage,
	validatePluginMessageCatalog,
	validateLanguageCatalogs,
	type CreateAdminI18nOptions,
} from "./runtime";
export type {
	AdminI18n,
	AdminTranslationKey,
	CoreTranslationCatalog,
	CoreTranslationKey,
	PluginMessageCatalog,
	PluginTranslationKey,
	ApplicationTranslationKey,
	ExtensionTranslationKey,
	PluralCategory,
	PluralMessage,
	TranslationLanguage,
	TranslationMessage,
	TranslationVariables,
} from "./types";
