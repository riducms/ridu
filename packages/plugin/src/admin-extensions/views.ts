import type { SchemaCollection, SchemaGlobal } from "@riducms/protocol";
import type { Component, Snippet } from "svelte";

import type { FieldDocument } from "../authoring";
import type { AdminI18n, ExtensionTranslationKey } from "../i18n";
import type {
	AdminExtensionNotificationTone,
	AdminExtensionProps,
	AdminViewComponent,
} from "./shared";

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
}

interface AdminGlobalCoreView {
	key: string;
	surface: "global";
	/** Omit to replace this surface for every global. */
	global?: string;
}

interface AdminNotFoundCoreView {
	key: string;
	surface: "notFound";
}

/**
 * Replace a collection/global/not-found screen. A resource-specific registration
 * wins over an all-resources fallback for that surface; duplicate targets fail.
 * The supplied `defaultView` lets your component wrap the existing screen.
 */
export type AdminCoreView = (
	AdminCollectionCoreView | AdminGlobalCoreView | AdminNotFoundCoreView
) &
	AdminViewComponent<AdminCoreViewProps>;

export interface AdminDashboardPanelProps {
	manifest: AdminExtensionProps["manifest"];
	user?: FieldDocument;
	i18n: AdminI18n;
}

/** Add dashboard content before/after Ridu's overview, or replace that overview. */
interface AdminDashboardPanelRegistration {
	/** Unique name among dashboard entries; duplicates throw. Array order controls display order. */
	key: string;
	/** Defaults to after. Replace hides Ridu's overview and may only be registered once. */
	position?: "before" | "after" | "replace";
}

export type AdminDashboardPanel = AdminDashboardPanelRegistration &
	AdminViewComponent<AdminDashboardPanelProps>;

export interface AdminRouteNavigation {
	/** Human-readable navigation label inside the authenticated admin shell. */
	label: string;
	labelKey?: ExtensionTranslationKey;
	/** Optional grouping hint reserved for shells that render grouped plugin navigation. */
	group?: string;
}

/** One statically bundled route mounted beneath the authenticated admin layout. */
export type AdminRoute = {
	/** Relative admin path, without a leading slash. Paired plugins must match their Go descriptor. */
	path: string;
	/** Optional navigation entry; omit it for a route reachable only by links or redirects. */
	navigation?: AdminRouteNavigation;
} & AdminViewComponent<AdminExtensionProps>;
