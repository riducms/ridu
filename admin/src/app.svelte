<script lang="ts">
	import { createBrowserRouter, RouterProvider } from "@hvniel/svelte-router";
	import { setAdminI18n, type AdminPlugin, type FieldPlugin } from "@riducms/plugin";
	import type { TranslationLanguage } from "@riducms/translations";
	import { TooltipProvider } from "@riducms/ui";
	import { untrack } from "svelte";

	import AdminRouterRoot from "@admin/app/admin-router-root.svelte";
	import AdminProviderLayer from "@admin/app/admin-provider-layer.svelte";
	import LoadingBoundary from "@admin/app/loading-boundary.svelte";
	import { Toaster } from "@admin/components/ui/sonner";
	import {
		NotificationCenter,
		setNotificationCenter,
	} from "@admin/core/notifications/notification-center.svelte";
	import { AdminRuntime, setAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import type { AdminClient } from "@admin/core/api/admin-client";
	import DevelopmentTools from "@admin/features/development/development-tools.svelte";

	interface Props {
		clientFactory: () => AdminClient;
		adminBasePath?: string;
		plugins?: readonly AdminPlugin[];
		fieldPlugins?: readonly FieldPlugin[];
		languages?: readonly TranslationLanguage[];
	}

	let {
		clientFactory,
		adminBasePath = "/admin",
		plugins = [],
		fieldPlugins = [],
		languages,
	}: Props = $props();
	// The runtime owns the client and plugin registry selected when this admin instance is mounted.
	// svelte-ignore state_referenced_locally
	const runtime = setAdminRuntime(
		new AdminRuntime(clientFactory(), plugins, fieldPlugins, languages)
	);
	setAdminI18n(runtime.i18n);
	const notifications = setNotificationCenter(new NotificationCenter());
	const development = import.meta.hot !== undefined;
	const notificationPosition = $derived(
		development ? (runtime.i18n.direction === "rtl" ? "top-left" : "top-right") : undefined
	);
	let schemaRefreshQueued = $state(false);
	// The router owns the base path selected when this admin instance is mounted.
	// svelte-ignore state_referenced_locally
	const router = createBrowserRouter([{ path: "*", Component: AdminRouterRoot }], {
		basename: adminBasePath,
	});

	$effect(() => {
		const hot = import.meta.hot;
		if (hot === undefined) return;
		const queueSchemaRefresh = () => {
			schemaRefreshQueued = true;
		};
		hot.on("ridu:schema-update", queueSchemaRefresh);
		return () => hot.off("ridu:schema-update", queueSchemaRefresh);
	});

	$effect(() => {
		void runtime.bootstrap();
	});

	$effect(() => {
		if (!schemaRefreshQueued || runtime.loading || runtime.refreshingManifest) return;
		const bootstrap = runtime.error !== undefined || runtime.manifest === undefined;
		schemaRefreshQueued = false;
		untrack(() => {
			void (bootstrap ? runtime.bootstrap() : runtime.refreshManifest());
		});
	});

	$effect(() => () => notifications.destroy());
</script>

<TooltipProvider delayDuration={350}>
	<AdminProviderLayer providers={runtime.providers} i18n={runtime.i18n}>
		<LoadingBoundary>
			<RouterProvider {router} />
		</LoadingBoundary>
	</AdminProviderLayer>

	{#if development && !runtime.loading && runtime.error === undefined}<DevelopmentTools />{/if}
	<Toaster theme={runtime.resolvedTheme} position={notificationPosition} />
</TooltipProvider>
