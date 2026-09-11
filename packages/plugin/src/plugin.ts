import type { FieldType } from "@riducms/protocol";
import type { RegisteredPluginField } from "./field";
import { ADMIN_PLUGIN_API_VERSION } from "@riducms/protocol";
import type {
	OperationCapabilities,
	SchemaCollection,
	SchemaField,
	SchemaGlobal,
	SchemaManifest,
} from "@riducms/protocol";
import type { Component, Snippet } from "svelte";

import type { FieldDocument } from "./authoring";
import type { AdminI18n, PluginMessageCatalog, ExtensionTranslationKey } from "./i18n";
import { freezeAdminMessages, validateAdminMessages } from "./i18n";
import type { RowLabelPlugin } from "./row-label";

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
	components?: Readonly<
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
 * Places where a plugin or application can add admin UI. Arrays keep registration
 * order; application entries follow plugin entries. Replacement slots allow one
 * matching replacement. Conflicts are reported instead of silently overriding UI.
 */
export interface AdminContributions {
	/** Authenticated admin routes contributed by this package. */
	routes?: readonly AdminRoute[];
	/** Panels composed into or replacing the authenticated dashboard. */
	dashboard?: readonly AdminDashboardPanel[];
	/** Components composed around or replacing the sign-in screen. */
	login?: readonly AdminLoginComponent[];
	/** Components composed around or replacing the framework account screens. */
	account?: readonly AdminAccountComponent[];
	/** Components composed around or replacing the framework navigation. */
	navigation?: readonly AdminNavigationComponent[];
	/** Replaces the default sign-out control in the account menu. */
	logoutButton?: AdminLogoutButton;
	/** Wraps or replaces framework collection, global, and not-found views. */
	views?: readonly AdminCoreView[];
	/** Replaces framework-owned brand graphics at exact shell surfaces. */
	branding?: readonly AdminBrandComponent[];
	/** Adds global header, action, or account-settings menu components. */
	shell?: readonly AdminShellComponent[];
	/** Wraps the admin in statically ordered Svelte context providers. */
	providers?: readonly AdminProvider[];
	/** Collection list cells rendered for exact collection/field pairs. */
	listCells?: readonly AdminListCell[];
	/** Actions rendered alongside framework-owned document actions. */
	documentActions?: readonly AdminDocumentAction[];
	/** Read-only or operational views rendered beside Edit and API. */
	documentViews?: readonly AdminDocumentView[];
}

/** Common data supplied to admin extension components. Use the generated SDK for requests. */
export interface AdminExtensionProps {
	/** Resolved Go schema available to this admin session; it may omit inaccessible resources. */
	manifest: SchemaManifest;
	/** Signed-in user document, absent on screens where no user is signed in yet. */
	user?: FieldDocument;
	/** Current admin interface translations and formatting preferences. */
	i18n: AdminI18n;
}

/** Sign-in operations provided to a custom login screen. */
export interface AdminLoginExtensionHost {
	/** Sign in, check admin access and enter the admin. Rejects if authentication/access fails. */
	login: (credentials: { email: string; password: string }) => Promise<void>;
	/** Show a temporary success or error message. Does not perform an operation itself. */
	notify: (tone: AdminExtensionNotificationTone, title: string, message?: string) => void;
}

export interface AdminLoginComponentProps extends AdminExtensionProps {
	/** Render with `{@render defaultView()}` to keep Ridu's sign-in screen inside your wrapper. */
	defaultView: Snippet;
	host: AdminLoginExtensionHost;
}

/** Add content before/after sign-in, or replace it with a custom screen. */
export interface AdminLoginComponent {
	key: string;
	component: Component<AdminLoginComponentProps>;
	/** Defaults to after. Only one replace entry is allowed across plugins and the application. */
	position?: "before" | "after" | "replace";
}

export type AdminAccountSurface = "profile" | "security";

/** Operations for keeping a custom account screen in sync with the signed-in session. */
export interface AdminAccountExtensionHost {
	/** Reloads the signed-in document and updates the shell identity. */
	refreshUser: () => Promise<FieldDocument | undefined>;
	/** Ends the current session and returns to sign-in. */
	logout: () => Promise<void>;
	/** Show a temporary success or error message. */
	notify: (tone: AdminExtensionNotificationTone, title: string, message?: string) => void;
}

export interface AdminAccountComponentProps extends AdminExtensionProps {
	surface: AdminAccountSurface;
	/** Render with `{@render defaultView()}` to keep Ridu's account screen inside your wrapper. */
	defaultView: Snippet;
	host: AdminAccountExtensionHost;
}

