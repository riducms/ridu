import type {
	AdminI18n,
	PluginMessageCatalog,
	PluginTranslationKey,
	ExtensionTranslationKey,
	TranslationMessage,
} from "@riducms/translations";
import { validatePluginMessageCatalog } from "@riducms/translations";
import { createContext } from "svelte";

type ExactCatalog<Source extends Readonly<Record<string, TranslationMessage>>> = Readonly<{
	[Key in keyof Source]: TranslationMessage;
}>;

type MessageText<Message> = Message extends string
	? Message
	: Message extends Readonly<Record<string, unknown>>
		? Extract<Message[keyof Message], string>
		: never;
type PlaceholderNames<Text extends string> = Text extends `${string}{${infer Name}}${infer Rest}`
	? Name | PlaceholderNames<Rest>
	: never;
type SamePlaceholders<Source, Candidate> = [
	Exclude<PlaceholderNames<MessageText<Source>>, PlaceholderNames<MessageText<Candidate>>>,
] extends [never]
	? [
			Exclude<PlaceholderNames<MessageText<Candidate>>, PlaceholderNames<MessageText<Source>>>,
		] extends [never]
		? true
		: false
	: false;
type CompatibleMessage<Source, Candidate> =
	SamePlaceholders<Source, Candidate> extends true
		? Source extends string
			? Candidate extends string
				? Candidate
				: never
			: Candidate extends string
				? never
				: Candidate
		: never;
type CheckedCatalog<
	Source extends Readonly<Record<string, TranslationMessage>>,
	Candidate extends ExactCatalog<Source>,
> = Readonly<{
	[Key in keyof Source]: CompatibleMessage<Source[Key], Candidate[Key]>;
}> &
	Readonly<Record<Exclude<keyof Candidate, keyof Source>, never>>;
type AdminMessagesShape = Readonly<{
	fallback: Readonly<Record<string, TranslationMessage>>;
	translations?: Readonly<Record<string, Readonly<Record<string, TranslationMessage>>>>;
}>;
type CheckedAdminMessages<Input extends AdminMessagesShape> =
	Input["translations"] extends Readonly<
		Record<string, Readonly<Record<string, TranslationMessage>>>
	>
		? {
				translations: {
					[
						Language in keyof Input["translations"]
					]: Input["translations"][Language] extends ExactCatalog<Input["fallback"]>
						? CheckedCatalog<Input["fallback"], Input["translations"][Language]>
						: never;
				};
			}
		: unknown;

export interface DefineAdminMessagesInput<
	Fallback extends Readonly<Record<string, TranslationMessage>>,
> {
	/** Default text for every message key. Keys here do not include the plugin/app namespace. */
	fallback: Fallback;
	/** Catalogs by language code; each supplied language must translate every fallback key. */
	translations?: Readonly<Record<string, ExactCatalog<Fallback>>>;
}

/**
 * Define interface text for a plugin or application. Every supplied translation
 * must have the fallback catalog's keys, message kinds and `{placeholder}` names.
 * The helper checks and freezes the catalogs; literals also receive TypeScript checks.
 *
 * For plugin key `"notes"`, fallback key `"copy"` is read as
 * `i18n.t("plugin.notes:copy")`. In `defineAdmin({ messages })`, it is `"app:copy"`.
 * These are admin interface translations, not translations of saved content.
 */
export function defineAdminMessages<const Input extends AdminMessagesShape>(
	input: Input & CheckedAdminMessages<Input>
): DefineAdminMessagesInput<Input["fallback"]> {
	validatePluginMessageCatalog("definition", input);
	return freezeAdminMessages(input) as DefineAdminMessagesInput<Input["fallback"]>;
}

/** Check a catalog at runtime, throwing with its owner key on invalid messages/translations. */
export function validateAdminMessages(key: string, messages: PluginMessageCatalog) {
	validatePluginMessageCatalog(key, messages);
}

export function freezeAdminMessages(messages: PluginMessageCatalog): PluginMessageCatalog {
	if (Object.isFrozen(messages)) return messages;
	Object.freeze(messages.fallback);
	for (const catalog of Object.values(messages.translations ?? {})) Object.freeze(catalog);
	if (messages.translations !== undefined) Object.freeze(messages.translations);
	return Object.freeze(messages);
}

// Separate named exports let TypeScript show their docs at call sites; these remain the same functions.
const [readAdminI18n, provideAdminI18n] = createContext<AdminI18n>();

/**
 * Read Ridu's translation context during Svelte component setup. Field/extension
 * components already receive `i18n` in props; nested components can use this getter.
 * Throws outside a provider, such as a standalone component test without context.
 */
export const getAdminI18n = readAdminI18n;

/**
 * Provide translations to child components during Svelte component setup.
 * Ridu normally does this. Use it when hosting components independently, for
 * example in a test that needs to supply its own AdminI18n instance.
 */
export const setAdminI18n = provideAdminI18n;
export type { AdminI18n, PluginMessageCatalog, PluginTranslationKey, ExtensionTranslationKey };
