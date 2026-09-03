import type { PluginMessageCatalog, TranslationLanguage } from "@riducms/translations";
import {
	createAdminI18n,
	en,
	resolvePreferredLanguage,
	type AdminI18n,
	type AdminTranslationKey,
	type TranslationVariables,
} from "@riducms/translations";
import type {
	AuthSession,
	SchemaAdminLanguage,
	SchemaAdminLocalizationSettings,
} from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";

import type { AdminClient } from "@admin/core/api/admin-client";
import {
	getPreferenceWriteQueue,
	preferenceOwnerID,
} from "@admin/core/preferences/preference-write-queue";
import type { AdminDocument } from "@admin/core/api/admin-client";

const languagePreferenceKey = "admin-language";
const timeZonePreferenceKey = "admin-timezone";
const browserLanguageKey = "ridu:admin-language";
const browserTimeZoneKey = "ridu:admin-timezone";

export class AdminI18nController implements AdminI18n {
	#language = $state("en");
	#timeZone = $state<string>();
	#settings = $state.raw<SchemaAdminLocalizationSettings>();
	#catalogs: readonly TranslationLanguage[];
	#catalogByCode: ReadonlyMap<string, TranslationLanguage>;
	#messages: Readonly<Record<string, PluginMessageCatalog>>;
	#translatorCache: AdminI18n | undefined;
	#translatorRevision = $state(0);
	#preferenceRequest = 0;
	#languageWrite = 0;
	#timeZoneWrite = 0;
	#confirmedLanguage = "en";
	#confirmedTimeZone: string | undefined;

