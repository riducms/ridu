import {
	resolveAdminPluginExtensions,
	type AdminDashboardPanel,
	type AdminLoginComponent,
	type AdminAccountComponent,
	type AdminNavigationComponent,
	type AdminLogoutButton,
	type AdminCoreView,
	type AdminBrandComponent,
	type AdminShellComponent,
	type AdminProvider,
	type AdminDocumentAction,
	type AdminDocumentView,
	type AdminListCell,
	type AdminPlugin,
	type AdminPluginRoute,
	type FieldPlugin,
} from "@riducms/plugin";
import type { AuthSession, OperationCapabilities, SchemaManifest } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import type { TranslationLanguage } from "@riducms/translations";
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
	manifest = $state<SchemaManifest>();
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
	themePreference = $state<ThemePreference>("system");
	resolvedTheme = $state<"light" | "dark">("dark");
	contentLocale = $state<string>();
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

	adoptContentLocale(locale: string) {
		if (!this.contentLocales.some((candidate) => candidate.code === locale)) return false;
		if (this.contentLocaleSwitchBlocked) return false;
		if (this.contentLocale === locale) return true;
		this.contentLocale = locale;
		void this.refreshAccess(locale).catch(() => {
			// Route adoption is best-effort; retain the last complete access snapshot on failure.
		});
		return true;
	}

	readonly pluginRoutes: readonly AdminPluginRoute[];
	readonly dashboardPanels: readonly AdminDashboardPanel[];
	readonly loginComponents: readonly AdminLoginComponent[];
	readonly accountComponents: readonly AdminAccountComponent[];
	readonly navigationComponents: readonly AdminNavigationComponent[];
	readonly logoutButton?: AdminLogoutButton;
	readonly coreViews: readonly AdminCoreView[];
	readonly brandComponents: readonly AdminBrandComponent[];
	readonly shellComponents: readonly AdminShellComponent[];
	readonly providers: readonly AdminProvider[];
	readonly listCells: readonly AdminListCell[];
	readonly documentActions: readonly AdminDocumentAction[];
	readonly documentViews: readonly AdminDocumentView[];
	readonly rowLabels: RowLabelRegistry;
	readonly i18n: AdminI18nController;

	constructor(
		readonly client: AdminClient,
		readonly plugins: readonly AdminPlugin[] = [],
		fieldPlugins: readonly FieldPlugin[] = [],
		languages?: readonly TranslationLanguage[],
		readonly fields: FieldRegistry = createCoreFieldRegistry([
			...plugins.flatMap((plugin) => plugin.fields),
			...fieldPlugins,
		])
	) {
		const extensions = resolveAdminPluginExtensions(plugins);
		this.pluginRoutes = extensions.routes;
		this.dashboardPanels = extensions.dashboard;
		this.loginComponents = extensions.login;
		this.accountComponents = extensions.account;
		this.navigationComponents = extensions.navigation;
		this.logoutButton = extensions.logoutButton;
		this.coreViews = extensions.views;
		this.brandComponents = extensions.branding;
		this.shellComponents = extensions.shell;
		this.providers = extensions.providers;
		this.listCells = extensions.listCells;
		this.documentActions = extensions.documentActions;
		this.documentViews = extensions.documentViews;
		this.rowLabels = createRowLabelRegistry(extensions.rowLabels);
		this.i18n = new AdminI18nController(client, () => this.session, languages, extensions.messages);
		this.#applyTheme();
		if (typeof window !== "undefined") {
			window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
				if (this.themePreference === "system") this.#applyTheme();
			});
		}
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

	async setContentLocale(locale: string) {
		if (!this.contentLocales.some((candidate) => candidate.code === locale)) return;
		if (this.contentLocaleSwitchBlocked) throw new ContentLocaleSwitchBlockedError();
		if (locale === this.contentLocale && locale === this.#confirmedContentLocale) return;
		const request = ++this.#contentLocaleRequest;
		const sessionID = this.session?.id;
		const ownerID = preferenceOwnerID(this.session);
		this.contentLocale = locale;
		const accessRefresh = this.refreshAccess(locale);
		void accessRefresh.catch(() => {
			// The operation awaits and reconciles this failure after its preference write settles.
		});
		try {
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
			if (request !== this.#contentLocaleRequest) return;
			if (this.session?.id !== sessionID || preferenceOwnerID(this.session) !== ownerID) {
				throw new StalePreferenceSessionError();
			}
			await accessRefresh;
			this.#confirmedContentLocale = locale;
			this.#confirmedContentLocaleOwnerID = ownerID;
		} catch (error) {
			if (request === this.#contentLocaleRequest) {
				this.contentLocale = this.#confirmedContentLocale;
				await this.refreshAccess(this.#confirmedContentLocale);
			}
			throw error;
		}
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

	async bootstrap() {
		const request = ++this.#manifestRequest;
		this.loading = true;
		this.error = undefined;
		try {
			const manifest = await this.client.schema();
			if (request !== this.#manifestRequest) return;
			this.manifest = manifest;
			this.manifestRevision += 1;
			this.#configureContentLocale();
			this.i18n.configure(manifest.application.adminLocalization);
			const admin = manifest.application.admin;
			if (admin !== undefined) {
				const bootstrap = await this.client.authBootstrap(admin.userCollectionSlug);
				if (request !== this.#manifestRequest) return;
				this.authBootstrapAvailable = bootstrap.available;
				if (bootstrap.available) {
					this.session = undefined;
				} else {
					try {
						const session = await this.client.session();
						if (request !== this.#manifestRequest) return;
						this.session = session.collection === admin.userCollectionSlug ? session : undefined;
					} catch (error) {
						if (request !== this.#manifestRequest) return;
						if (!(error instanceof RiduError) || error.code !== "access_denied") throw error;
						this.session = undefined;
					}
				}
			} else {
				this.session = undefined;
				this.authBootstrapAvailable = false;
			}
			await this.loadContentLocale(this.#browserRouteContentLocale());
			const accessAuthoritative = await this.refreshAccess(this.contentLocale);
			if (accessAuthoritative && this.session !== undefined && !this.adminAccessAllowed) {
				try {
					await this.client.logout();
				} catch {
					// The local session is still denied even if server cleanup fails.
				}
				this.session = undefined;
			}
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
			this.manifest = manifest;
			this.manifestRevision += 1;
			this.#configureContentLocale();
			this.i18n.configure(manifest.application.adminLocalization);
			const admin = manifest.application.admin;
			if (admin === undefined || this.session?.collection !== admin.userCollectionSlug) {
				this.session = undefined;
			}
			if (admin !== undefined && this.session === undefined) {
				const bootstrap = await this.client.authBootstrap(admin.userCollectionSlug);
				if (request !== this.#manifestRequest) return;
				this.authBootstrapAvailable = bootstrap.available;
			} else {
				this.authBootstrapAvailable = false;
			}
			await this.loadContentLocale(this.#browserRouteContentLocale());
			const accessAuthoritative = await this.refreshAccess(this.contentLocale);
			if (accessAuthoritative && this.session !== undefined && !this.adminAccessAllowed) {
				try {
					await this.client.logout();
				} catch {
					// The local session is still denied even if server cleanup fails.
				}
				this.session = undefined;
			}
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
