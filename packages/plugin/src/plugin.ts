import type { FieldPlugin } from "./field";
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
import type { AdminI18n, PluginMessageCatalog, PluginTranslationKey } from "./i18n";
import { freezeAdminMessages, validateAdminMessages } from "./i18n";
import type { RowLabelPlugin } from "./row-label";

export { ADMIN_PLUGIN_API_VERSION };

/**
 * Describes the admin half exported by a statically installed plugin package.
 * Pairing versions are plugin-owned and must change when its Go and admin
 * halves are no longer mutually compatible.
 */
export interface AdminPlugin {
	/** Framework admin-plugin contract version compiled by this package. */
	apiVersion: typeof ADMIN_PLUGIN_API_VERSION;
	/** Stable key matching the compiled Go plugin. */
	key: string;
	/** Plugin-owned backend/admin compatibility version. */
	pairingVersion: number;
	/** Field renderers contributed by this package. */
	fields: readonly FieldPlugin[];
	/** Array and blocks row-label components contributed by this package. */
	rowLabels?: readonly RowLabelPlugin[];
	/** Statically bundled, plugin-owned interface messages. */
	messages?: PluginMessageCatalog;
	/** Authenticated admin routes contributed by this package. */
	routes?: readonly AdminPluginRoute[];
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
	/** Package-relative static modules imported by the generated admin registry. */
	assets?: readonly string[];
}

export interface AdminExtensionProps {
	manifest: SchemaManifest;
	user?: FieldDocument;
	i18n: AdminI18n;
}

export interface AdminLoginExtensionHost {
	/** Authenticates, evaluates admin access, and enters the authenticated shell. */
	login: (credentials: { email: string; password: string }) => Promise<void>;
	notify: (tone: AdminExtensionNotificationTone, title: string, message?: string) => void;
}

export interface AdminLoginComponentProps extends AdminExtensionProps {
	/** The complete framework sign-in screen, available to replacement wrappers. */
	defaultView: Snippet;
	host: AdminLoginExtensionHost;
}

export interface AdminLoginComponent {
	key: string;
	component: Component<AdminLoginComponentProps>;
	position?: "before" | "after" | "replace";
}

export type AdminAccountSurface = "profile" | "security";

export interface AdminAccountExtensionHost {
	/** Reloads the signed-in document and updates the shell identity. */
	refreshUser: () => Promise<FieldDocument | undefined>;
	/** Ends the current session and returns to sign-in. */
	logout: () => Promise<void>;
	notify: (tone: AdminExtensionNotificationTone, title: string, message?: string) => void;
}

export interface AdminAccountComponentProps extends AdminExtensionProps {
	surface: AdminAccountSurface;
	/** The complete framework account screen, available to replacement wrappers. */
	defaultView: Snippet;
	host: AdminAccountExtensionHost;
}

export interface AdminAccountComponent {
	key: string;
	surface: AdminAccountSurface;
	component: Component<AdminAccountComponentProps>;
	position?: "before" | "after" | "replace";
}

export type AdminNavigationPosition = "before" | "beforeLinks" | "afterLinks" | "after" | "replace";

export interface AdminNavigationComponentProps extends AdminExtensionProps {
	/** The complete framework navigation, available to replacement wrappers. */
	defaultView: Snippet;
}

export interface AdminNavigationComponent {
	key: string;
	component: Component<AdminNavigationComponentProps>;
	position: AdminNavigationPosition;
}

export interface AdminLogoutExtensionHost {
	logout: () => Promise<void>;
}

export interface AdminLogoutButtonProps extends AdminExtensionProps {
	host: AdminLogoutExtensionHost;
}

export interface AdminLogoutButton {
	key: string;
	component: Component<AdminLogoutButtonProps>;
}

export type AdminCoreViewSurface =
	"collectionList" | "collectionCreate" | "collectionEdit" | "global" | "notFound";

export interface AdminCoreViewHost {
	refreshManifest: () => Promise<void>;
	documentsChanged: () => void;
	notify: (tone: AdminExtensionNotificationTone, title: string, message?: string) => void;
}