/** Add to or replace the profile/security screen identified by `surface`. */
export interface AdminAccountComponent {
	key: string;
	surface: AdminAccountSurface;
	component: Component<AdminAccountComponentProps>;
	/** Defaults to after. Each account surface permits at most one replacement. */
	position?: "before" | "after" | "replace";
}

/** Choose the whole navigation's boundary, the resource-link list's boundary, or replace navigation. */
export type AdminNavigationPosition = "before" | "beforeLinks" | "afterLinks" | "after" | "replace";

export interface AdminNavigationComponentProps extends AdminExtensionProps {
	/** Render with `{@render defaultView()}` to include Ridu's navigation in a replacement. */
	defaultView: Snippet;
}

/** Insert a component at a navigation position. At most one entry can replace navigation. */
export interface AdminNavigationComponent {
	key: string;
	component: Component<AdminNavigationComponentProps>;
	position: AdminNavigationPosition;
}

export interface AdminLogoutExtensionHost {
	/** End the current session and return to sign-in. Await this instead of only changing local UI. */
	logout: () => Promise<void>;
}

export interface AdminLogoutButtonProps extends AdminExtensionProps {
	host: AdminLogoutExtensionHost;
}

/** Replace the account menu's sign-out button; call the supplied `host.logout()` on activation. */
export interface AdminLogoutButton {
	key: string;
	component: Component<AdminLogoutButtonProps>;
}

export type AdminCoreViewSurface =
	"collectionList" | "collectionCreate" | "collectionEdit" | "global" | "notFound";

/** Refresh tools for a custom collection/global screen. They do not save documents. */
export interface AdminCoreViewHost {
	/** Fetch the current Go schema again and update the admin's schema-dependent UI. */
	refreshManifest: () => Promise<void>;
	/** Notify Ridu that your code changed documents so dependent lists/lookups can refresh. */
	documentsChanged: () => void;
	/** Show a temporary success or error message. */
	notify: (tone: AdminExtensionNotificationTone, title: string, message?: string) => void;
}

export interface AdminCoreViewProps extends AdminExtensionProps {
	surface: AdminCoreViewSurface;
	collection?: SchemaCollection;
	global?: SchemaGlobal;
	documentID?: string;
	/** Render with `{@render defaultView()}` to wrap the normal screen rather than rebuilding it. */
	defaultView: Snippet;
	host: AdminCoreViewHost;
}

interface AdminCollectionCoreView {
	key: string;
	surface: "collectionList" | "collectionCreate" | "collectionEdit";
	/** Omit to replace this surface for every collection. */
	collection?: string;
	component: Component<AdminCoreViewProps>;
}

interface AdminGlobalCoreView {
	key: string;
	surface: "global";
	/** Omit to replace this surface for every global. */
	global?: string;
	component: Component<AdminCoreViewProps>;
}

interface AdminNotFoundCoreView {
	key: string;
	surface: "notFound";
	component: Component<AdminCoreViewProps>;
}

/**
 * Replace a collection/global/not-found screen. A resource-specific registration
 * wins over an all-resources fallback for that surface; duplicate targets fail.
 * The supplied `defaultView` lets your component wrap the existing screen.
 */
export type AdminCoreView = AdminCollectionCoreView | AdminGlobalCoreView | AdminNotFoundCoreView;

export type AdminBrandSurface = "loginLogo" | "navigationLogo" | "accountAvatar";

export interface AdminBrandComponentProps extends AdminExtensionProps {
	surface: AdminBrandSurface;
}

/** Replace one logo/avatar location. Only one component may claim each surface. */
export interface AdminBrandComponent {
	key: string;
	surface: AdminBrandSurface;
	component: Component<AdminBrandComponentProps>;
}

export type AdminShellPosition = "header" | "actions" | "settingsMenu";

export interface AdminShellComponentProps extends AdminExtensionProps {
	position: AdminShellPosition;
}

/** Add UI to the global header, actions area or account settings menu in registration order. */
export interface AdminShellComponent {
	key: string;
	position: AdminShellPosition;
	component: Component<AdminShellComponentProps>;
}

export interface AdminProviderProps {
	/** Render `{@render defaultView()}` after setting context so the nested providers/admin appear. */
	defaultView: Snippet;
	i18n: AdminI18n;
}

