<script lang="ts">
	import { Button } from "@riducms/ui";
	import { getAdminI18n } from "@riducms/plugin";
	import { preparedAdminModule } from "@admin/core/bootstrap/admin-route-modules";
	import type { AdminDocumentV1, AdminReadResultV1 } from "@riducms/protocol";

	type DocumentAPIViewProps = {
		resourceSlug: string;
		documentID?: string;
		globalResource?: boolean;
		contentLocale?: string;
		fallbackValue: unknown;
		prepared?: AdminReadResultV1<AdminDocumentV1>;
	};

	let props: DocumentAPIViewProps = $props();
	const i18n = getAdminI18n();
	const view =
		preparedAdminModule<typeof import("@admin/features/documents/document-api-view.svelte")>(
			"document-api"
		) ?? import("@admin/features/documents/document-api-view.svelte");

	import "@admin/features/documents/document-loading.scss";
</script>

{#await view}
	<section
		class="ridu-document-module"
		data-ridu-loading-surface="document-api-module"
		aria-busy="true"
		aria-label={i18n.t("documents:loadingAPIView")}
	>
		<p class="ridu-document-module__message">{i18n.t("documents:loadingAPIView")}</p>
	</section>
{:then { default: DocumentAPIView }}
	<DocumentAPIView {...props} />
{:catch}
	<section class="ridu-document-module ridu-document-module--error" role="alert">
		<p class="ridu-document-module__message">{i18n.t("documents:apiViewLoadFailed")}</p>
		<Button onclick={() => window.location.reload()}>{i18n.t("general:reloadPage")}</Button>
	</section>
{/await}
