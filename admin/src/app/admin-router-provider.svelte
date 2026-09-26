<script lang="ts">
	import { createBrowserRouter, RouterProvider } from "@hvniel/svelte-router";

	import AdminRouteError from "@admin/app/admin-route-error.svelte";
	import AdminRouterRoot from "@admin/app/admin-router-root.svelte";
	import type { AdminBootstrapCoordinator } from "@admin/core/bootstrap/admin-bootstrap";

	let {
		coordinator,
		adminBasePath,
	}: { coordinator: AdminBootstrapCoordinator; adminBasePath: string } = $props();
	// One router owns the coordinator and base path selected when this provider mounts.
	// svelte-ignore state_referenced_locally
	const router = createBrowserRouter(
		[
			{
				id: "admin-root",
				path: "*",
				Component: AdminRouterRoot,
				ErrorBoundary: AdminRouteError,
				loader: coordinator.loader,
				shouldRevalidate: coordinator.shouldRevalidate,
			},
		],
		{
			basename: adminBasePath,
			hydrationData: { loaderData: { "admin-root": coordinator.initialState ?? null } },
		}
	);

	$effect(() => () => router.dispose());
</script>

<RouterProvider {router} />
