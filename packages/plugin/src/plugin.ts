import type { FieldType } from "@riducms/protocol";
import type { RegisteredPluginField } from "./field";
import { ADMIN_PLUGIN_API_VERSION } from "@riducms/protocol";
import type { PluginMessageCatalog, ExtensionTranslationKey } from "./i18n";
import { freezeAdminMessages, validateAdminMessages } from "./i18n";
import type { RowLabelPlugin } from "./row-label";
import type {
	AdminAccountComponent,
	AdminAccountSurface,
	AdminBrandComponent,
	AdminContributions,
	AdminCoreView,
	AdminDashboardPanel,
	AdminDocumentAction,
	AdminDocumentView,
	AdminListCellRenderer,
	AdminListResultsRenderer,
	AdminLoginComponent,
	AdminLogoutButton,
	AdminNavigationComponent,
	AdminProvider,
	AdminRoute,
	AdminShellSlot,
	ResolvedAdminExtensions,
} from "./admin-extensions";

export * from "./admin-extensions";

export { ADMIN_PLUGIN_API_VERSION };

/**
 * The browser UI supplied by an installed Go/admin plugin pair.
 * Create this with `defineAdminPlugin` from `@riducms/plugin/authoring/v1`,
 * which supplies the framework API version. Go declares the plugin's server
 * behaviour; this object connects its fields and other admin UI components.
 */
export interface AdminPlugin extends AdminContributions {
	/** Supplied by the versioned authoring import. Authors must not set this themselves. */
	apiVersion: typeof ADMIN_PLUGIN_API_VERSION;
	/** Owning Go plugin's key, for example `"editorial-tools"`; not necessarily a field-type key. */
	key: string;
	/** Positive integer matching the Go descriptor; bump it when the two halves become incompatible. */
	pairingVersion: number;
	/**
	 * Default editors for field types declared by this Go plugin. Each map key is a
	 * globally unique field-type name; each value comes from `definePluginField`.
	 * For example, one plugin may provide both `"review-note"` and `"color-swatch"`.
	 */
	fields?: Readonly<
		Record<string, RegisteredPluginField & { readonly type: "plugin"; readonly fieldType?: never }>
	>;
	/**
	 * Alternative editors made with `defineFieldComponent`, keyed by component name.
	 * Go chooses one with `.Admin(field.Admin{Editor: field.PluginComponent(pluginKey, componentName, config)})`.
	 * These may edit built-in fields or an explicitly named plugin field type.
	 */
	fieldEditors?: Readonly<
		Record<
			string,
			RegisteredPluginField &
				(
					| { readonly type: Exclude<FieldType, "plugin"> }
					| { readonly type: "plugin"; readonly fieldType: string }
				)
		>
	>;
	/** Array and blocks row-label components contributed by this package. */
	rowLabels?: readonly RowLabelPlugin[];
	/** Statically bundled, plugin-owned interface messages. */
	messages?: PluginMessageCatalog;
	/** Package-relative modules/styles to import at build time; must match the Go descriptor's list. */
	assets?: readonly string[];
}

/**
 * Go plugin metadata written into Ridu's generated admin registration file.
 * Do not hand-maintain a second copy: `ridu generate` resolves the executable Go
 * configuration and emits the matching imports and compatibility checks.
 */
export interface BackendAdminPlugin {
	/** Framework admin-plugin API required by the compiled backend. */
	apiVersion: number;
	/** Named AdminPlugin export in package. */
	export: string;
	/** Stable backend plugin key. */
	key: string;
	/** Installed bare JavaScript package specifier. */
	package: string;
	/** Plugin-owned backend/admin compatibility version. */
	pairingVersion: number;
	/** Exact ordered route paths declared by the compiled backend. */
	routes?: readonly string[];
	/** Exact ordered package-relative static assets declared by the backend. */
	assets?: readonly string[];
	/** Exact field-type keys declared by this backend plugin. */
	fieldTypes?: readonly string[];
}

