<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import { Button } from "@riducms/ui";
	import type { UploadDraft } from "@admin/features/uploads/upload-draft.svelte";
	import type { SchemaUploadSettings } from "@riducms/protocol";
	import { preparedAdminModule } from "@admin/core/bootstrap/admin-route-modules";

	type UploadDocumentPreviewProps = {
		draft: UploadDraft;
		settings: SchemaUploadSettings;
		editable?: boolean;
	};

	let props: UploadDocumentPreviewProps = $props();
	const i18n = getAdminI18n();
	const preview =
		preparedAdminModule<typeof import("@admin/features/uploads/upload-control.svelte")>(
			"upload-preview"
		) ?? import("@admin/features/uploads/upload-control.svelte");

	import "@admin/features/documents/document-loading.scss";
</script>

{#await preview}
	<section
		class="ridu-document-module ridu-document-module--upload"
		data-ridu-loading-surface="upload-preview-module"
		aria-busy="true"
		aria-label={i18n.t("uploads:loadingAssetPreview")}
	>
		<p class="ridu-document-module__message">{i18n.t("uploads:loadingAssetPreview")}</p>
	</section>
{:then { default: Preview }}
	<Preview {...props} />
{:catch}
	<section
		class="ridu-document-module ridu-document-module--upload ridu-document-module--error"
		role="alert"
	>
		<p>{i18n.t("uploads:previewLoadFailed")}</p>
		<Button onclick={() => window.location.reload()}>{i18n.t("general:reloadPage")}</Button>
	</section>
{/await}
