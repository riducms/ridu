import type { AdminI18n } from "@riducms/plugin";

import { richTextMessages } from "#lib/messages.js";

type Catalog = typeof richTextMessages.fallback;
type Variables = Parameters<AdminI18n["t"]>[1];
type Message = string | Readonly<Partial<Record<Intl.LDMLPluralRule, string>> & { other: string }>;

/** One of the editor's messages, such as `"editor.placeholder"`. */
export type RichTextMessageKey = keyof Catalog;

/**
 * Text for some of the editor's messages, such as the app's own translations. Text keeps the
 * English message's `{placeholders}`; messages left out come from the built-in catalogue.
 */
export type RichTextMessages = Readonly<Partial<Record<RichTextMessageKey, string>>>;

interface RichTextI18nOptions {
	/**
	 * A language tag. Languages the editor translates use their catalogue; others, and tags Intl
	 * can't read such as "" or "en_US", use English.
	 */
	lang?: string | undefined;
	dir?: "ltr" | "rtl" | undefined;
	messages?: RichTextMessages | undefined;
}

const namespace = "plugin.richtext:";

/**
 * The translations the editor reads outside the admin, built from its own message catalogue so an
 * app doesn't need Ridu's admin translations.
 */
export function createRichTextI18n(options: RichTextI18nOptions = {}): AdminI18n {
	const locale = readLocale(options.lang);
	const language = locale.toString();
	const translations = richTextMessages.translations ?? {};
	const catalog: Readonly<Partial<Record<RichTextMessageKey, Message>>> =
		translations[language] ?? translations[locale.language] ?? {};
	const plurals = new Intl.PluralRules(language);
	const numbers = new Intl.NumberFormat(language);
	const messages = options.messages ?? {};

	return {
		language,
		direction: options.dir ?? "ltr",
		text: (canonical, texts) => texts?.[language] ?? canonical,
		t: (key, variables) => {
			const name = key.slice(namespace.length);
			if (!key.startsWith(namespace) || !isRichTextMessageKey(name))
				throw new Error(`The rich-text editor has no message ${key}.`);
			const message: Message = messages[name] ?? catalog[name] ?? richTextMessages.fallback[name];
			const text =
				typeof message === "string"
					? message
					: (message[plurals.select(Number(variables?.count))] ?? message.other);
			return text.replace(/\{(\w+)\}/g, (placeholder, variable: string) => {
				const value = variables?.[variable];
				if (value === undefined) return placeholder;
				return typeof value === "number" ? numbers.format(value) : value;
			});
		},
		formatDate: (value, format) =>
			new Intl.DateTimeFormat(language, format).format(new Date(value)),
		formatNumber: (value, format) => new Intl.NumberFormat(language, format).format(value),
		formatRelativeTime: (value, unit) =>
			new Intl.RelativeTimeFormat(language, { numeric: "auto" }).format(value, unit),
		formatList: (values, format) => new Intl.ListFormat(language, format).format(values),
	};
}

// HTML allows any lang value, but Intl throws for one it can't read, which would fail the server
// render.
function readLocale(tag: string | undefined) {
	if (tag === undefined || tag === "") return new Intl.Locale("en");
	try {
		return new Intl.Locale(tag);
	} catch (error) {
		if (error instanceof RangeError) return new Intl.Locale("en");
		throw error;
	}
}

function isRichTextMessageKey(name: string): name is RichTextMessageKey {
	return Object.hasOwn(richTextMessages.fallback, name);
}