/** One imported admin plugin and its generated Go metadata, compared before use. */
export interface AdminPluginPair {
	admin: AdminPlugin;
	backend: BackendAdminPlugin;
}

/**
 * Check that installed Go/admin plugin packages agree on identity, API version,
 * pairing version, routes, assets and field types. Ridu's generated file calls this;
 * authors should fix mismatched packages rather than editing generated metadata.
 * Missing imports fail during compilation; incompatible pairs throw here.
 */
export function assertAdminPluginPairs(pairs: readonly AdminPluginPair[]): void {
	const keys = new Set<string>();

	for (const { admin, backend } of pairs) {
		const identity = `${backend.package}#${backend.export}`;
		if (keys.has(backend.key))
			throw new Error(`Admin plugin ${backend.key} is paired more than once`);
		if (backend.apiVersion !== ADMIN_PLUGIN_API_VERSION) {
			throw new Error(
				`Backend plugin ${backend.key} requires admin plugin API ${backend.apiVersion}, but this admin supports ${ADMIN_PLUGIN_API_VERSION}`
			);
		}
		if (admin.apiVersion !== ADMIN_PLUGIN_API_VERSION) {
			throw new Error(
				`Admin plugin ${identity} uses API ${admin.apiVersion}, but this admin supports ${ADMIN_PLUGIN_API_VERSION}`
			);
		}
		if (admin.key !== backend.key) {
			throw new Error(
				`Admin plugin ${identity} declares key ${admin.key}, but the compiled backend expects ${backend.key}`
			);
		}
		if (admin.pairingVersion !== backend.pairingVersion) {
			throw new Error(
				`Admin plugin ${backend.key} pairing version ${admin.pairingVersion} does not match backend version ${backend.pairingVersion}; install matching plugin packages`
			);
		}
		assertSameValues(
			`${backend.key} routes`,
			backend.routes ?? [],
			(admin.routes ?? []).map((route) => route.path)
		);
		assertSameValues(`${backend.key} assets`, backend.assets ?? [], admin.assets ?? []);
		assertSameValues(
			`${backend.key} field types`,
			[...(backend.fieldTypes ?? [])].sort(),
			Object.keys(admin.fields ?? {}).sort()
		);
		keys.add(backend.key);
	}
}

/**
 * Collect plugin UI first, then application UI, preserving order and rejecting
 * duplicate identities or competing replacements. Used by Ridu's admin runtime;
 * application authors normally provide `defineAdmin` configuration instead.
 */
