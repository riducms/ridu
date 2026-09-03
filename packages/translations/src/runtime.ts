import { en } from "./languages/en";
import type {
	AdminI18n,
	AdminTranslationKey,
	CoreTranslationCatalog,
	PluginMessageCatalog,
	TranslationLanguage,
	TranslationMessage,
	TranslationVariables,
} from "./types";

export interface CreateAdminI18nOptions {
	languages?: readonly TranslationLanguage[];
	language?: string;
	fallbackLanguage?: string;
	timeZone?: string;
	pluginMessages?: Readonly<Record<string, PluginMessageCatalog>>;
}

/** Defines and validates a complete statically bundled admin language. */
export function defineTranslationLanguage<const Language extends TranslationLanguage>(
	language: Language
): Language {
	validateLanguageCatalogs([language]);
	return language;
}

/** Creates a complete catalog from a built-in language and typed project overrides. */
export function extendTranslationLanguage(
	base: TranslationLanguage,
	options: {
		code?: string;
		label?: string;
		rtl?: boolean;
		messages: Readonly<Partial<Record<keyof CoreTranslationCatalog, TranslationMessage>>>;
	}
): TranslationLanguage {
	const language: TranslationLanguage = {
		...base,
		...(options.code === undefined ? {} : { code: options.code }),
		...(options.label === undefined ? {} : { label: options.label }),
		...(options.rtl === undefined ? {} : { rtl: options.rtl }),
		messages: { ...base.messages, ...options.messages },
	};
	validateLanguageCatalogs([language]);
	return language;
}

export function createAdminI18n(options: CreateAdminI18nOptions = {}): AdminI18n {
	const languages = options.languages?.length ? options.languages : [en];
	validateLanguageCatalogs(languages);
	if (options.timeZone !== undefined) validateTimeZone(options.timeZone);
	const fallback = languageByCode(languages, options.fallbackLanguage) ?? languages[0];
	if (fallback === undefined) throw new Error("Admin translations require at least one language.");
	const active = languageByCode(languages, options.language) ?? fallback;
	const pluralRules = new Intl.PluralRules(active.code);
	const numberFormat = new Intl.NumberFormat(active.code);
	const plugins = options.pluginMessages ?? {};
	for (const [pluginKey, catalog] of Object.entries(plugins)) {
		validatePluginMessageCatalog(pluginKey, catalog);
	}
	return {
		language: active.code,
		direction: active.rtl === true ? "rtl" : "ltr",
		...(options.timeZone === undefined ? {} : { timeZone: options.timeZone }),
		text: (canonical, translations) =>
			translations?.[active.code] ?? translations?.[fallback.code] ?? canonical,
		t: (key, variables) => {
			const message = key.startsWith("plugin.")
				? pluginMessage(plugins, key, active.code, fallback.code)
				: (active.messages[key as keyof CoreTranslationCatalog] ??
					fallback.messages[key as keyof CoreTranslationCatalog]);
			if (message === undefined) throw new Error(`Missing admin translation ${key}.`);
			return interpolate(
				selectMessage(message, variables, pluralRules),
				variables,
				key,
				numberFormat
			);
		},
		formatDate: (value, formatOptions = {}) =>
			new Intl.DateTimeFormat(active.code, {
				...(hasExplicitDateFormat(formatOptions) ? {} : { dateStyle: "medium" as const }),
				...(options.timeZone === undefined ? {} : { timeZone: options.timeZone }),
				...formatOptions,
			}).format(normalizeDate(value)),
		formatNumber: (value, formatOptions) =>
			new Intl.NumberFormat(active.code, formatOptions).format(value),
		formatRelativeTime: (value, unit) =>
			new Intl.RelativeTimeFormat(active.code, { numeric: "auto" }).format(value, unit),
		formatList: (values, formatOptions) =>
			new Intl.ListFormat(active.code, formatOptions).format(values),
	};
}

function validateTimeZone(timeZone: string) {
	try {
		new Intl.DateTimeFormat("en", { timeZone }).format(0);
	} catch {
		throw new Error(`Invalid admin timezone ${timeZone}.`);
	}
}

export function resolvePreferredLanguage(
	preferences: readonly string[],
	supported: readonly string[],
	fallback: string
): string {
	const exact = new Map(supported.map((code) => [code.toLowerCase(), code]));
	for (const preference of preferences) {
		const candidates = preferredLanguageCandidates(preference);
		for (const candidate of candidates) {
			const match = exact.get(candidate.toLowerCase());
			if (match !== undefined) return match;
		}
		for (const candidate of candidates) {
			const prefix = `${candidate.toLowerCase()}-`;
			const match = supported.find((code) => code.toLowerCase().startsWith(prefix));
			if (match !== undefined) return match;
		}
	}
	return exact.get(fallback.toLowerCase()) ?? supported[0] ?? fallback;
}

