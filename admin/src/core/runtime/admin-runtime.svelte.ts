import {
	resolveAdminConfig,
	validateAdminManifest,
	type AdminConfig,
	type ResolvedAdminConfig,
} from "@riducms/plugin/admin";
import {
	bindSchemaManifest,
	type AdminPreparedNavigationV1,
	type AdminPreparedRuntimeV1,
	type AuthSession,
	type OperationCapabilities,
	type SchemaManifest,
} from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import { createContext } from "svelte";

import type { AdminClient, AdminDocument } from "@admin/core/api/admin-client";
import { createCoreFieldRegistry, type FieldRegistry } from "@admin/core/plugins/field-registry";
import {
	createRowLabelRegistry,
	type RowLabelRegistry,
} from "@admin/core/plugins/row-label-registry";
import {
	getPreferenceWriteQueue,
	preferenceOwnerID,
} from "@admin/core/preferences/preference-write-queue";
import { AdminI18nController } from "@admin/core/i18n/admin-i18n.svelte";
import { isHiddenAdminResource } from "@admin/core/schema/hidden-resource";

export type ThemePreference = "system" | "light" | "dark";

export class ContentLocaleSwitchBlockedError extends Error {
	constructor() {
		super("The content locale cannot be changed in the current admin state.");
	}
}

class StalePreferenceSessionError extends Error {
	constructor() {
		super("The signed-in session changed before the preference could be saved.");
	}
}