	constructor(
		private readonly client: AdminClient,
		private readonly session: () => AuthSession<AdminDocument> | undefined,
		catalogs: readonly TranslationLanguage[] = [en],
		messages: Readonly<Record<string, PluginMessageCatalog>> = {}
	) {
		this.#catalogs = catalogs.length === 0 ? [en] : [...catalogs];
		this.#catalogByCode = new Map(this.#catalogs.map((catalog) => [catalog.code, catalog]));
		this.#messages = messages;
		const stored = readBrowserPreference(browserLanguageKey);
		this.#language = resolvePreferredLanguage(
			[
				...(stored === undefined ? [] : [stored]),
				...(typeof navigator === "undefined" ? [] : navigator.languages),
			],
			this.#catalogs.map((catalog) => catalog.code),
			this.#catalogByCode.has("en") ? "en" : (this.#catalogs[0]?.code ?? "en")
		);
		this.#confirmedLanguage = this.#language;
		this.#applyDocumentLanguage();
	}

	get language() {
		return this.#language;
	}

	get direction(): "ltr" | "rtl" {
		return (this.languages.find((language) => language.code === this.#language)?.rtl ??
			this.#catalogByCode.get(this.#language)?.rtl) === true
			? "rtl"
			: "ltr";
	}

	get timeZone() {
		return this.#timeZone;
	}

	get languages(): readonly SchemaAdminLanguage[] {
		return (
			this.#settings?.languages ??
			this.#catalogs.map(({ code, label, rtl }) => ({ code, label, ...(rtl ? { rtl } : {}) }))
		);
	}

	get timeZones() {
		return this.#settings?.timeZones ?? [];
	}

	get #translator() {
		// Components may first call t() after a translator was prepared during bootstrap. Reading an
		// explicit revision keeps those calls reactive even when the cached translator itself does not
		// need to read #language or #timeZone again.
		void this.#translatorRevision;
		return (this.#translatorCache ??= createAdminI18n({
			languages: this.#configuredCatalogsFor(this.languages),
			language: this.#language,
			fallbackLanguage: this.#fallbackLanguage,
			...(this.#timeZone === undefined ? {} : { timeZone: this.#timeZone }),
			pluginMessages: this.#messages,
		}));
	}

	get #fallbackLanguage() {
		return (
			this.#settings?.defaultLanguage ??
			(this.#catalogByCode.has("en") ? "en" : (this.#catalogs[0]?.code ?? "en"))
		);
	}

	#configuredCatalogsFor(languages: readonly SchemaAdminLanguage[]) {
		return languages.map((language) => {
			const catalog = this.#catalogByCode.get(language.code);
			if (catalog === undefined) {
				throw new Error(
					`Admin language ${language.code} is configured in Go but no matching static catalog was passed to mountAdmin.`
				);
			}
			if ((catalog.rtl === true) !== (language.rtl === true)) {
				throw new Error(
					`Admin language ${language.code} has conflicting RTL settings in Go and its static catalog.`
				);
			}
			return { ...catalog, label: language.label, rtl: language.rtl === true };
		});
	}

	text = (canonical: string, translations?: Readonly<Record<string, string>>) =>
		this.#translator.text(canonical, translations);

	t = (key: AdminTranslationKey, variables?: TranslationVariables) =>
		this.#translator.t(key, variables);

	formatDate = (value: Date | string | number, options?: Intl.DateTimeFormatOptions) =>
		this.#translator.formatDate(value, options);

	formatNumber = (value: number, options?: Intl.NumberFormatOptions) =>
		this.#translator.formatNumber(value, options);

	formatRelativeTime = (value: number, unit: Intl.RelativeTimeFormatUnit) =>
		this.#translator.formatRelativeTime(value, unit);

	formatList = (values: readonly string[], options?: Intl.ListFormatOptions) =>
		this.#translator.formatList(values, options);

	configure(settings: SchemaAdminLocalizationSettings | undefined) {
		const languages =
			settings?.languages ??
			this.#catalogs.map(({ code, label, rtl }) => ({ code, label, ...(rtl ? { rtl } : {}) }));
		const fallbackLanguage =
			settings?.defaultLanguage ??
			(this.#catalogByCode.has("en") ? "en" : (this.#catalogs[0]?.code ?? "en"));
		const supported = languages.map((language) => language.code);
		const stored = readBrowserPreference(browserLanguageKey);
		const preferences = [
			...(stored === undefined ? [] : [stored]),
			...(typeof navigator === "undefined" ? [] : navigator.languages),
		];
		const language = resolvePreferredLanguage(preferences, supported, fallbackLanguage);
		const browserTimeZone = readBrowserPreference(browserTimeZoneKey) ?? detectedTimeZone();
		const timeZone = this.#resolvedTimeZone(browserTimeZone, settings);
		const translator = createAdminI18n({
			languages: this.#configuredCatalogsFor(languages),
			language,
			fallbackLanguage,
			...(timeZone === undefined ? {} : { timeZone }),
			pluginMessages: this.#messages,
		});

		// Commit only after every catalog, direction, locale, and timezone validates.
		this.#settings = settings;
		this.#language = language;
		this.#timeZone = timeZone;
		this.#confirmedLanguage = language;
		this.#confirmedTimeZone = timeZone;
		this.#translatorCache = translator;
		this.#translatorRevision += 1;
		this.#languageWrite += 1;
		this.#timeZoneWrite += 1;
		this.#applyDocumentLanguage();
	}

	async loadPreferences() {
		const request = ++this.#preferenceRequest;
		this.#languageWrite += 1;
		this.#timeZoneWrite += 1;
		const sessionID = this.session()?.id;
		if (sessionID === undefined) return;
		const [language, timeZone] = await Promise.all([
			this.#readPreference(languagePreferenceKey),
			this.#readPreference(timeZonePreferenceKey),
		]);
		if (request !== this.#preferenceRequest || this.session()?.id !== sessionID) return;
		if (typeof language === "string" && this.languages.some((item) => item.code === language)) {
			this.#language = language;
			this.#confirmedLanguage = language;
			writeBrowserPreference(browserLanguageKey, language);
		}
		if (typeof timeZone === "string" && this.timeZones.some((item) => item.id === timeZone)) {
			this.#timeZone = timeZone;
			this.#confirmedTimeZone = timeZone;
			writeBrowserPreference(browserTimeZoneKey, timeZone);
		}
		this.#invalidateTranslator();
		this.#applyDocumentLanguage();
	}

	async setLanguage(language: string) {
		if (!this.languages.some((item) => item.code === language)) {
			throw new Error(`Admin language ${language} is not configured.`);
		}
		this.#preferenceRequest += 1;
		const write = ++this.#languageWrite;
		const sessionID = this.session()?.id;
		const ownerID = preferenceOwnerID(this.session());
		this.#language = language;
		this.#invalidateTranslator();
		this.#applyDocumentLanguage();
		writeBrowserPreference(browserLanguageKey, language);
		try {
			await this.#writePreference(languagePreferenceKey, language);
			if (this.session()?.id !== sessionID || preferenceOwnerID(this.session()) !== ownerID) {
				throw new Error("The signed-in session changed before the preference was saved.");
			}
			this.#confirmedLanguage = language;
		} catch (error) {
			if (
				write === this.#languageWrite &&
				this.session()?.id === sessionID &&
				preferenceOwnerID(this.session()) === ownerID
			) {
				this.#language = this.#confirmedLanguage;
				this.#invalidateTranslator();
				this.#applyDocumentLanguage();
				writeBrowserPreference(browserLanguageKey, this.#confirmedLanguage);
			}
			throw error;
		}
	}

	async setTimeZone(timeZone: string) {
		if (!this.timeZones.some((item) => item.id === timeZone)) {
			throw new Error(`Admin timezone ${timeZone} is not configured.`);
		}
		this.#preferenceRequest += 1;
		const write = ++this.#timeZoneWrite;
		const sessionID = this.session()?.id;
		const ownerID = preferenceOwnerID(this.session());
		this.#timeZone = timeZone;
		this.#invalidateTranslator();
		writeBrowserPreference(browserTimeZoneKey, timeZone);
		try {
			await this.#writePreference(timeZonePreferenceKey, timeZone);
			if (this.session()?.id !== sessionID || preferenceOwnerID(this.session()) !== ownerID) {
				throw new Error("The signed-in session changed before the preference was saved.");
			}
			this.#confirmedTimeZone = timeZone;
		} catch (error) {
			if (
				write === this.#timeZoneWrite &&
				this.session()?.id === sessionID &&
				preferenceOwnerID(this.session()) === ownerID
			) {
				this.#timeZone = this.#confirmedTimeZone;
				this.#invalidateTranslator();
				if (this.#confirmedTimeZone === undefined) removeBrowserPreference(browserTimeZoneKey);
				else writeBrowserPreference(browserTimeZoneKey, this.#confirmedTimeZone);
			}
			throw error;
		}
	}

	reset() {
		this.#preferenceRequest += 1;
		this.#languageWrite += 1;
		this.#timeZoneWrite += 1;
		removeBrowserPreference(browserLanguageKey);
		removeBrowserPreference(browserTimeZoneKey);
		this.configure(this.#settings);
	}

	async #readPreference(key: string): Promise<unknown> {
		try {
			return await this.client.preference<unknown>(key);
		} catch (error) {
			if (error instanceof RiduError && error.code === "not_found") return undefined;
			return undefined;
		}
	}

	async #writePreference(key: string, value: string) {
		const owner = preferenceOwnerID(this.session());
		if (owner === "") {
			await this.client.setPreference(key, value);
			return;
		}
		const sessionID = this.session()?.id;
		const result = await getPreferenceWriteQueue().enqueue(
			JSON.stringify([owner, key]),
			owner,
			() => this.session()?.id === sessionID,
			() => this.client.setPreference(key, value)
		);
		if (!result.dispatched)
			throw new Error("The signed-in session changed before the preference was saved.");
	}

	#resolvedTimeZone(
		candidate: string | undefined,
		settings: SchemaAdminLocalizationSettings | undefined = this.#settings
	) {
		const configured = settings?.timeZones ?? [];
		if (configured.length === 0) return undefined;
		if (candidate !== undefined && configured.some((item) => item.id === candidate))
			return candidate;
		return settings?.defaultTimeZone ?? configured[0]?.id;
	}

	#invalidateTranslator() {
		this.#translatorCache = undefined;
		this.#translatorRevision += 1;
	}

	#applyDocumentLanguage() {
		if (typeof document === "undefined") return;
		document.documentElement.lang = this.#language;
		document.documentElement.dir = this.direction;
		document.documentElement.dataset.adminLanguage = this.#language;
	}
}

function readBrowserPreference(key: string) {
	try {
		return globalThis.localStorage?.getItem(key) ?? undefined;
	} catch {
		return undefined;
	}
}

function writeBrowserPreference(key: string, value: string) {
	try {
		globalThis.localStorage?.setItem(key, value);
	} catch {
		// Browser persistence is an enhancement; the server preference remains authoritative.
	}
}

function removeBrowserPreference(key: string) {
	try {
		globalThis.localStorage?.removeItem(key);
	} catch {
		// Browser persistence is an enhancement; the server preference remains authoritative.
	}
}

function detectedTimeZone() {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone;
	} catch {
		return undefined;
	}
}
