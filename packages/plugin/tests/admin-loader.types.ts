import type { Component } from "svelte";
import type {
	AdminCoreView,
	AdminCoreViewProps,
	AdminDashboardPanel,
	AdminExtensionProps,
	AdminLoaderProps,
	AdminRoute,
} from "../src/plugin";
import { withAdminLoader } from "../src/plugin";
import type { AdminLoader } from "@riducms/sdk";

declare const loader: AdminLoader<{ q?: string }, { count: number }>;
declare const basic: Component<AdminExtensionProps & AdminLoaderProps<{ count: number }>>;
declare const core: Component<AdminCoreViewProps & AdminLoaderProps<{ count: number }>>;
declare const wrong: Component<AdminExtensionProps & AdminLoaderProps<{ count: string }>>;
declare const noProps: Component;

const route: AdminRoute = { path: "report", ...withAdminLoader(loader, basic) };
const plainRoute: AdminRoute = { path: "static", component: noProps };
const dashboard: AdminDashboardPanel = { key: "report", ...withAdminLoader(loader, basic) };
const view: AdminCoreView = {
	key: "list",
	surface: "collectionList",
	...withAdminLoader(loader, core),
};
// @ts-expect-error The component's data must match the generated Go output.
withAdminLoader(loader, wrong);
// @ts-expect-error Core-view-only props cannot be supplied by a standalone custom route.
const wrongHost: AdminRoute = { path: "report", ...withAdminLoader(loader, core) };
void [route, plainRoute, dashboard, view, wrongHost];