/** Wrap the admin in a Svelte context provider. Render the supplied `defaultView` to continue the UI. */
export interface AdminProvider {
	key: string;
	component: Component<AdminProviderProps>;
}

export interface AdminDashboardPanelProps {
	manifest: SchemaManifest;
	user?: FieldDocument;
	i18n: AdminI18n;
}

/** Add dashboard content before/after Ridu's overview, or replace that overview. */
export interface AdminDashboardPanel {
	/** Unique name among dashboard entries; duplicates throw. Array order controls display order. */
	key: string;
	component: Component<AdminDashboardPanelProps>;
	/** Defaults to after. Replace hides Ridu's overview and may only be registered once. */
	position?: "before" | "after" | "replace";
}

/** Data for displaying a collection table cell. This is not an editable document-form binding. */
export interface AdminListCellProps {
	collection: SchemaCollection;
	field: SchemaField;
	document: FieldDocument;
	/** This field's saved value. Check its type before rendering; changing it does not save a document. */
	value: unknown;
	i18n: AdminI18n;
}

/** Replace the cell display for one collection/field pair; duplicate targets are rejected. */
export interface AdminListCell {
	key: string;
	/** Collection slug, for example `"posts"`. */
	collection: string;
	/** Top-level field name/path in that collection, for example `"title"`. */
	field: string;
	/** Fallback column heading. */
	label: string;
	/** Optional translation key from this plugin's messages, or app:... for application entries. */
	labelKey?: ExtensionTranslationKey;
	component: Component<AdminListCellProps>;
}

export type AdminExtensionNotificationTone = "success" | "error";

/** Tools for a document action or extra document tab after it completes its own operation. */
export interface AdminDocumentExtensionHost {
	/** Reload the current document from the server. This is not a save of unsaved form edits. */
	refresh: () => Promise<void>;
	/** Show a temporary success or error message. */
	notify: (tone: AdminExtensionNotificationTone, title: string, message?: string) => void;
}

/** Saved document data for actions/tabs; use the generated SDK for application-specific operations. */
export interface AdminDocumentExtensionProps {
	collection: SchemaCollection;
	document: FieldDocument;
	host: AdminDocumentExtensionHost;
	i18n: AdminI18n;
}

/** Add a component alongside the document's standard actions. Implement the operation in that component. */
export interface AdminDocumentAction {
	key: string;
	/** Omit to show the action for every non-global collection. */
	collection?: string;
	/** Hide unless this document operation is allowed. Visibility does not replace server authorization. */
	requires?: keyof OperationCapabilities;
	component: Component<AdminDocumentExtensionProps>;
}

/** Add a tab beside Edit and API. Keys `edit` and `api` are reserved for Ridu. */
export interface AdminDocumentView {
	key: string;
	label: string;
	labelKey?: ExtensionTranslationKey;
	/** Collection or global slug; omit to show for every collection and global. */
	collection?: string;
	component: Component<AdminDocumentExtensionProps>;
}

export interface AdminRouteNavigation {
	/** Human-readable navigation label inside the authenticated admin shell. */
	label: string;
	labelKey?: ExtensionTranslationKey;
	/** Optional grouping hint reserved for shells that render grouped plugin navigation. */
	group?: string;
}

/** One statically bundled route mounted beneath the authenticated admin layout. */
export interface AdminRoute {
	/** Relative admin path, without a leading slash. Paired plugins must match their Go descriptor. */
	path: string;
	/** Svelte route component rendered by the framework router. */
	component: Component;
	/** Optional navigation entry; omit it for a route reachable only by links or redirects. */
	navigation?: AdminRouteNavigation;
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

/** Checked plugin registrations collected for Ridu's admin runtime. Normally consumed by generated code. */
export interface ResolvedAdminExtensions {
	rowLabels: readonly RowLabelPlugin[];
	routes: readonly AdminRoute[];
	dashboard: readonly AdminDashboardPanel[];
	login: readonly AdminLoginComponent[];
	account: readonly AdminAccountComponent[];
	navigation: readonly AdminNavigationComponent[];
	logoutButton: AdminLogoutButton | undefined;
	views: readonly AdminCoreView[];
	branding: readonly AdminBrandComponent[];
	shell: readonly AdminShellComponent[];
	providers: readonly AdminProvider[];
	listCells: readonly AdminListCell[];
	documentActions: readonly AdminDocumentAction[];
	documentViews: readonly AdminDocumentView[];
	messages: Readonly<Record<string, PluginMessageCatalog>>;
	applicationMessages: PluginMessageCatalog | undefined;
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
	const dashboard: AdminDashboardPanel[] = [];
	const login: AdminLoginComponent[] = [];
	const account: AdminAccountComponent[] = [];
	const navigation: AdminNavigationComponent[] = [];
	const views: AdminCoreView[] = [];
	const branding: AdminBrandComponent[] = [];
	const shell: AdminShellComponent[] = [];
	const providers: AdminProvider[] = [];
	const listCells: AdminListCell[] = [];
	const documentActions: AdminDocumentAction[] = [];
	const documentViews: AdminDocumentView[] = [];
	const rowLabels: RowLabelPlugin[] = [];
	const messages: Record<string, PluginMessageCatalog> = Object.create(null) as Record<
		string,
		PluginMessageCatalog
	>;
	const identities = new Set<string>();
	let replacementDashboard: string | undefined;
	let replacementLogin: string | undefined;
	const replacementAccounts = new Map<AdminAccountSurface, string>();
	let replacementNavigation: string | undefined;
	let logoutButton: AdminLogoutButton | undefined;

