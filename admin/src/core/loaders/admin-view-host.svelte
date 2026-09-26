<script lang="ts" generics="Props extends object">
	import type { AdminViewComponent } from "@riducms/plugin";
	import { useLocation } from "@hvniel/svelte-router";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { getAdminBootstrapCoordinator } from "@admin/core/bootstrap/admin-bootstrap";
	import { normalizedURLIdentity } from "@admin/core/bootstrap/admin-prepared-state";
	import { Banner } from "@admin/components/ui/banner";
	import { Button } from "@riducms/ui";
	import { AdminViewLoader } from "@admin/core/loaders/admin-view-loader.svelte";
	import { registerAdminScrollPage } from "@admin/core/routing/admin-scroll.svelte";

	let {
		view,
		props,
		ownsRoute = false,
	}: { view: AdminViewComponent<Props>; props: Props; ownsRoute?: boolean } = $props();
	const runtime = getAdminRuntime();
	const bootstrap = getAdminBootstrapCoordinator();
	const location = useLocation();
	// Registrations are static for the lifetime of this keyed host.
	// svelte-ignore state_referenced_locally
	const loader = view.loader;
	const controller =
		loader === undefined
			? undefined
			: new AdminViewLoader({
					client: runtime.client,
					loader,
					get route() {
						return normalizedURLIdentity(location.current);
					},
					get prepared() {
						return bootstrap.loaderData(
							loader.key,
							location.current.pathname,
							location.current.search
						);
					},
				});
	// Route hosts wait for their fallback read or terminal error before restoring scroll. Dashboard
	// panels share a page and deliberately leave this false so they cannot compete for ownership.
	// svelte-ignore state_referenced_locally
	const routeOwner = ownsRoute;
	if (routeOwner) {
		registerAdminScrollPage({
			ready: () => loader === undefined || controller?.settled === true,
		});
	}
</script>

{#if view.loader === undefined}
	<view.component {...props} />
{:else if controller !== undefined}
	{#if controller.error !== undefined}
		<Banner tone="destructive">
			<p>{controller.error}</p>
			<Button variant="outline" onclick={controller.refresh}>
				{runtime.i18n.t("general:retry")}
			</Button>
		</Banner>
	{/if}
	{#if controller.ready}
		<view.component
			{...props}
			data={controller.data}
			refresh={controller.refresh}
			refreshing={controller.refreshing}
		/>
	{:else if controller.error === undefined}
		<div aria-busy="true" data-ridu-loading-surface="admin-view-loader"></div>
	{/if}
{/if}
