<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";

	const i18n = getAdminI18n();
	const route = import("@admin/features/documents/document-route.svelte");
</script>

{#await route}
	<section
		class="grid min-h-72 place-items-center"
		aria-busy="true"
		aria-label={i18n.t("documents:loadingDocument")}
	>
		<p class="text-[12px] text-foreground-faint">{i18n.t("documents:loadingDocument")}</p>
	</section>
{:then { default: DocumentRoute }}
	<DocumentRoute />
{:catch}
	<section class="grid min-h-72 place-items-center" role="alert">
		<p class="text-[12px] text-destructive">{i18n.t("documents:workspaceLoadFailed")}</p>
	</section>
{/await}