	const sources = [
		...plugins.map((plugin) => ({ ...plugin, namespace: `plugin.${plugin.key}:` })),
		...(application === undefined
			? []
			: [{ ...application, key: "application", namespace: "app:", rowLabels: [] }]),
	];
	for (const plugin of sources) {
		validateContributions(plugin, plugin.namespace === "app:");
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
			claim(identities, `route:${route.path.toLowerCase()}`, `Admin plugin route ${route.path}`);
			routes.push(route);
		}
		for (const panel of plugin.dashboard ?? []) {
			claim(identities, `dashboard:${panel.key}`, `Admin dashboard panel ${panel.key}`);
			if (panel.position === "replace") {
				if (replacementDashboard !== undefined) {
					throw new Error(
						`Admin dashboard replacements ${replacementDashboard} and ${panel.key} are both registered`
					);
				}
				replacementDashboard = panel.key;
			}
			dashboard.push(panel);
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
		for (const view of plugin.views ?? []) {
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
			views.push(view);
		}
		for (const component of plugin.branding ?? []) {
			claim(
				identities,
				`branding:${component.surface}`,
				`Admin branding surface ${component.surface}`
			);
			branding.push(component);
		}
		for (const component of plugin.shell ?? []) {
			claim(
				identities,
				`shell:${component.position}:${component.key}`,
				`Admin shell component ${component.key}`
			);
			shell.push(component);
		}
		for (const provider of plugin.providers ?? []) {
			claim(identities, `provider:${provider.key}`, `Admin provider ${provider.key}`);
			providers.push(provider);
		}
		for (const cell of plugin.listCells ?? []) {
			validatePluginLabelKey(plugin, cell.labelKey, `list cell ${cell.collection}.${cell.field}`);
			claim(
				identities,
				`list-cell:${cell.collection}:${cell.field}`,
				`Admin list cell ${cell.collection}.${cell.field}`
			);
			listCells.push(cell);
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
		dashboard: Object.freeze(dashboard),
		login: Object.freeze(login),
		account: Object.freeze(account),
		navigation: Object.freeze(navigation),
		logoutButton,
		views: Object.freeze(views),
		branding: Object.freeze(branding),
		shell: Object.freeze(shell),
		providers: Object.freeze(providers),
		listCells: Object.freeze(listCells),
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

// Application routes are literal paths: no router
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
function validateContributions(
	source: AdminContributions & { messages?: PluginMessageCatalog },
	local: boolean
) {
	if (source.messages !== undefined) validateAdminMessages("application", source.messages);
	const groups = [
		"routes",
		"dashboard",
		"login",
		"account",
		"navigation",
		"views",
		"branding",
		"shell",
		"providers",
		"listCells",
		"documentActions",
		"documentViews",
	] as const;
	const positions = {
		dashboard: ["before", "after", "replace"],
		login: ["before", "after", "replace"],
		account: ["before", "after", "replace"],
		navigation: ["before", "beforeLinks", "afterLinks", "after", "replace"],
		shell: ["header", "actions", "settingsMenu"],
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
					(position === undefined && (group === "navigation" || group === "shell")) ||
					(position !== undefined && !allowed.includes(position))
				)
					throw new Error(`Admin ${group} registration has an invalid position.`);
			}
			if (group === "account" || group === "branding" || group === "views") {
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
	for (const route of local ? (source.routes ?? []) : []) {
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
