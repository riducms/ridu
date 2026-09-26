<script lang="ts">
	import { useLocation } from "@hvniel/svelte-router";

	import { ADMIN_NAME, adminApplicationName } from "@admin/app-meta";
	import { resolveAdminPageLabel } from "@admin/app/admin-page-title";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();
	const location = useLocation();
	const extensions = runtime.config.extensions;
	const pageLabel = $derived(
		resolveAdminPageLabel({
			pathname: location.current.pathname,
			manifest: runtime.manifest,
			routes: extensions.routes,
			documentViews: extensions.documentViews,
			i18n: runtime.i18n,
		})
	);
	const applicationName = $derived(
		adminApplicationName(runtime.manifest, runtime.i18n, ADMIN_NAME)
	);
	const pageTitle = $derived(`${pageLabel} - ${applicationName}`);
</script>

<svelte:head><title>{pageTitle}</title></svelte:head>