export class AdminRuntime {
	manifest = $state.raw<SchemaManifest>();
	manifestRevision = $state(0);
	documentRevision = $state(0);
	session = $state<AuthSession<AdminDocument>>();
	authBootstrapAvailable = $state(false);
	loading = $state(true);
	error = $state<string>();
	refreshingManifest = $state(false);
	manifestRefreshError = $state<string>();
	collectionOperations = $state.raw<Record<string, OperationCapabilities>>({});
	globalOperations = $state.raw<Record<string, OperationCapabilities>>({});
	preparedPreferences = $state.raw<Record<string, unknown>>({});
	themePreference = $state<ThemePreference>("system");
	resolvedTheme = $state<"light" | "dark">("dark");
	contentLocale = $state<string>();
	contentLocaleTransitioning = $state(false);
	#confirmedThemePreference: ThemePreference = "system";
	#confirmedThemeOwnerID = "";
	#confirmedContentLocale?: string;
	#confirmedContentLocaleOwnerID = "";
	#manifestRequest = 0;
	#accessRequest = 0;
	#themeRequest = 0;
	#contentLocaleRequest = 0;
	#contentLocaleBlockers = new Map<symbol, () => boolean>();
	#contentLocaleBlockerSequence = 0;
	#contentLocaleBlockerRevision = $state(0);
	#systemThemeQuery?: MediaQueryList;
	readonly #handleSystemThemeChange = () => {
		if (this.themePreference === "system") this.#applyTheme();
	};
	#preferenceReset?: {
		sessionID: string | undefined;
		ownerID: string;
		promise: Promise<void>;
	};

	get authCollection() {
		const admin = this.manifest?.application.admin;
		if (admin === undefined) return undefined;
		return this.manifest?.collections.find(
			(collection) =>
				collection.id === admin.userCollectionId &&
				collection.slug === admin.userCollectionSlug &&
				collection.capabilities.auth
		);
	}

	get authenticationRequired() {
		return this.manifest?.application.admin !== undefined;
	}

	get authenticated() {
		return !this.authenticationRequired || (this.session !== undefined && this.adminAccessAllowed);
	}

	get adminAccessAllowed() {
		if (!this.authenticationRequired) return true;
		const slug = this.authCollection?.slug;
		return slug !== undefined && this.collectionOperations[slug]?.admin === true;
	}

	get visibleCollections() {
		return (this.manifest?.collections ?? []).filter(
			(collection) => this.collectionOperations[collection.slug]?.read === true
		);
	}

	get visibleGlobals() {
		return (this.manifest?.globals ?? []).filter(
			(global) => this.globalOperations[global.slug]?.read === true
		);
	}

	// Navigable resources are the readable ones the config does not hide. A
	// hidden resource stays in the manifest, so relationships and pickers that
	// target it keep working; only the admin's own pages for it go away.
	get navigableCollections() {
		return this.visibleCollections.filter((collection) => collection.admin.hidden !== true);
	}

	get navigableGlobals() {
		return this.visibleGlobals.filter((global) => global.admin.hidden !== true);
	}

	isHiddenResource(kind: "collection" | "global", slug: string): boolean {
		return isHiddenAdminResource(this.manifest, kind, slug);
	}

	get contentLocales() {
		return this.manifest?.application.localization?.locales ?? [];
	}

	get contentLocaleSwitchBlocked() {
		this.#contentLocaleBlockerRevision;
		return [...this.#contentLocaleBlockers.values()].some((blocked) => blocked());
	}

	documentsChanged() {
		this.documentRevision += 1;
	}

	registerContentLocaleBlocker(blocked: () => boolean) {
		const owner = Symbol("content-locale-blocker");
		this.#contentLocaleBlockers.set(owner, blocked);
		this.#contentLocaleBlockerRevision = ++this.#contentLocaleBlockerSequence;
		return () => {
			if (!this.#contentLocaleBlockers.delete(owner)) return;
			this.#contentLocaleBlockerRevision = ++this.#contentLocaleBlockerSequence;
		};
	}

	readonly rowLabels: RowLabelRegistry;
	readonly i18n: AdminI18nController;

	readonly config: ResolvedAdminConfig;
	readonly fields: FieldRegistry;

	constructor(
		readonly client: AdminClient,
		config: AdminConfig = {},
		readonly adminBasePath = "/admin"
	) {
		this.config = resolveAdminConfig(config);
		this.fields = createCoreFieldRegistry(this.config.pluginFields);
		const extensions = this.config.extensions;
		this.rowLabels = createRowLabelRegistry(extensions.rowLabels, this.config.rowLabels);
		this.i18n = new AdminI18nController(
			client,
			() => this.session,
			this.config.languages,
			extensions.messages,
			extensions.applicationMessages
		);
		this.#applyTheme();
		if (typeof window !== "undefined") {
			this.#systemThemeQuery = window.matchMedia("(prefers-color-scheme: dark)");
			this.#systemThemeQuery.addEventListener("change", this.#handleSystemThemeChange);
		}
	}

	hardNavigate(path: string) {
		const base = `/${this.adminBasePath.split("/").filter(Boolean).join("/")}`;
		const route = path.startsWith("/") ? path : `/${path}`;
		window.location.assign(base === "/" ? route : `${base}${route}`);
		// Keep identity-changing handlers pending until the new document takes ownership.
		return new Promise<never>(() => {});
	}

	dispose() {
		this.#systemThemeQuery?.removeEventListener("change", this.#handleSystemThemeChange);
		this.#systemThemeQuery = undefined;
	}

	adoptPrepared(prepared: AdminPreparedRuntimeV1) {
		this.#manifestRequest += 1;
		this.error = undefined;
		this.loading = true;
		const manifest = bindSchemaManifest(prepared.manifest);
		this.#installManifest(manifest);
		this.session = prepared.session as AuthSession<AdminDocument> | undefined;
		this.authBootstrapAvailable = prepared.authBootstrapAvailable;
		this.collectionOperations = { ...prepared.collectionOperations };
		this.globalOperations = { ...prepared.globalOperations };
		this.preparedPreferences = { ...prepared.preferences };
		this.#confirmedThemePreference = isThemePreference(prepared.theme) ? prepared.theme : "system";
		this.#confirmedThemeOwnerID = preferenceOwnerID(this.session);
		this.themePreference = this.#confirmedThemePreference;
		this.#applyTheme();
		if (
			prepared.contentLocale !== undefined &&
			this.contentLocales.some((candidate) => candidate.code === prepared.contentLocale)
		) {
			this.contentLocale = prepared.contentLocale;
			this.#confirmedContentLocale = prepared.contentLocale;
			this.#confirmedContentLocaleOwnerID = preferenceOwnerID(this.session);
		}
		this.i18n.adoptPreparedPreferences(prepared.adminLanguage, prepared.adminTimeZone);
		this.loading = false;
	}

	adoptPreparedNavigation(prepared: AdminPreparedNavigationV1) {
		this.collectionOperations = { ...prepared.collectionOperations };
		this.globalOperations = { ...prepared.globalOperations };
		if (
			prepared.contentLocale !== undefined &&
			this.contentLocales.some((candidate) => candidate.code === prepared.contentLocale)
		) {
			this.contentLocale = prepared.contentLocale;
			this.#confirmedContentLocale = prepared.contentLocale;
			this.#confirmedContentLocaleOwnerID = preferenceOwnerID(this.session);
		}
		// The context key guarantees this is the same identity. Preferences are
		// adopted once from the cold document; a navigation response may have started
		// before a local theme/language/time-zone write and must not roll it back.
	}

	async loadTheme() {
		const request = ++this.#themeRequest;
		const sessionID = this.session?.id;
		const ownerID = preferenceOwnerID(this.session);
		let preference: unknown;
		let authoritative = this.session === undefined;
		if (this.session !== undefined) {
			try {
				preference = await this.client.preference<unknown>("theme");
				authoritative = true;
			} catch (cause) {
				if (cause instanceof RiduError && cause.code === "not_found") authoritative = true;
			}
		}
		if (request !== this.#themeRequest || this.session?.id !== sessionID) return;
		this.#confirmedThemePreference = authoritative
			? isThemePreference(preference)
				? preference
				: "system"
			: this.#confirmedThemeOwnerID === ownerID
				? this.#confirmedThemePreference
				: "system";
		this.#confirmedThemeOwnerID = ownerID;
		this.themePreference = this.#confirmedThemePreference;
		this.#applyTheme();
	}

	contentLocaleForPath(path: string) {
		return contentLocaleFromPath(path, this.contentLocales);
	}

	/** A supported URL locale wins; otherwise retain the session's selection or the default. */
	resolveContentLocale(requested: string | null | undefined) {
		return (
			this.contentLocales.find((locale) => locale.code === requested)?.code ??
			this.contentLocale ??
			this.manifest?.application.localization?.defaultLocale
		);
	}

	async loadContentLocale(activeLocale?: string) {
		const request = ++this.#contentLocaleRequest;
		const localization = this.manifest?.application.localization;
		if (localization === undefined) {
			this.contentLocale = undefined;
			this.#confirmedContentLocale = undefined;
			this.#confirmedContentLocaleOwnerID = "";
			return;
		}
		const sessionID = this.session?.id;
		const ownerID = preferenceOwnerID(this.session);
		let preference: unknown;
		let authoritative = this.session === undefined;
		if (this.session !== undefined) {
			try {
				preference = await this.client.preference<unknown>("content-locale");
				authoritative = true;
			} catch (cause) {
				if (cause instanceof RiduError && cause.code === "not_found") authoritative = true;
			}
		}
		if (request !== this.#contentLocaleRequest || this.session?.id !== sessionID) return;
		const preferred = contentLocalePreference(preference, localization.locales);
		this.#confirmedContentLocale = authoritative
			? (preferred ?? localization.defaultLocale)
			: this.#confirmedContentLocaleOwnerID === ownerID
				? (this.#confirmedContentLocale ?? localization.defaultLocale)
				: localization.defaultLocale;
		this.#confirmedContentLocaleOwnerID = ownerID;
		this.contentLocale = this.contentLocales.some((candidate) => candidate.code === activeLocale)
			? activeLocale
			: this.#confirmedContentLocale;
	}

	async persistContentLocalePreference(locale: string) {
		if (!this.contentLocales.some((candidate) => candidate.code === locale)) return;
		if (this.contentLocaleSwitchBlocked) throw new ContentLocaleSwitchBlockedError();
		const sessionID = this.session?.id;
		const ownerID = preferenceOwnerID(this.session);
		if (sessionID === undefined || ownerID === "") {
			await this.client.setPreference("content-locale", { locale });
		} else {
			const result = await getPreferenceWriteQueue().enqueue(
				JSON.stringify([ownerID, "content-locale"]),
				ownerID,
				() => this.session?.id === sessionID,
				() => this.client.setPreference("content-locale", { locale })
			);
			if (!result.dispatched) throw new StalePreferenceSessionError();
		}
		this.#confirmedContentLocale = locale;
		this.#confirmedContentLocaleOwnerID = ownerID;
	}

	async setTheme(preference: ThemePreference) {
		const themeRequest = ++this.#themeRequest;
		const sessionID = this.session?.id;
		const ownerID = preferenceOwnerID(this.session);
		const activeReset = this.#preferenceReset;
		if (activeReset !== undefined) {
			if (activeReset.sessionID === sessionID && activeReset.ownerID === ownerID) {
				try {
					await activeReset.promise;
				} catch (error) {
					await this.#reconcileTheme(themeRequest);
					throw error;
				}
			} else {
				try {
					await activeReset.promise;
				} catch {
					// A previous session's failed reset does not decide this session's theme.
				}
			}
		}
		if (this.session?.id !== sessionID || preferenceOwnerID(this.session) !== ownerID) {
			throw new StalePreferenceSessionError();
		}
		this.themePreference = preference;
		this.#applyTheme();
		try {
			if (sessionID === undefined || ownerID === "") {
				await this.client.setPreference("theme", preference);
			} else {
				const result = await getPreferenceWriteQueue().enqueue(
					JSON.stringify([ownerID, "theme"]),
					ownerID,
					() => this.session?.id === sessionID,
					() => this.client.setPreference("theme", preference)
				);
				if (!result.dispatched) {
					await this.#reconcileTheme(themeRequest);
					throw new StalePreferenceSessionError();
				}
			}
			if (this.session?.id !== sessionID || preferenceOwnerID(this.session) !== ownerID) {
				await this.#reconcileTheme(themeRequest);
				throw new StalePreferenceSessionError();
			}
			this.#confirmedThemePreference = preference;
			this.#confirmedThemeOwnerID = ownerID;
		} catch (error) {
			if (error instanceof StalePreferenceSessionError) throw error;
			if (this.session?.id !== sessionID || preferenceOwnerID(this.session) !== ownerID) {
				await this.#reconcileTheme(themeRequest);
				throw new StalePreferenceSessionError();
			}
			await this.#reconcileTheme(themeRequest);
			throw error;
		}
	}

	async #reconcileTheme(themeRequest: number) {
		if (themeRequest !== this.#themeRequest) return;
		await this.loadTheme();
	}

	async resetPreferences(): Promise<void> {
		const sessionID = this.session?.id;
		const ownerID = preferenceOwnerID(this.session);
		const activeReset = this.#preferenceReset;
		if (activeReset !== undefined) {
			if (activeReset.sessionID === sessionID && activeReset.ownerID === ownerID) {
				return activeReset.promise;
			}
			try {
				await activeReset.promise;
			} catch {
				// A previous session's failed reset does not decide this session's request.
			}
			if (this.session?.id !== sessionID || preferenceOwnerID(this.session) !== ownerID) {
				throw new Error("The signed-in session changed before preferences could be reset.");
			}
			return this.resetPreferences();
		}
		const themeRequest = ++this.#themeRequest;
		const reset = (async () => {
			try {
				if (ownerID !== "") await getPreferenceWriteQueue().settleOwner(ownerID);
				if (this.session?.id !== sessionID) {
					throw new Error("The signed-in session changed before preferences could be reset.");
				}
				await this.client.resetPreferences();
				if (ownerID !== "") getPreferenceWriteQueue().invalidateOwner(ownerID);
				if (this.session?.id === sessionID) {
					this.#confirmedThemePreference = "system";
					this.#confirmedThemeOwnerID = ownerID;
					this.themePreference = "system";
					this.#applyTheme();
					this.#resetContentLocale();
					await this.refreshAccess(this.contentLocale);
					this.i18n.reset();
				}
			} catch (error) {
				await this.#reconcileTheme(themeRequest);
				throw error;
			}
		})();
		this.#preferenceReset = { sessionID, ownerID, promise: reset };
		try {
			await reset;
		} finally {
			if (this.#preferenceReset?.promise === reset) this.#preferenceReset = undefined;
		}
	}

	#applyTheme() {
		const systemDark =
			typeof window === "undefined" || window.matchMedia("(prefers-color-scheme: dark)").matches;
		this.resolvedTheme =
			this.themePreference === "system" ? (systemDark ? "dark" : "light") : this.themePreference;
		if (typeof document !== "undefined") {
			document.documentElement.dataset.theme = this.resolvedTheme;
			document.documentElement.style.colorScheme = this.resolvedTheme;
		}
	}

	#installManifest(manifest: SchemaManifest) {
		validateAdminManifest(this.config, manifest);
		this.manifest = manifest;
		this.manifestRevision += 1;
		this.#configureContentLocale();
		this.i18n.configure(manifest.application.adminLocalization);
		return manifest.application.admin;
	}

	async #refreshLocalizedAccess() {
		await this.loadContentLocale(this.#browserRouteContentLocale());
		const accessAuthoritative = await this.refreshAccess(this.contentLocale);
		if (!accessAuthoritative || this.session === undefined || this.adminAccessAllowed) return;
		try {
			await this.client.auth.logout();
		} catch {
			// The local session is still denied even if server cleanup fails.
		}
		this.session = undefined;
	}

	async bootstrap() {
		const request = ++this.#manifestRequest;
		this.loading = true;
		this.error = undefined;
		try {
			const manifest = await this.client.schema();
			if (request !== this.#manifestRequest) return;
			const admin = this.#installManifest(manifest);
			if (admin !== undefined) {
				const bootstrap = await this.client.auth.bootstrap({
					collection: admin.userCollectionSlug,
				});
				if (request !== this.#manifestRequest) return;
				this.authBootstrapAvailable = bootstrap.available;
				if (bootstrap.available) {
					this.session = undefined;
				} else {
					// getSession resolves null without a session and rejects on an outage.
					const session = await this.client.auth.getSession();
					if (request !== this.#manifestRequest) return;
					this.session = session?.collection === admin.userCollectionSlug ? session : undefined;
				}
			} else {
				this.session = undefined;
				this.authBootstrapAvailable = false;
			}
			await this.#refreshLocalizedAccess();
			await Promise.all([this.loadTheme(), this.i18n.loadPreferences()]);
		} catch (error) {
			if (request !== this.#manifestRequest) return;
			this.error = error instanceof Error ? error.message : this.i18n.t("errors:loadAdmin");
		} finally {
			if (request === this.#manifestRequest) this.loading = false;
		}
	}

	async refreshManifest() {
		const request = ++this.#manifestRequest;
		this.refreshingManifest = true;
		this.manifestRefreshError = undefined;
		try {
			const manifest = await this.client.schema();
			if (request !== this.#manifestRequest) return;
			const admin = this.#installManifest(manifest);
			if (admin === undefined || this.session?.collection !== admin.userCollectionSlug) {
				this.session = undefined;
			}
			if (admin !== undefined && this.session === undefined) {
				const bootstrap = await this.client.auth.bootstrap({
					collection: admin.userCollectionSlug,
				});
				if (request !== this.#manifestRequest) return;
				this.authBootstrapAvailable = bootstrap.available;
			} else {
				this.authBootstrapAvailable = false;
			}
			await this.#refreshLocalizedAccess();
			await this.i18n.loadPreferences();
		} catch (error) {
			if (request !== this.#manifestRequest) return;
			this.manifestRefreshError =
				error instanceof Error ? error.message : this.i18n.t("errors:loadAdmin");
		} finally {
			if (request === this.#manifestRequest) this.refreshingManifest = false;
		}
	}

	async refreshAccess(locale = this.contentLocale): Promise<boolean> {
		const request = ++this.#accessRequest;
		const manifest = this.manifest;
		if (manifest === undefined) {
			this.collectionOperations = {};
			this.globalOperations = {};
			return true;
		}
		try {
			const [collections, globals] = await Promise.all([
				Promise.all(
					manifest.collections.map(async (collection) => {
						const access = await this.client.collectionAccess(collection.slug, { locale });
						return [collection.slug, access.operations] as const;
					})
				),
				Promise.all(
					(manifest.globals ?? []).map(async (global) => {
						const access = await this.client.globalAccess(global.slug, { locale });
						return [global.slug, access.operations] as const;
					})
				),
			]);
			if (request !== this.#accessRequest) return false;
			this.collectionOperations = Object.fromEntries(collections);
			this.globalOperations = Object.fromEntries(globals);
			return true;
		} catch (error) {
			if (request !== this.#accessRequest) return false;
			throw error;
		}
	}

	#configureContentLocale() {
		const localization = this.manifest?.application.localization;
		if (localization === undefined) {
			this.contentLocale = undefined;
			return;
		}
		if (!localization.locales.some((locale) => locale.code === this.contentLocale)) {
			this.contentLocale = localization.defaultLocale;
		}
	}

	#resetContentLocale() {
		this.#contentLocaleRequest += 1;
		const locale = this.manifest?.application.localization?.defaultLocale;
		this.#confirmedContentLocale = locale;
		this.#confirmedContentLocaleOwnerID = preferenceOwnerID(this.session);
		this.contentLocale = locale;
	}

	#browserRouteContentLocale() {
		if (typeof window === "undefined") return undefined;
		return this.contentLocaleForPath(
			`${window.location.pathname}${window.location.search}${window.location.hash}`
		);
	}
}

function isThemePreference(value: unknown): value is ThemePreference {
	return value === "system" || value === "light" || value === "dark";
}

function contentLocalePreference(
	value: unknown,
	locales: readonly { code: string }[]
): string | undefined {
	if (typeof value !== "object" || value === null) return undefined;
	const locale = (value as { locale?: unknown }).locale;
	return typeof locale === "string" && locales.some((candidate) => candidate.code === locale)
		? locale
		: undefined;
}

function contentLocaleFromPath(
	path: string,
	locales: readonly { code: string }[]
): string | undefined {
	try {
		const origin = "https://ridu.invalid";
		const current = new URL(path, origin);
		if (current.origin !== origin) return undefined;
		const direct = current.searchParams.get("locale");
		if (direct !== null && locales.some((candidate) => candidate.code === direct)) return direct;
		const redirect = current.searchParams.get("redirect");
		if (redirect === null) return undefined;
		const target = new URL(redirect, origin);
		if (target.origin !== origin) return undefined;
		const nested = target.searchParams.get("locale");
		return nested !== null && locales.some((candidate) => candidate.code === nested)
			? nested
			: undefined;
	} catch {
		return undefined;
	}
}

const [getAdminRuntime, setAdminRuntime] = createContext<AdminRuntime>();

export { getAdminRuntime, setAdminRuntime };