function preferredLanguageCandidates(preference: string) {
	let canonical: string;
	try {
		canonical = canonicalLanguageCode(preference.trim());
	} catch {
		return [];
	}
	const parts = canonical.split("-");
	const candidates: string[] = [];
	while (parts.length > 0) {
		candidates.push(parts.join("-"));
		parts.pop();
		// RFC 4647 removes an extension singleton together with its extension.
		while (parts.at(-1)?.length === 1) parts.pop();
	}
	return candidates;
}

export function parseAcceptLanguage(header: string): string[] {
	return header
		.split(",")
		.map((part, index) => {
			const [code = "", ...parameters] = part.trim().split(";");
			const qualityParameter = parameters.find((parameter) => parameter.trim().startsWith("q="));
			const parsedQuality =
				qualityParameter === undefined ? 1 : Number(qualityParameter.trim().slice(2));
			const quality =
				Number.isFinite(parsedQuality) && parsedQuality >= 0 && parsedQuality <= 1
					? parsedQuality
					: 0;
			return { code, quality, index };
		})
		.filter(({ code, quality }) => code !== "*" && code !== "" && quality > 0)
		.sort((left, right) => right.quality - left.quality || left.index - right.index)
		.map(({ code }) => code);
}

export function validateLanguageCatalogs(languages: readonly TranslationLanguage[]): void {
	const expectedKeys = Object.keys(en.messages).sort();
	const seen = new Set<string>();
	for (const language of languages) {
		const canonicalCode = canonicalLanguageCode(language.code);
		if (canonicalCode !== language.code) {
			throw new Error(
				`Admin language ${language.code} must use canonical BCP-47 casing (${canonicalCode}).`
			);
		}
		if (language.label.trim() === "")
			throw new Error(`Admin language ${language.code} has an empty label.`);
		const identity = language.code.toLowerCase();
		if (seen.has(identity)) throw new Error(`Duplicate admin language ${language.code}.`);
		seen.add(identity);
		const actualKeys = Object.keys(language.messages).sort();
		if (actualKeys.join("\0") !== expectedKeys.join("\0")) {
			throw new Error(
				`Admin language ${language.code} does not exactly match the English catalog keys.`
			);
		}
		for (const key of expectedKeys) {
			const source = en.messages[key as keyof typeof en.messages];
			const translated = language.messages[key as keyof CoreTranslationCatalog];
			validateMessage(key, source, translated, language.code);
		}
	}
}

export function validatePluginMessageCatalog(
	pluginKey: string,
	catalog: PluginMessageCatalog
): void {
	if (!/^[a-z][a-z0-9-]*$/.test(pluginKey)) {
		throw new Error(`Invalid admin plugin message namespace ${pluginKey}.`);
	}
	const fallbackKeys = Object.keys(catalog.fallback).sort();
	if (fallbackKeys.length === 0) {
		throw new Error(`Admin plugin ${pluginKey} must define at least one fallback message.`);
	}
	for (const key of fallbackKeys) {
		if (!/^[A-Za-z][A-Za-z0-9._-]*$/.test(key) || key === "__proto__") {
			throw new Error(`Admin plugin ${pluginKey} has invalid message key ${key}.`);
		}
		validateMessage(
			`plugin.${pluginKey}:${key}`,
			catalog.fallback[key]!,
			catalog.fallback[key]!,
			"fallback"
		);
	}
	for (const [language, messages] of Object.entries(catalog.translations ?? {})) {
		const canonicalCode = canonicalLanguageCode(language);
		if (canonicalCode !== language) {
			throw new Error(
				`Admin plugin ${pluginKey} language ${language} must use canonical BCP-47 casing (${canonicalCode}).`
			);
		}
		const keys = Object.keys(messages).sort();
		if (keys.join("\0") !== fallbackKeys.join("\0")) {
			throw new Error(
				`Admin plugin ${pluginKey} language ${language} does not exactly match its fallback keys.`
			);
		}
		for (const key of fallbackKeys) {
			validateMessage(
				`plugin.${pluginKey}:${key}`,
				catalog.fallback[key]!,
				messages[key]!,
				language
			);
		}
	}
}

function canonicalLanguageCode(code: string) {
	try {
		const [canonical] = Intl.getCanonicalLocales(code);
		if (canonical === undefined) throw new RangeError();
		return canonical;
	} catch {
		throw new Error(`Invalid admin language code ${code}.`);
	}
}

