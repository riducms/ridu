<script lang="ts">
	import RiduLogo from "@admin/components/brand/ridu-logo.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();
	const applicationName = $derived(
		runtime.manifest?.application.name ?? runtime.i18n.t("general:riduApplication")
	);
</script>

<div
	class="inline-flex text-foreground"
	role="img"
	aria-label={runtime.i18n.t("auth:adminBrand", { application: applicationName })}
>
	<RiduLogo class="h-7" />
</div>