export interface AdminCoreViewProps extends AdminExtensionProps {
	surface: AdminCoreViewSurface;
	collection?: SchemaCollection;
	global?: SchemaGlobal;
	documentID?: string;
	/** The complete framework route, available to replacement wrappers. */
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

export type AdminCoreView = AdminCollectionCoreView | AdminGlobalCoreView | AdminNotFoundCoreView;

export type AdminBrandSurface = "loginLogo" | "navigationLogo" | "accountAvatar";

export interface AdminBrandComponentProps extends AdminExtensionProps {
	surface: AdminBrandSurface;
}

export interface AdminBrandComponent {
	key: string;
	surface: AdminBrandSurface;
	component: Component<AdminBrandComponentProps>;
}

export type AdminShellPosition = "header" | "actions" | "settingsMenu";

export interface AdminShellComponentProps extends AdminExtensionProps {
	position: AdminShellPosition;
}

export interface AdminShellComponent {
	key: string;
	position: AdminShellPosition;
	component: Component<AdminShellComponentProps>;
}

export interface AdminProviderProps {
	/** The remaining provider stack and framework admin application. */
	defaultView: Snippet;
	i18n: AdminI18n;
}

export interface AdminProvider {
	key: string;
	component: Component<AdminProviderProps>;
}

export interface AdminDashboardPanelProps {
	manifest: SchemaManifest;
	user?: FieldDocument;
	i18n: AdminI18n;
}

export interface AdminDashboardPanel {
	/** Plugin-owned identity used for deterministic ordering and collision checks. */
	key: string;
	component: Component<AdminDashboardPanelProps>;
	/** Replace suppresses the framework overview and may only be registered once. */
	position?: "before" | "after" | "replace";
}

export interface AdminListCellProps {
	collection: SchemaCollection;
	field: SchemaField;
	document: FieldDocument;
	value: unknown;
	i18n: AdminI18n;
}

export interface AdminListCell {
	key: string;
	collection: string;
	field: string;
	label: string;
	labelKey?: PluginTranslationKey;
	component: Component<AdminListCellProps>;
}

export type AdminExtensionNotificationTone = "success" | "error";

export interface AdminDocumentExtensionHost {
	refresh: () => Promise<void>;
	notify: (tone: AdminExtensionNotificationTone, title: string, message?: string) => void;
}

export interface AdminDocumentExtensionProps {
	collection: SchemaCollection;
	document: FieldDocument;
	host: AdminDocumentExtensionHost;
	i18n: AdminI18n;
}

export interface AdminDocumentAction {
	key: string;
	/** Omit to show the action for every non-global collection. */
	collection?: string;
	/** Hide the action unless the evaluated document operation is allowed. */
	requires?: keyof OperationCapabilities;
	component: Component<AdminDocumentExtensionProps>;
}

export interface AdminDocumentView {
	key: string;
	label: string;
	labelKey?: PluginTranslationKey;
	/** Omit to show the view for every collection and global. */
	collection?: string;
	component: Component<AdminDocumentExtensionProps>;
}

export interface AdminPluginNavigation {
	/** Human-readable navigation label inside the authenticated admin shell. */
	label: string;
	labelKey?: PluginTranslationKey;
	/** Optional grouping hint reserved for shells that render grouped plugin navigation. */
	group?: string;
}

/** One statically bundled route mounted beneath the authenticated admin layout. */
export interface AdminPluginRoute {
	/** Relative route path; it must exactly match the compiled backend descriptor. */
	path: string;
	/** Svelte route component rendered by the framework router. */
	component: Component;
	/** Optional navigation entry; omit it for a route reachable only by links or redirects. */
	navigation?: AdminPluginNavigation;
}

/** Metadata emitted from the compiled Go plugin into the generated registry. */
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
}

export interface AdminPluginPair {
	admin: AdminPlugin;
	backend: BackendAdminPlugin;
}

export interface ResolvedAdminPluginPairs {
	plugins: readonly AdminPlugin[];
	fields: readonly FieldPlugin[];
	rowLabels: readonly RowLabelPlugin[];
	routes: readonly AdminPluginRoute[];
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
}

export type ResolvedAdminPluginExtensions = Omit<ResolvedAdminPluginPairs, "plugins" | "fields">;

/** Defines one package export consumed by Ridu's generated static registry. */
export function defineAdminPlugin<const Plugin extends AdminPlugin>(plugin: Plugin): Plugin {
	return plugin;
}

/**
 * Validates every compiled-backend/admin-package pair before exposing its
 * field registrations. Missing packages still fail at the static import, while
 * stale or mismatched packages fail here with a plugin-specific diagnostic.
 */
export function resolveAdminPluginPairs(
	pairs: readonly AdminPluginPair[]
): ResolvedAdminPluginPairs {
	const keys = new Set<string>();
	const fieldIdentities = new Set<string>();
	const plugins: AdminPlugin[] = [];
	const fields: FieldPlugin[] = [];

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
		for (const field of admin.fields) {
			if (
				(field.type === "plugin" || field.componentKey !== undefined) &&
				field.key !== backend.key
			) {
				throw new Error(
					`Admin plugin ${backend.key} registered plugin field ${field.key ?? "<missing>"}`
				);
			}
			const fieldIdentity = `${field.type}:${field.key ?? ""}:${field.componentKey ?? ""}`;
			if (fieldIdentities.has(fieldIdentity)) {
				throw new Error(`Admin field renderer ${fieldIdentity} is registered more than once`);
			}
			fieldIdentities.add(fieldIdentity);
			fields.push(field);
		}
		keys.add(backend.key);
		plugins.push(admin);
	}
	const extensions = resolveAdminPluginExtensions(plugins);

	return {
		plugins: Object.freeze(plugins),
		fields: Object.freeze(fields),
		...extensions,
	};
}

/** Finalizes frontend-only extension registrations before the admin mounts. */
export function resolveAdminPluginExtensions(
	plugins: readonly AdminPlugin[]
): ResolvedAdminPluginExtensions {
	const routes: AdminPluginRoute[] = [];
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

	for (const plugin of plugins) {
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
		if (plugin.messages !== undefined) {
			if (messages[plugin.key] !== undefined) {
				throw new Error(`Admin plugin messages for ${plugin.key} are registered more than once`);
			}
			validateAdminMessages(plugin.key, plugin.messages);
			messages[plugin.key] = freezeAdminMessages(plugin.messages);
		}
		for (const route of plugin.routes ?? []) {
			validatePluginLabelKey(plugin, route.navigation?.labelKey, `route ${route.path}`);
			claim(identities, `route:${route.path}`, `Admin plugin route ${route.path}`);
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
	plugin: AdminPlugin,
	labelKey: PluginTranslationKey | undefined,
	surface: string
) {
	if (labelKey === undefined) return;
	const prefix = `plugin.${plugin.key}:`;
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