export function resolveAdminExtensions(
	plugins: readonly AdminPlugin[],
	application?: AdminContributions & { messages?: PluginMessageCatalog }
): ResolvedAdminExtensions {
	const routes: AdminRoute[] = [];
	const dashboardPanels: AdminDashboardPanel[] = [];
	const login: AdminLoginComponent[] = [];
	const account: AdminAccountComponent[] = [];
	const navigation: AdminNavigationComponent[] = [];
	const coreViews: AdminCoreView[] = [];
	const branding: AdminBrandComponent[] = [];
	const shellSlots: AdminShellSlot[] = [];
	const providers: AdminProvider[] = [];
	const listCellRenderers: AdminListCellRenderer[] = [];
	const listResultsRenderers: AdminListResultsRenderer[] = [];
	const documentActions: AdminDocumentAction[] = [];
	const documentViews: AdminDocumentView[] = [];
	const rowLabels: RowLabelPlugin[] = [];
	const messages: Record<string, PluginMessageCatalog> = Object.create(null) as Record<
		string,
		PluginMessageCatalog
	>;
	// These identities represent mount slots, not package-local names. Claiming one
	// set across every source prevents application UI from silently shadowing a plugin.
	const identities = new Set<string>();
	let replacementDashboard: string | undefined;
	let replacementLogin: string | undefined;
	const replacementAccounts = new Map<AdminAccountSurface, string>();
	let replacementNavigation: string | undefined;
	let logoutButton: AdminLogoutButton | undefined;

	// Preserve authored order within each source, with the application consistently
	// following installed plugins for additive surfaces and provider nesting.
	const sources = [
		...plugins.map((plugin) => ({ ...plugin, namespace: `plugin.${plugin.key}:` })),
		...(application === undefined
			? []
			: [{ ...application, key: "application", namespace: "app:", rowLabels: [] }]),
	];
	for (const plugin of sources) {
		validateContributions(plugin);
		for (const rowLabel of plugin.rowLabels ?? []) {
			if (rowLabel.key !== plugin.key) {
				throw new Error(
					`Admin plugin ${plugin.key} registered row label component for plugin ${rowLabel.key}`
				);
			}
			if (!adminComponentKeyPattern.test(rowLabel.componentKey)) {
				throw new Error(
					`Admin row label component ${rowLabel.key}:${rowLabel.componentKey} has an invalid component key`
				);
			}
			claim(
				identities,
				`row-label:${rowLabel.key}:${rowLabel.componentKey}`,
				`Admin row label component ${rowLabel.key}:${rowLabel.componentKey}`
			);
			rowLabels.push(rowLabel);
		}
		if (plugin.messages !== undefined && plugin.namespace !== "app:") {
			if (messages[plugin.key] !== undefined) {
				throw new Error(`Admin plugin messages for ${plugin.key} are registered more than once`);
			}
			validateAdminMessages(plugin.key, plugin.messages);
			messages[plugin.key] = freezeAdminMessages(plugin.messages);
		}
		for (const route of plugin.routes ?? []) {
			validatePluginLabelKey(plugin, route.navigation?.labelKey, `route ${route.path}`);
			// Treat differently cased paths as one identity rather than allowing
			// registrations whose reachability depends on a host or proxy's normalization.
			claim(identities, `route:${route.path.toLowerCase()}`, `Admin plugin route ${route.path}`);
			routes.push(route);
		}
		for (const panel of plugin.dashboardPanels ?? []) {
			claim(identities, `dashboard:${panel.key}`, `Admin dashboard panel ${panel.key}`);
			if (panel.position === "replace") {
				if (replacementDashboard !== undefined) {
					throw new Error(
						`Admin dashboard replacements ${replacementDashboard} and ${panel.key} are both registered`
					);
				}
				replacementDashboard = panel.key;
			}
			dashboardPanels.push(panel);
		}
		for (const component of plugin.login ?? []) {
			claim(identities, `login:${component.key}`, `Admin login component ${component.key}`);
			if (component.position === "replace") {
				if (replacementLogin !== undefined) {
					throw new Error(
						`Admin login replacements ${replacementLogin} and ${component.key} are both registered`
					);
				}
				replacementLogin = component.key;
			}
			login.push(component);
		}
		for (const component of plugin.account ?? []) {
			claim(
				identities,
				`account:${component.surface}:${component.key}`,
				`Admin account component ${component.key}`
			);
			if (component.position === "replace") {
				const replacement = replacementAccounts.get(component.surface);
				if (replacement !== undefined) {
					throw new Error(
						`Admin ${component.surface} account replacements ${replacement} and ${component.key} are both registered`
					);
				}
				replacementAccounts.set(component.surface, component.key);
			}
			account.push(component);
		}
		for (const component of plugin.navigation ?? []) {
			claim(
				identities,
				`navigation:${component.key}`,
				`Admin navigation component ${component.key}`
			);
			if (component.position === "replace") {
				if (replacementNavigation !== undefined) {
					throw new Error(
						`Admin navigation replacements ${replacementNavigation} and ${component.key} are both registered`
					);
				}
				replacementNavigation = component.key;
			}
			navigation.push(component);
		}
		if (plugin.logoutButton !== undefined) {
			if (logoutButton !== undefined) {
				throw new Error(
					`Admin logout buttons ${logoutButton.key} and ${plugin.logoutButton.key} are both registered`
				);
			}
			claim(
				identities,
				`logout-button:${plugin.logoutButton.key}`,
				`Admin logout button ${plugin.logoutButton.key}`
			);
			logoutButton = plugin.logoutButton;
		}
		for (const view of plugin.coreViews ?? []) {
			const resource =
				"collection" in view
					? (view.collection ?? "*")
					: "global" in view
						? (view.global ?? "*")
						: "*";
			claim(
				identities,
				`core-view:${view.surface}:${resource}`,
				`Admin core view ${view.surface}.${resource}`
			);
			coreViews.push(view);
		}
		for (const component of plugin.branding ?? []) {
			claim(
				identities,
				`branding:${component.surface}`,
				`Admin branding surface ${component.surface}`
			);
			branding.push(component);
		}
		for (const component of plugin.shellSlots ?? []) {
			claim(
				identities,
				`shell:${component.position}:${component.key}`,
				`Admin shell slot ${component.key}`
			);
			shellSlots.push(component);
		}
		for (const provider of plugin.providers ?? []) {
			claim(identities, `provider:${provider.key}`, `Admin provider ${provider.key}`);
			providers.push(provider);
		}
		for (const cell of plugin.listCellRenderers ?? []) {
			validatePluginLabelKey(plugin, cell.labelKey, `list cell ${cell.collection}.${cell.field}`);
			claim(
				identities,
				`list-cell:${cell.collection}:${cell.field}`,
				`Admin list-cell renderer ${cell.collection}.${cell.field}`
			);
			listCellRenderers.push(cell);
		}
		for (const results of plugin.listResultsRenderers ?? []) {
			claim(
				identities,
				`list-results:${results.collection}`,
				`Admin list-results renderer ${results.collection}`
			);
			listResultsRenderers.push(results);
		}
		for (const action of plugin.documentActions ?? []) {
			claim(
				identities,
				`document-action:${action.collection ?? "*"}:${action.key}`,
				`Admin document action ${action.key}`
			);
			documentActions.push(action);
		}
		for (const view of plugin.documentViews ?? []) {
			validatePluginLabelKey(plugin, view.labelKey, `document view ${view.key}`);
			if (view.key === "edit" || view.key === "api") {
				throw new Error(`Admin document view ${view.key} collides with a framework view`);
			}
			claim(
				identities,
				`document-view:${view.collection ?? "*"}:${view.key}`,
				`Admin document view ${view.key}`
			);
			documentViews.push(view);
		}
	}

	return {
		applicationMessages:
			application?.messages === undefined ? undefined : freezeAdminMessages(application.messages),
		rowLabels: Object.freeze(rowLabels),
		routes: Object.freeze(routes),
		dashboardPanels: Object.freeze(dashboardPanels),
		login: Object.freeze(login),
		account: Object.freeze(account),
		navigation: Object.freeze(navigation),
		logoutButton,
		coreViews: Object.freeze(coreViews),
		branding: Object.freeze(branding),
		shellSlots: Object.freeze(shellSlots),
		providers: Object.freeze(providers),
		listCellRenderers: Object.freeze(listCellRenderers),
		listResultsRenderers: Object.freeze(listResultsRenderers),
		documentActions: Object.freeze(documentActions),
		documentViews: Object.freeze(documentViews),
		messages: Object.freeze(messages),
	};
}

