import type { PluginMessageCatalog } from "#lib/i18n.js";
import type { RowLabelPlugin } from "#lib/row-label.js";
import type { AdminDocumentAction, AdminDocumentView } from "#lib/admin-extensions/documents.js";
import type {
	AdminListCellRenderer,
	AdminListResultsRenderer,
} from "#lib/admin-extensions/lists.js";
import type {
	AdminBrandComponent,
	AdminLogoutButton,
	AdminNavigationComponent,
	AdminProvider,
	AdminShellSlot,
} from "#lib/admin-extensions/shell.js";
import type {
	AdminAccountComponent,
	AdminCoreView,
	AdminDashboardPanel,
	AdminLoginComponent,
	AdminRoute,
} from "#lib/admin-extensions/views.js";

export * from "#lib/admin-extensions/documents.js";
export * from "#lib/admin-extensions/lists.js";
export * from "#lib/admin-extensions/shared.js";
export * from "#lib/admin-extensions/shell.js";
export * from "#lib/admin-extensions/views.js";

/**
 * Places where a plugin or application can add admin UI. Arrays keep registration
 * order; application entries follow plugin entries. Replacement slots allow one
 * matching replacement. Conflicts are reported instead of silently overriding UI.
 */
export interface AdminContributions {
	/** Authenticated admin routes contributed by this package. */
	routes?: readonly AdminRoute[];
	/** Panels composed into or replacing the authenticated dashboard. */
	dashboardPanels?: readonly AdminDashboardPanel[];
	/** Components composed around or replacing the sign-in screen. */
	login?: readonly AdminLoginComponent[];
	/** Components composed around or replacing the framework account screens. */
	account?: readonly AdminAccountComponent[];
	/** Components composed around or replacing the framework navigation. */
	navigation?: readonly AdminNavigationComponent[];
	/** Replaces the default sign-out control in the navigation footer. */
	logoutButton?: AdminLogoutButton;
	/** Wraps or replaces framework collection, global, and not-found views. */
	coreViews?: readonly AdminCoreView[];
	/** Replaces framework-owned brand graphics at exact shell surfaces. */
	branding?: readonly AdminBrandComponent[];
	/** Adds components to explicit global shell slots. */
	shellSlots?: readonly AdminShellSlot[];
	/** Wraps the admin in statically ordered Svelte context providers. */
	providers?: readonly AdminProvider[];
	/** Collection list cells rendered for exact collection/field pairs. */
	listCellRenderers?: readonly AdminListCellRenderer[];
	/** Replace only the results layout; Ridu retains list loading, controls and pagination. */
	listResultsRenderers?: readonly AdminListResultsRenderer[];
	/** Actions rendered alongside framework-owned document actions. */
	documentActions?: readonly AdminDocumentAction[];
	/** Read-only or operational views rendered beside Edit and API. */
	documentViews?: readonly AdminDocumentView[];
}

/** Checked registrations collected for Ridu's admin runtime. Normally consumed by generated code. */
export interface ResolvedAdminExtensions {
	rowLabels: readonly RowLabelPlugin[];
	routes: readonly AdminRoute[];
	dashboardPanels: readonly AdminDashboardPanel[];
	login: readonly AdminLoginComponent[];
	account: readonly AdminAccountComponent[];
	navigation: readonly AdminNavigationComponent[];
	logoutButton: AdminLogoutButton | undefined;
	coreViews: readonly AdminCoreView[];
	branding: readonly AdminBrandComponent[];
	shellSlots: readonly AdminShellSlot[];
	providers: readonly AdminProvider[];
	listCellRenderers: readonly AdminListCellRenderer[];
	listResultsRenderers: readonly AdminListResultsRenderer[];
	documentActions: readonly AdminDocumentAction[];
	documentViews: readonly AdminDocumentView[];
	messages: Readonly<Record<string, PluginMessageCatalog>>;
	applicationMessages: PluginMessageCatalog | undefined;
}
