<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";

	type DocumentAPIViewProps = {
		resourceSlug: string;
		resourceLabel: string;
		documentID?: string;
		globalResource?: boolean;
		fallbackValue: unknown;
	};

	let props: DocumentAPIViewProps = $props();
	const i18n = getAdminI18n();
	const view = import("@admin/features/documents/document-api-view.svelte");
</script>

{#await view}
	<section
		class="grid min-h-72 place-items-center"
		aria-busy="true"
		aria-label={i18n.t("documents:loadingAPIView")}
	>
		<p class="text-[12px] text-foreground-faint">{i18n.t("documents:loadingAPIView")}</p>
	</section>
{:then { default: DocumentAPIView }}
	<DocumentAPIView {...props} />
{:catch}
	<section class="grid min-h-72 place-items-center" role="alert">
		<p class="text-[12px] text-destructive">{i18n.t("documents:apiViewLoadFailed")}</p>
	</section>
{/await}
