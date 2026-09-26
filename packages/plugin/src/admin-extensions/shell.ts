import type { Component, Snippet } from "svelte";

import type { AdminI18n } from "../i18n";
import type { AdminExtensionProps } from "./shared";

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

/** Replace the navigation footer's sign-out button; call the supplied `host.logout()` on activation. */
export interface AdminLogoutButton {
	key: string;
	component: Component<AdminLogoutButtonProps>;
}

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

export type AdminShellSlotPosition = "header" | "actions" | "settingsMenu";

export interface AdminShellSlotProps extends AdminExtensionProps {
	position: AdminShellSlotPosition;
}

/** Add UI to the global header, actions area or account settings menu in registration order. */
export interface AdminShellSlot {
	key: string;
	position: AdminShellSlotPosition;
	component: Component<AdminShellSlotProps>;
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
