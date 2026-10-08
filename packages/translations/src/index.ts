export { ar, arMessages } from "./languages/ar.js";
export { en, enMessages } from "./languages/en.js";
export { fr, frMessages } from "./languages/fr.js";
export {
	createAdminI18n,
	defineTranslationLanguage,
	extendTranslationLanguage,
	parseAcceptLanguage,
	resolvePreferredLanguage,
	validatePluginMessageCatalog,
	validateLanguageCatalogs,
	type CreateAdminI18nOptions,
} from "./runtime.js";
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
} from "./types.js";
