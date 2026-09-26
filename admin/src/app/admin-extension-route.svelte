<script lang="ts">
	import type { AdminRoute } from "@riducms/plugin";

	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import AdminViewHost from "@admin/core/loaders/admin-view-host.svelte";

	let { route }: { route: AdminRoute } = $props();
	const runtime = getAdminRuntime();
</script>

{#if runtime.manifest !== undefined}
	{#key route}
		<AdminViewHost
			view={route}
			ownsRoute
			props={{ manifest: runtime.manifest, user: runtime.session?.user, i18n: runtime.i18n }}
		/>
	{/key}
{/if}
