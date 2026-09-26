<script lang="ts">
	import { useLoaderData, useLocation } from "@hvniel/svelte-router";
	import type { AdminPreparedRouteStateV1 } from "@riducms/protocol";
	import { tick } from "svelte";

	import { createAdminScroll } from "@admin/core/routing/admin-scroll.svelte";
	import { getAdminBootstrapCoordinator } from "@admin/core/bootstrap/admin-bootstrap";
	import { normalizedURLIdentity } from "@admin/core/bootstrap/admin-prepared-state";
	import AdminNavigationProgress from "@admin/app/admin-navigation-progress.svelte";
	import AdminPageTitle from "@admin/app/admin-page-title.svelte";
	import AdminRoutes from "@admin/app/admin-routes.svelte";

	const coordinator = getAdminBootstrapCoordinator();
	const location = useLocation();
	const loaderData = useLoaderData<AdminPreparedRouteStateV1 | null>();
	createAdminScroll();
	let committedStateKey = "";
	let markedIdentity = "";

	$effect.pre(() => {
		const routedIdentity = normalizedURLIdentity(location.current);
		const state = loaderData.current;
		const stateKey = `${routedIdentity}:${state?.fingerprint ?? "none"}:${state?.outcome ?? "none"}`;
		if (stateKey === committedStateKey) return;

		committedStateKey = stateKey;
		if (state == null) return;

		const stateIdentity = `${state.pathname}${state.search}`;
		const url = new URL(stateIdentity, window.location.origin);
		const preparedRoute = coordinator.commit(state, url);
		if (preparedRoute !== true) return;

		// Descendant controllers adopt the staged seed during this update. Discard it only after
		// their pre-effects and the resulting DOM have committed.
		tick().then(() => {
			if (!coordinator.isRouteStaged(state)) return;
			coordinator.discardRouteData(state);

			if (stateIdentity !== markedIdentity) {
				markedIdentity = stateIdentity;
				performance.mark("ridu:admin-route-ready");
			}
		});
	});
</script>

<AdminNavigationProgress />
<AdminPageTitle />
<AdminRoutes />
