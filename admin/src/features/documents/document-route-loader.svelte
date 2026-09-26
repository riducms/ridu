<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";

	import { Button } from "@riducms/ui";
	import { preparedAdminModule } from "@admin/core/bootstrap/admin-route-modules";
	import { registerAdminScrollPage } from "@admin/core/routing/admin-scroll.svelte";

	registerAdminScrollPage({ ready: () => false });
	const i18n = getAdminI18n();
	const route =
		preparedAdminModule<typeof import("@admin/features/documents/document-route.svelte")>(
			"document"
		) ?? import("@admin/features/documents/document-route.svelte");

	import "@admin/features/documents/document-loading.scss";
</script>

{#await route}
	<section
		class="ridu-document-module"
		data-ridu-loading-surface="document-module"
		aria-busy="true"
		aria-label={i18n.t("documents:loadingDocument")}
	>
		<p class="ridu-document-module__message">{i18n.t("documents:loadingDocument")}</p>
	</section>
{:then { default: DocumentRoute }}
	<DocumentRoute />
{:catch}
	<section class="ridu-document-module ridu-document-module--error" role="alert">
		<p class="ridu-document-module__message">{i18n.t("documents:workspaceLoadFailed")}</p>
		<Button onclick={() => window.location.reload()}>{i18n.t("general:reloadPage")}</Button>
	</section>
{/await}