function validateMessage(
	key: string,
	source: TranslationMessage,
	translated: TranslationMessage,
	language: string
) {
	validateMessageShape(key, source, language);
	validateMessageShape(key, translated, language);
	if ((typeof source === "string") !== (typeof translated === "string")) {
		throw new Error(`Admin language ${language} message ${key} changes plural message shape.`);
	}
	const sourcePlaceholders = placeholders(source);
	for (const text of messageVariants(translated)) {
		if (text.trim() === "")
			throw new Error(`Admin language ${language} has an empty ${key} message.`);
		const translatedPlaceholders = [...extractPlaceholders(text)].sort();
		if (translatedPlaceholders.join("\0") !== sourcePlaceholders.join("\0")) {
			throw new Error(`Admin language ${language} message ${key} has different placeholders.`);
		}
	}
}

const pluralCategories = new Set<Intl.LDMLPluralRule>([
	"zero",
	"one",
	"two",
	"few",
	"many",
	"other",
]);

function validateMessageShape(key: string, message: TranslationMessage, language: string) {
	if (typeof message === "string") return;
	if (message === null || typeof message !== "object" || Array.isArray(message)) {
		throw new Error(`Admin language ${language} message ${key} has an invalid message shape.`);
	}
	for (const [category, text] of Object.entries(message)) {
		if (!pluralCategories.has(category as Intl.LDMLPluralRule) || typeof text !== "string") {
			throw new Error(`Admin language ${language} message ${key} has an invalid plural category.`);
		}
	}
	if (typeof message.other !== "string") {
		throw new Error(`Admin language ${language} plural message ${key} requires other.`);
	}
}

function placeholders(message: TranslationMessage) {
	const result = new Set<string>();
	for (const text of messageVariants(message)) {
		for (const placeholder of extractPlaceholders(text)) result.add(placeholder);
	}
	return [...result].sort();
}

function messageVariants(message: TranslationMessage): string[] {
	return typeof message === "string"
		? [message]
		: Object.values(message).filter((value): value is string => typeof value === "string");
}

function extractPlaceholders(message: string): Set<string> {
	return new Set([...message.matchAll(/\{([A-Za-z][A-Za-z0-9_]*)\}/g)].map((match) => match[1]!));
}

function selectMessage(
	message: TranslationMessage,
	variables: TranslationVariables | undefined,
	pluralRules: Intl.PluralRules
) {
	if (typeof message === "string") return message;
	const count = variables?.count;
	if (typeof count !== "number")
		throw new Error("Plural admin translations require a numeric count.");
	return message[pluralRules.select(count)] ?? message.other;
}

function interpolate(
	message: string,
	variables: TranslationVariables | undefined,
	key: AdminTranslationKey,
	numberFormat: Intl.NumberFormat
) {
	return message.replace(/\{([A-Za-z][A-Za-z0-9_]*)\}/g, (_, name: string) => {
		const value = variables?.[name];
		if (value === undefined) throw new Error(`Admin translation ${key} requires ${name}.`);
		return typeof value === "number" ? numberFormat.format(value) : value;
	});
}

function normalizeDate(value: Date | string | number) {
	const date = value instanceof Date ? value : new Date(value);
	if (Number.isNaN(date.valueOf())) throw new RangeError("Cannot format an invalid date.");
	return date;
}

function hasExplicitDateFormat(options: Intl.DateTimeFormatOptions) {
	return [
		"dateStyle",
		"timeStyle",
		"weekday",
		"era",
		"year",
		"month",
		"day",
		"dayPeriod",
		"hour",
		"minute",
		"second",
		"fractionalSecondDigits",
		"timeZoneName",
	].some((key) => key in options);
}

function languageByCode(languages: readonly TranslationLanguage[], code: string | undefined) {
	if (code === undefined) return undefined;
	return languages.find((language) => language.code.toLowerCase() === code.toLowerCase());
}

function pluginMessage(
	plugins: Readonly<Record<string, PluginMessageCatalog>>,
	key: string,
	language: string,
	fallbackLanguage: string
): TranslationMessage | undefined {
	const separator = key.indexOf(":");
	const namespace = key.slice("plugin.".length, separator);
	const localKey = key.slice(separator + 1);
	const catalog = plugins[namespace];
	return (
		catalog?.translations?.[language]?.[localKey] ??
		catalog?.translations?.[fallbackLanguage]?.[localKey] ??
		catalog?.fallback[localKey]
	);
}
