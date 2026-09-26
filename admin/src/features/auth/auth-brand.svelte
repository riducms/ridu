<script lang="ts">
	import RiduLogo from "@admin/components/brand/ridu-logo.svelte";
	import { adminApplicationName } from "@admin/app-meta";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();

	const applicationName = $derived(
		adminApplicationName(runtime.manifest, runtime.i18n, runtime.i18n.t("general:riduApplication"))
	);
</script>

<div
	class="ridu-auth__logo"
	role="img"
	aria-label={runtime.i18n.t("auth:adminBrand", { application: applicationName })}
>
	<RiduLogo class="ridu-auth__logo-mark" />
</div>