const adminComponentKeyPattern = /^[A-Za-z_$][A-Za-z0-9_$]*$/;

function validatePluginLabelKey(
	plugin: { key: string; namespace: string; messages?: PluginMessageCatalog },
	labelKey: ExtensionTranslationKey | undefined,
	surface: string
) {
	if (labelKey === undefined) return;
	const prefix = plugin.namespace;
	if (!labelKey.startsWith(prefix)) {
		throw new Error(
			`Admin plugin ${plugin.key} ${surface} label key must use its own ${prefix} namespace`
		);
	}
	const messageKey = labelKey.slice(prefix.length);
	if (plugin.messages?.fallback[messageKey] === undefined) {
		throw new Error(
			`Admin plugin ${plugin.key} ${surface} label key ${labelKey} is not defined in its messages`
		);
	}
}

function claim(identities: Set<string>, identity: string, label: string) {
	if (identities.has(identity)) throw new Error(`${label} is already registered`);
	identities.add(identity);
}

function assertSameValues(label: string, backend: readonly string[], admin: readonly string[]) {
	if (backend.length !== admin.length || backend.some((value, index) => value !== admin[index])) {
		throw new Error(`Admin plugin ${label} do not match the compiled backend declaration`);
	}
}

// Admin routes are literal paths: no router
// patterns, query strings, normalization aliases or framework namespace shadowing.
const reservedRouteRoots = new Set([
	"account",
	"collections",
	"globals",
	"login",
	"create-first-user",
	"forgot-password",
	"reset-password",
	"request-verification",
	"verify-email",
]);
function validateContributions(source: AdminContributions & { messages?: PluginMessageCatalog }) {
	if (source.messages !== undefined) validateAdminMessages("application", source.messages);
	const groups = [
		"routes",
		"dashboardPanels",
		"login",
		"account",
		"navigation",
		"coreViews",
		"branding",
		"shellSlots",
		"providers",
		"listCellRenderers",
		"listResultsRenderers",
		"documentActions",
		"documentViews",
	] as const;
	const positions = {
		dashboardPanels: ["before", "after", "replace"],
		login: ["before", "after", "replace"],
		account: ["before", "after", "replace"],
		navigation: ["before", "beforeLinks", "afterLinks", "after", "replace"],
		shellSlots: ["header", "actions", "settingsMenu"],
	} as const;
	for (const group of groups) {
		const entries = source[group];
		if (entries === undefined) continue;
		if (!Array.isArray(entries)) throw new Error(`Admin ${group} registrations must be an array.`);
		for (const entry of entries) {
			if (entry === null || typeof entry !== "object" || typeof entry.component !== "function")
				throw new Error(`Admin ${group} registration requires a Svelte component.`);
			if (
				"key" in entry &&
				(typeof entry.key !== "string" || !/^[A-Za-z][A-Za-z0-9_-]*$/.test(entry.key))
			)
				throw new Error(`Admin ${group} registration has an invalid key.`);
			if (group !== "routes" && !("key" in entry))
				throw new Error(`Admin ${group} registration requires a key.`);
			if (group in positions) {
				const allowed: readonly string[] = positions[group as keyof typeof positions];
				const position = "position" in entry ? entry.position : undefined;
				if (
					(position === undefined && (group === "navigation" || group === "shellSlots")) ||
					(position !== undefined && !allowed.includes(position))
				)
					throw new Error(`Admin ${group} registration has an invalid position.`);
			}
			if (group === "account" || group === "branding" || group === "coreViews") {
				const allowed =
					group === "account"
						? ["profile", "security"]
						: group === "branding"
							? ["loginLogo", "navigationLogo", "accountAvatar"]
							: ["collectionList", "collectionCreate", "collectionEdit", "global", "notFound"];
				if (!("surface" in entry) || !allowed.includes(entry.surface))
					throw new Error(`Admin ${group} registration has an invalid surface.`);
			}
		}
	}
	for (const route of source.routes ?? []) {
		if (
			typeof route.path !== "string" ||
			!/^[A-Za-z0-9_-]+(?:\/[A-Za-z0-9_-]+)*$/.test(route.path) ||
			reservedRouteRoots.has(route.path.split("/")[0]!.toLowerCase())
		)
			throw new Error(
				`Admin route ${route.path} must be a literal relative path outside framework namespaces.`
			);
	}
	if (
		source.logoutButton !== undefined &&
		(typeof source.logoutButton.component !== "function" || !source.logoutButton.key)
	)
		throw new Error("Admin logout button requires a key and Svelte component.");
	for (const action of source.documentActions ?? []) {
		if (
			action.requires !== undefined &&
			![
				"admin",
				"create",
				"read",
				"readVersions",
				"update",
				"delete",
				"duplicate",
				"publish",
				"unpublish",
				"restoreDeleted",
				"deletePermanent",
				"selectAll",
			].includes(action.requires)
		)
			throw new Error(`Admin document action ${action.key} has an invalid required operation.`);
	}
}
