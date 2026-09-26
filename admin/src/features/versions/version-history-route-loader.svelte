<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";

	import { Button } from "@riducms/ui";
	import { preparedAdminModule } from "@admin/core/bootstrap/admin-route-modules";
	import { registerAdminScrollPage } from "@admin/core/routing/admin-scroll.svelte";

	registerAdminScrollPage({ ready: () => false });
	const i18n = getAdminI18n();
	const route =
		preparedAdminModule<typeof import("@admin/features/versions/version-history-route.svelte")>(
			"versions"
		) ?? import("@admin/features/versions/version-history-route.svelte");
</script>

{#await route}
	<section
		class="grid min-h-72 place-items-center"
		data-ridu-loading-surface="versions-module"
		aria-busy="true"
		aria-label={i18n.t("general:loading")}
	>
		<p class="text-[12px] text-foreground-faint">{i18n.t("general:loading")}</p>
	</section>
{:then { default: VersionHistoryRoute }}
	<VersionHistoryRoute />
{:catch}
	<section class="grid min-h-72 content-center justify-items-center gap-4" role="alert">
		<p class="text-[12px] text-destructive">{i18n.t("versions:historyLoadFailed")}</p>
		<Button onclick={() => window.location.reload()}>{i18n.t("general:reloadPage")}</Button>
	</section>
{/await}
