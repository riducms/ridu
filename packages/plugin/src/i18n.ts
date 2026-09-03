import type {
	AdminI18n,
	PluginMessageCatalog,
	PluginTranslationKey,
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
	fallback: Fallback;
	translations?: Readonly<Record<string, ExactCatalog<Fallback>>>;
}

/** Defines one plugin-owned message catalog with exact translated key coverage. */
export function defineAdminMessages<const Input extends AdminMessagesShape>(
	input: Input & CheckedAdminMessages<Input>
): DefineAdminMessagesInput<Input["fallback"]> {
	validatePluginMessageCatalog("definition", input);
	return freezeAdminMessages(input) as DefineAdminMessagesInput<Input["fallback"]>;
}

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

const [getAdminI18n, setAdminI18n] = createContext<AdminI18n>();

export { getAdminI18n, setAdminI18n };
export type { AdminI18n, PluginMessageCatalog, PluginTranslationKey };
