<script lang="ts">
	import { setAdminI18n } from "@riducms/plugin";
	import { TooltipProvider } from "@riducms/ui";

	import AdminProviderLayer from "@admin/app/admin-provider-layer.svelte";
	import AdminRouterProvider from "@admin/app/admin-router-provider.svelte";
	import LoadingBoundary from "@admin/app/loading-boundary.svelte";
	import { Toaster } from "@admin/components/ui/sonner";
	import {
		NotificationCenter,
		setNotificationCenter,
	} from "@admin/core/notifications/notification-center.svelte";
	import { AdminRuntime, setAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import {
		AdminBootstrapCoordinator,
		setAdminBootstrapCoordinator,
	} from "@admin/core/bootstrap/admin-bootstrap";
	import type { AdminClient } from "@admin/core/api/admin-client";
	import DevelopmentTools from "@admin/features/development/development-tools.svelte";

	import type { AdminConfig } from "@riducms/plugin/admin";

	interface Props {
		adminConfig?: AdminConfig;
		clientFactory: () => AdminClient;
		adminBasePath?: string;
	}

	let { clientFactory, adminBasePath = "/admin", adminConfig = {} }: Props = $props();
	const coordinator = setAdminBootstrapCoordinator(new AdminBootstrapCoordinator());

	// The runtime owns the client factory, config, and base path selected when this admin instance
	// is mounted. Those inputs intentionally remain stable for the instance lifetime.
	// svelte-ignore state_referenced_locally
	const runtime = setAdminRuntime(new AdminRuntime(clientFactory(), adminConfig, adminBasePath));
	coordinator.configure(runtime);

	let preparedRuntimeAdopted = false;
	if (coordinator.initialState?.runtime !== undefined) {
		try {
			runtime.adoptPrepared(coordinator.initialState.runtime);
			preparedRuntimeAdopted = true;
		} catch {
			// An invalid embedded snapshot must fall back to the ordinary client bootstrap.
			preparedRuntimeAdopted = false;
		}
	}

	setAdminI18n(runtime.i18n);
	const notifications = setNotificationCenter(new NotificationCenter());
	const development = import.meta.hot !== undefined;
	const notificationPosition = $derived(
		development ? (runtime.i18n.direction === "rtl" ? "top-left" : "top-right") : undefined
	);
	let schemaRefreshQueued = $state(false);
	let startupReady = $state(false);

	$effect(() => {
		let active = true;
		const hot = import.meta.hot;
		const queueSchemaRefresh = () => {
			schemaRefreshQueued = true;
		};
		hot?.on("ridu:schema-update", queueSchemaRefresh);

		async function start() {
			if (!preparedRuntimeAdopted) await runtime.bootstrap();
			await coordinator.prepareInitial({ stageRoute: preparedRuntimeAdopted });
			if (!active) return;
			startupReady = true;
		}

		start().catch(() => {
			if (!active) return;
			// Runtime and route controllers own their visible failures; the shell must still mount
			// so their fallback state can render.
			startupReady = true;
		});

		return () => {
			active = false;
			hot?.off("ridu:schema-update", queueSchemaRefresh);
			runtime.dispose();
			notifications.destroy();
		};
	});

	$effect(() => {
		if (!schemaRefreshQueued || runtime.loading || runtime.refreshingManifest) return;
		const bootstrap = runtime.error !== undefined || runtime.manifest === undefined;
		schemaRefreshQueued = false;
		void (bootstrap ? runtime.bootstrap() : runtime.refreshManifest());
	});
</script>

<TooltipProvider delayDuration={350}>
	<AdminProviderLayer providers={runtime.config.extensions.providers} i18n={runtime.i18n}>
		{#if startupReady}
			<LoadingBoundary>
				<AdminRouterProvider {coordinator} {adminBasePath} />
			</LoadingBoundary>
			{#if development && !runtime.loading && runtime.error === undefined}
				<DevelopmentTools />
			{/if}
		{:else}
			<main class="ridu-surface-grid min-h-screen" aria-busy="true"></main>
		{/if}
	</AdminProviderLayer>

	<Toaster theme={runtime.resolvedTheme} position={notificationPosition} />
</TooltipProvider>
